#!/usr/bin/env bash
# Read-only guard. Run only on a NEW, disposable Debian 11 or 12 staging VPS.
set -Eeuo pipefail
fail(){ echo "FAIL: $*" >&2; exit 1; }
panel=""; machine_id=""
while (($#)); do
  case "$1" in
    --panel) (($#>=2)) || fail "missing --panel value"; panel="$2"; shift 2;;
    --machine-id) (($#>=2)) || fail "missing --machine-id value"; machine_id="$2"; shift 2;;
    --help|-h) echo "Usage: bash staging-preflight.sh --panel https://staging-panel.example --machine-id STAGING_ID"; exit 0;;
    *) fail "unknown argument $1";;
  esac
done
[[ "$machine_id" =~ ^[1-9][0-9]*$ ]] || fail "machine ID must be positive"
[[ "$machine_id" != "24" ]] || fail "production machine ID 24 is forbidden"
[[ "$panel" =~ ^https://[^/[:space:]]+/?$ ]] || fail "use HTTPS staging panel origin (no path/query)"
host="${panel#https://}"; host="${host%/}"; host="${host%%:*}"
host="${host,,}"
[[ "$host" != "soulook.org" && "$host" != *.soulook.org ]] || fail "production panel domain is forbidden"
[[ -r /etc/os-release ]] || fail "cannot detect Debian system"
. /etc/os-release
[[ "${ID:-}" == debian && ( "${VERSION_ID:-}" == 11 || "${VERSION_ID:-}" == 12 ) ]] || fail "requires an isolated Debian 11 or 12 staging VPS"
if [[ "${VERSION_ID}" == 11 ]]; then
  echo "WARN: Debian 11 standard LTS ended 2026-08-31. Restrict access and use only for short-lived isolated staging." >&2
fi
if command -v systemctl >/dev/null; then
  if systemctl is-active --quiet xboard-node 2>/dev/null; then
    fail "xboard-node already running: never replace a current installation"
  fi
fi
[[ ! -e /etc/xboard-node/config.yml ]] || fail "existing xboard-node config found"
[[ ! -e /etc/xboard-node/credentials.env ]] || fail "existing credentials found"
[[ ! -e /etc/xboard-node/instances ]] || fail "existing instance data found"
printf 'PASS: Debian %s, staging machine ID %s, non-production panel host %s\n' "$VERSION_ID" "$machine_id" "$host"
echo "PASS: no active xboard-node service or node installation data found"
echo "NOTE: this is NOT proof panel/database/users are isolated; verify those separately before install."
