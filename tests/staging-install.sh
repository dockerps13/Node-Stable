#!/usr/bin/env bash
# ONLY for a NEW disposable Debian 12 staging VPS.
# Refuses known production identities and existing xboard-node installations.
set -Eeuo pipefail
umask 077
fail() { echo "FAIL: $*" >&2; exit 1; }
usage() {
  cat <<'EOF'
Usage: bash staging-install.sh --panel https://your-independent-staging-panel.example --machine-id NEW_STAGING_ID
Run on the NEW Debian 12 staging VPS as root. A staging-only token is read
privately from the terminal. No production panel/ID or existing installation.
EOF
}
panel=""; machine_id=""
while (($#)); do
  case "$1" in
    --panel) (($# >= 2)) || fail "missing --panel"; panel="$2"; shift 2;;
    --machine-id) (($# >= 2)) || fail "missing --machine-id"; machine_id="$2"; shift 2;;
    --help|-h) usage; exit 0;;
    *) fail "unknown option $1";;
  esac
done
(( EUID == 0 )) || fail "run as root only on your isolated test VPS"
[[ -t 0 ]] || fail "an interactive SSH terminal is required to enter staging token"
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$here"
test -f STAGING_ONLY.txt || fail "not an isolated staging candidate package"
test -f SOURCE_COMMIT || fail "missing candidate commit metadata"
test -f SHA256SUMS || fail "missing checksum manifest"
[[ "$(cat SOURCE_COMMIT)" =~ ^[a-f0-9]{40}$ ]] || fail "invalid source commit"
bash "$here/staging-preflight.sh" --panel "$panel" --machine-id "$machine_id"
sha256sum -c SHA256SUMS || fail "candidate integrity check failed"
case "$(uname -m)" in
  x86_64) arch=amd64;;
  aarch64) arch=arm64;;
  *) fail "only Linux amd64 / arm64 candidates are supported";;
esac
binary="$here/xboard-node-linux-$arch"
cli="$here/xbctl-linux-$arch"
test -s "$binary" || fail "missing matching $arch binary"
test -s "$cli" || fail "missing matching $arch CLI"
chmod 700 "$binary" "$cli"
"$binary" -v | grep -F 'v1.0.4' >/dev/null || fail "not a v1.0.4 candidate binary"
echo "Candidate: $(cat SOURCE_COMMIT)"
echo "IMPORTANT: this modifies ONLY the new test VPS and requires an isolated XBoard panel."
read -rsp 'STAGING-ONLY machine token (not production token): ' staging_token
echo
[[ -n "$staging_token" ]] || fail "empty token"
trap 'unset staging_token' EXIT
bash "$here/install.sh" install \
  --mode machine \
  --panel "$panel" \
  --machine-id "$machine_id" \
  --token "$staging_token" \
  --version v1.0.4 \
  --binary "$binary" \
  --xbctl-binary "$cli"
echo "Staging candidate installed. Check real panel reports, protocol traffic and rollback before merge/release."
