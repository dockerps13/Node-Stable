#!/usr/bin/env bash
# Test-only probe: backport the narrowly scoped TUIC callback lifetime fix
# into an isolated local copy of sing-quic v0.6.0.
# This does NOT update the repository's go.mod/go.sum or production binaries.
set -euo pipefail

workdir="${1:?provide an empty temporary work directory}"
if [[ -e "$workdir" ]]; then
  echo "Refusing non-empty/existing probe destination: $workdir" >&2
  exit 2
fi

module_json="$(go mod download -json github.com/sagernet/sing-quic@v0.6.0)"
export MODULE_JSON="$module_json" PROBE_DIR="$workdir"
python3 - <<'PY'
import json
import os
import pathlib
import shutil

mod = json.loads(os.environ["MODULE_JSON"])
source = pathlib.Path(mod["Dir"])
dest = pathlib.Path(os.environ["PROBE_DIR"])
shutil.copytree(source, dest)
target = dest / "tuic" / "service_packet.go"
# Module cache sources are copied read-only by default.
target.chmod(target.stat().st_mode | 0o200)
old = target.read_text()
anchor = """func (s *serverSession[U]) handleUDPMessage(message *udpMessage, udpStream bool) {
	s.udpAccess.RLock()"""
replacement = """func (s *serverSession[U]) handleUDPMessage(message *udpMessage, udpStream bool) {
	// A pooled udpMessage may be released/reused before this session closes.
	// Capture the immutable session ID instead of retaining message itself.
	sessionID := message.sessionID
	s.udpAccess.RLock()"""
if old.count(anchor) != 1:
    raise RuntimeError("upstream function changed; abort unsafe backport")
start = old.index(anchor)
end = old.find("\n}", start)
if end < 0:
    raise RuntimeError("cannot locate TUIC handler end")
portion = old[start:end + 2]
if portion.count("message.sessionID") != 4:
    raise RuntimeError("unexpected upstream session ID references")
portion = portion.replace(anchor, replacement)
portion = portion.replace("message.sessionID", "sessionID")
# Restore the captured assignment after replacement.
portion = portion.replace("sessionID := sessionID", "sessionID := message.sessionID")
updated = old[:start] + portion + old[end + 2:]
if updated.count("sessionID := message.sessionID") != 1:
    raise RuntimeError("backport verification failed")
target.write_text(updated)
print("Applied isolated sing-quic v0.6.0 TUIC sessionID lifetime backport")
PY

go mod edit "-replace=github.com/sagernet/sing-quic=$workdir"
go mod tidy
echo "Local compatibility probe uses:"
go list -m github.com/sagernet/sing-quic github.com/sagernet/sing github.com/sagernet/quic-go
