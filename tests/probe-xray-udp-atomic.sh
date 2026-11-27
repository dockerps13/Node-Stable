#!/usr/bin/env bash
# Test-only Go source overlay for the cedar2025/Xray-core UDP dispatcher race.
# Does not mutate go.mod, the module cache, or installed binaries.
set -euo pipefail
dst="${1:?provide an unused output directory}"
if [[ -e "$dst" ]]; then echo "destination exists" >&2; exit 2; fi
mkdir -m 700 -p "$dst"
module_json="$(go list -m -json github.com/xtls/xray-core)"
export XRAY_MODULE_JSON="$module_json" XRAY_OVERLAY_DST="$(realpath "$dst")"
python3 - <<'PY'
import json
import os
from pathlib import Path

m = json.loads(os.environ["XRAY_MODULE_JSON"])
replacement = m.get("Replace", {})
if replacement.get("Path") != "github.com/cedar2025/Xray-core":
    raise RuntimeError("unexpected Xray-core module replacement: %r" % replacement)
source_dir = Path(replacement["Dir"])
source = source_dir / "transport/internet/udp/dispatcher.go"
original = source.read_text()
if "sync/atomic" in original:
    raise RuntimeError("Xray source may already be fixed; review patch before reuse")
patches = [
    ('\t"sync"\n', '\t"sync"\n\t"sync/atomic"\n'),
    ('\tclosed bool\n', '\tclosed atomic.Bool\n'),
    ('if c.closed {\n\t\tpanic("terminate called more than once")', 'if c.closed.Swap(true) {\n\t\tpanic("terminate called more than once")'),
    ('\tc.closed = true\n\tc.cancel()', '\tc.cancel()'),
    ('if v.conn.closed {', 'if v.conn.closed.Load() {'),
]
patched = original
for old, new in patches:
    occurrences = patched.count(old)
    if occurrences != 1:
        raise RuntimeError("Xray UDP source mismatch (%d occurrences): %r" % (occurrences, old))
    patched = patched.replace(old, new, 1)
dst = Path(os.environ["XRAY_OVERLAY_DST"])
patched_path = dst / "dispatcher.go"
patched_path.write_text(patched)
overlay_path = dst / "xray-overlay.json"
overlay_path.write_text(json.dumps({"Replace": {str(source): str(patched_path)}}, indent=2) + "\n")
(dst / "provenance.json").write_text(json.dumps({
    "module": m["Path"],
    "replacement_path": replacement["Path"],
    "replacement_version": replacement.get("Version"),
    "original_source": str(source),
    "changes": ["connEntry.closed bool -> atomic.Bool",
                "terminate Swap(true)", "getInboundRay Load()"]
}, indent=2)+"\n")
print("Isolated Xray UDP overlay prepared:", overlay_path)
PY
