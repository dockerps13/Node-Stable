#!/usr/bin/env bash
# Isolated Redis + HTTP client soak. Requires disposable Redis at 127.0.0.1:6379.
# This exercises the Go machine-client HTTP test-double backed by real Redis,
# then measures the Redis Lua lease path and captures process resource usage.
# It is NOT a real Laravel panel or an Xray MUX/XUDP traffic test.
set -euo pipefail

duration="${1:-180}"
outdir="${2:-soak-evidence}"
case "$duration" in
  ''|*[!0-9]*) echo "duration must be integer seconds" >&2; exit 2;;
esac
if (( duration < 60 || duration > 18000 )); then
  echo "duration must be 60..18000 seconds" >&2; exit 2
fi
mkdir -p "$outdir"
outdir="$(realpath "$outdir")"
printf 'time_utc,cycle,used_memory,used_memory_peak,connected_clients,expired_keys,evicted_keys\n' > "$outdir/redis-resources.csv"
printf 'cycle,elapsed_seconds,result\n' > "$outdir/http-redis-cycles.csv"
redis-cli -h 127.0.0.1 -p 6379 ping | grep -qx PONG
export GLOBAL_ADMISSION_REDIS_INTEGRATION=1
started="$(date +%s)"
deadline=$((started + duration))
cycle=0
while (( $(date +%s) < deadline )); do
  cycle=$((cycle+1))
  cycle_start="$(date +%s)"
  # A real HTTP client, two machine tokens and two independently spawned
  # client processes share the same *test* Redis-backed authority.
  go test -count=1 -timeout=90s ./internal/globaladmission -run '^TestTwoMachineClientsHTTPRedisAuthority$|^TestTwoIndependentNodeProcesses$' \
    > "$outdir/http-redis-cycle-${cycle}.log" 2>&1 || {
      cat "$outdir/http-redis-cycle-${cycle}.log" >&2
      printf '%d,%d,FAIL\n' "$cycle" "$(( $(date +%s) - cycle_start ))" >> "$outdir/http-redis-cycles.csv"
      exit 1
    }
  printf '%d,%d,PASS\n' "$cycle" "$(( $(date +%s) - cycle_start ))" >> "$outdir/http-redis-cycles.csv"
  python3 tests/redis-lease-benchmark.py --duration 5 --workers 12 --cardinalities 32,256 \
    --output "$outdir/lease-cycle-${cycle}.json" > "$outdir/lease-cycle-${cycle}.log"
  info="$(redis-cli -h 127.0.0.1 -p 6379 info | tr -d '\r')"
  field() { printf '%s\n' "$info" | awk -F: -v key="$1" '$1 == key {print $2; exit}'; }
  printf '%s,%d,%s,%s,%s,%s,%s\n' "$(date -u +%FT%TZ)" "$cycle" \
    "$(field used_memory)" "$(field used_memory_peak)" "$(field connected_clients)" \
    "$(field expired_keys)" "$(field evicted_keys)" >> "$outdir/redis-resources.csv"
  echo "PASS cycle=$cycle elapsed=$(( $(date +%s) - started )) sec" 
done
echo "PASS: Redis/HTTP test-double soak cycles=$cycle duration=$(( $(date +%s) - started )) sec"
echo "NOT TESTED: real XBoard Laravel PHP, Xray MUX/XUDP, deployed systemd, 24-hour continuous operation"
