#!/usr/bin/env bash
# Isolated, writable Go module copy for testing the upstream Xray UDP fix.
# Only this CI checkout's go.mod is changed. Production dependency stays pinned.
set -euo pipefail
dst="${1:?provide an unused output directory}"
if [[ -e "$dst" ]]; then echo "destination exists: $dst" >&2; exit 2; fi

module_json="$(go list -m -json github.com/xtls/xray-core)"
export XRAY_MODULE_JSON="$module_json" XRAY_PATCH_DST="$dst"
python3 - <<'PY'
import json
import os
from pathlib import Path
import shutil
import subprocess

module = json.loads(os.environ["XRAY_MODULE_JSON"])
replacement = module.get("Replace", {})
if replacement.get("Path") != "github.com/cedar2025/Xray-core":
    raise RuntimeError("unexpected Xray replacement: %r" % replacement)

src = replacement.get("Dir")
if not src:
    version = replacement.get("Version")
    if not version:
        raise RuntimeError("Xray replacement version missing")
    output = subprocess.check_output(
        ["go", "mod", "download", "-json", replacement["Path"] + "@" + version],
        text=True,
    )
    src = json.loads(output)["Dir"]
dst = Path(os.environ["XRAY_PATCH_DST"]).resolve()
shutil.copytree(Path(src), dst)
# Go module-cache directories are read-only; make the isolated root writable.
dst.chmod(dst.stat().st_mode | 0o700)
target = dst / "transport/internet/udp/dispatcher.go"
target.chmod(target.stat().st_mode | 0o200)
original = target.read_text()
if '"sync/atomic"' in original:
    raise RuntimeError("already patched: re-audit source before continuing")
patches = [
    ('\t"sync"\n', '\t"sync"\n\t"sync/atomic"\n'),
    ('\tclosed bool\n', '\tclosed atomic.Bool\n'),
    ('if c.closed {\n\t\tpanic("terminate called more than once")',
     'if c.closed.Swap(true) {\n\t\tpanic("terminate called more than once")'),
    ('\tc.closed = true\n\tc.cancel()', '\tc.cancel()'),
    ('if v.conn.closed {', 'if v.conn.closed.Load() {'),
]
patched = original
for old, new in patches:
    if patched.count(old) != 1:
        raise RuntimeError("unexpected upstream source for patch anchor: %r" % old)
    patched = patched.replace(old, new, 1)
target.write_text(patched)
# PacketReader's r.dest points at the reusable MUX FrameMetadata.Target.
# Give each asynchronously dispatched UDP buffer an owned destination copy.
reader = dst / "common/mux/reader.go"
reader.chmod(reader.stat().st_mode | 0o200)
original_reader = reader.read_text()
old_reader = "\t\tb.UDP = r.dest\n"
new_reader = "\t\t// The MUX frame metadata is reused on the next frame.\n\t\t// Keep an independent destination for this dispatched buffer.\n\t\tdestination := *r.dest\n\t\tb.UDP = &destination\n"
if original_reader.count(old_reader) != 1:
    raise RuntimeError("unexpected Xray MUX PacketReader destination anchor")
patched_reader = original_reader.replace(old_reader, new_reader, 1)
reader.write_text(patched_reader)
(dst / "xray-udp-patch-provenance.json").write_text(json.dumps({
    "upstream": replacement["Path"],
    "version": replacement.get("Version"),
    "patch": "UDP dispatcher connEntry.closed atomic Bool plus owned MUX UDP packet destination",
    "mux_reader_original_sha256": __import__("hashlib").sha256(original_reader.encode()).hexdigest(),
    "mux_reader_patched_sha256": __import__("hashlib").sha256(patched_reader.encode()).hexdigest(),
    "source_sha256": __import__("hashlib").sha256(original.encode()).hexdigest(),
    "patched_sha256": __import__("hashlib").sha256(patched.encode()).hexdigest(),
    "scope": "test-only; repository Go module replacement not persisted",
}, indent=2) + "\n")
print("Patched test-only Xray module:", dst)
PY
# Change only the ephemeral CI working tree's Go module resolution.
go mod edit "-replace=github.com/xtls/xray-core=$(realpath "$dst")"
go mod tidy
go list -m -json github.com/xtls/xray-core | grep -F "$(realpath "$dst")"
