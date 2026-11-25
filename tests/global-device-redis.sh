#!/usr/bin/env bash
# Integration tests against a disposable Redis instance.
# Requires redis-cli and a reachable Redis server on localhost:6379.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT/internal/globaladmission/redis_lease.lua"
PREFIX="ga-ci-${GITHUB_RUN_ID:-local}-$$"
KEY="$PREFIX:panel-a:user-7"
OTHER="$PREFIX:panel-b:user-7"

cleanup() {
  redis-cli -h 127.0.0.1 -p 6379 DEL "$KEY" "$OTHER" >/dev/null 2>&1 || true
}
trap cleanup EXIT
redis-cli -h 127.0.0.1 -p 6379 PING | grep -qx PONG

lease() {
  local key="$1" op="$2" owner="$3" ip="$4" limit="$5" ttl="$6"
  redis-cli --raw -h 127.0.0.1 -p 6379 --eval "$SCRIPT" "$key" , "$op" "$owner" "$ip" "$limit" "$ttl"
}
assert_first() {
  local want="$1"; shift
  local got
  got="$("$@" | head -n 1)"
  if [[ "$got" != "$want" ]]; then
    printf 'FAIL: want %s, got %s: %q\n' "$want" "$got" "$*" >&2
    exit 1
  fi
}

# Two owners on different nodes sharing an IP use one distinct slot.
assert_first 1 lease "$KEY" acquire "node-a:1" "192.0.2.1" 1 30000
assert_first 1 lease "$KEY" acquire "node-b:1" "192.0.2.1" 1 30000
assert_first 0 lease "$KEY" acquire "node-c:1" "192.0.2.2" 1 30000

# One node's release cannot release another node's ownership.
assert_first 1 lease "$KEY" release "node-a:1" "" 1 30000
assert_first 0 lease "$KEY" acquire "node-c:1" "192.0.2.2" 1 30000
assert_first 1 lease "$KEY" renew "node-b:1" "" 1 30000
assert_first 1 lease "$KEY" release "node-b:1" "" 1 30000
assert_first 1 lease "$KEY" acquire "node-c:1" "192.0.2.2" 1 30000
assert_first 0 lease "$KEY" acquire "node-c:1" "192.0.2.3" 1 30000

# Account isolation.
assert_first 1 lease "$OTHER" acquire "node-a:1" "192.0.2.3" 1 30000

# Short TTL expires on the Redis clock; expired lease may not be renewed.
assert_first 1 lease "$KEY" release "node-c:1" "" 1 30000
assert_first 1 lease "$KEY" acquire "node-a:short" "192.0.2.1" 1 100
sleep 0.25
assert_first 0 lease "$KEY" renew "node-a:short" "" 1 1000
assert_first 1 lease "$KEY" acquire "node-b:new" "192.0.2.2" 1 30000
assert_first 1 lease "$KEY" release "node-b:new" "" 1 30000

# Concurrent attempts must produce exactly two accepted distinct IPs.
export PREFIX SCRIPT
seq 1 60 | xargs -P 16 -I '{}' bash -c '
  n="$1"
  redis-cli --raw -h 127.0.0.1 -p 6379 --eval "$SCRIPT" "$PREFIX:panel-a:user-7" , acquire "parallel:$n" "198.51.100.$n" 2 30000 | head -n 1
' _ '{}' > "/tmp/global-device-redis-results-$$"
accepted="$(grep -cx 1 "/tmp/global-device-redis-results-$$" || true)"
rm -f "/tmp/global-device-redis-results-$$"
if [[ "$accepted" != 2 ]]; then
  echo "FAIL: expected 2 concurrent grants, got $accepted" >&2
  exit 1
fi

echo "PASS: Redis Lua device lease integration"
