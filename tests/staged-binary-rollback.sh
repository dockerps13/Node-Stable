#!/usr/bin/env bash
# Offline packaged-binary replacement and rollback check.
# All files live beneath the caller-provided temporary staging directory.
# This does not access systemd, production configuration, databases or Redis.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <baseline-bin-dir> <candidate-bin-dir> <staging-dir>" >&2
  exit 2
fi

baseline="$(realpath "$1")"
candidate="$(realpath "$2")"
staging="$(realpath -m "$3")"

if [[ ! -d "$baseline" || ! -d "$candidate" ]]; then
  echo "baseline/candidate binary directories must exist" >&2
  exit 2
fi
if [[ -e "$staging" ]]; then
  echo "staging path must not exist (avoid overwriting): $staging" >&2
  exit 2
fi
case "$staging" in
  /|/etc|/usr|/usr/local|/var|/root|/home)
    echo "unsafe staging path" >&2; exit 2;;
esac

mkdir -m 700 -p "$staging"/{active,backup}
for bin in xboard-node-linux-amd64 xbctl-linux-amd64; do
  [[ -x "$baseline/$bin" && -x "$candidate/$bin" ]] || {
    echo "missing executable $bin" >&2; exit 1;
  }
  cp "$baseline/$bin" "$staging/active/$bin"
  cp "$staging/active/$bin" "$staging/backup/$bin"
done

# Confirm the baseline binaries can execute before replacement.
"$staging/active/xboard-node-linux-amd64" -v
"$staging/active/xbctl-linux-amd64" version

(
  cd "$staging/backup"
  sha256sum xboard-node-linux-amd64 xbctl-linux-amd64 > "$staging/baseline.sha256"
)

# Replace both staged binaries without touching installed executables.
for bin in xboard-node-linux-amd64 xbctl-linux-amd64; do
  cp "$candidate/$bin" "$staging/active/$bin.next"
  mv -f "$staging/active/$bin.next" "$staging/active/$bin"
done
"$staging/active/xboard-node-linux-amd64" -v
"$staging/active/xbctl-linux-amd64" version
(
  cd "$staging/active"
  sha256sum xboard-node-linux-amd64 xbctl-linux-amd64 > "$staging/candidate.sha256"
)
(
  cd "$candidate"
  sha256sum -c "$staging/candidate.sha256"
)

# Restore and verify bit-for-bit identical baseline files.
for bin in xboard-node-linux-amd64 xbctl-linux-amd64; do
  cp "$staging/backup/$bin" "$staging/active/$bin.next"
  mv -f "$staging/active/$bin.next" "$staging/active/$bin"
done
(
  cd "$staging/active"
  sha256sum -c "$staging/baseline.sha256"
)
"$staging/active/xboard-node-linux-amd64" -v
"$staging/active/xbctl-linux-amd64" version
echo "PASS: isolated baseline -> candidate -> baseline binary rollback"
echo "NOTE: no service, panel, Redis, real traffic or schema migration tested"
