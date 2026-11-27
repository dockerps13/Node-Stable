#!/usr/bin/env bash
# v1.0.5 dedicated-staging continuous acceptance collector.
# Only run on a staging host: this is not a standalone traffic generator.
# Required probes are executable scripts that test REAL staging panel/dual-node
# admission and protocol traffic. Missing probes fail closed.
set -euo pipefail
usage() {
 cat <<'EOF'
Usage: sudo-free env:
  STAGING_SENTINEL=/etc/xboard-node/STAGING-ONLY
  STAGING_REDIS_URL=redis://127.0.0.1:6379/15
  STAGING_HEALTH_URL=http://127.0.0.1:18555/healthz
  STAGING_ADMISSION_PROBE=/secure/path/admission-check.sh
  STAGING_TRAFFIC_PROBE=/secure/path/traffic-check.sh
  STAGING_NODE_UNITS="xboard-node-stage-a xboard-node-stage-b"
  STAGING_DURATION_SECONDS=86400  (minimum 86400)
  STAGING_INTERVAL_SECONDS=60
  STAGING_OUTPUT_DIR=/var/tmp/xboard-v105-stage-evidence-new
  bash tests/staging-24h-collector.sh

Each probe must return nonzero on auth/traffic failures and be safe to run
repeatedly. No secrets or tokens may be printed. This runner never sets up
credentials, changes service units, or injects load by itself.
EOF
}
die(){ echo "ERROR: $*" >&2; exit 2; }
[[ "${1:-}" != "--help" ]] || { usage; exit 0; }
[[ "$(id -u)" -ne 0 ]] || die "run unprivileged; no root"
[[ "${STAGING_SENTINEL:-}" == /etc/xboard-node/STAGING-ONLY ]] || die "explicit staging sentinel path required"
[[ -f "$STAGING_SENTINEL" ]] || die "staging sentinel file absent"
[[ "$(cat "$STAGING_SENTINEL")" == "I_CONFIRM_DEDICATED_STAGING_NOT_PRODUCTION" ]] || die "incorrect staging sentinel contents"
[[ "${STAGING_REDIS_URL:-}" == redis://127.0.0.1:* ]] || die "use only loopback staging Redis URL"
[[ "${STAGING_HEALTH_URL:-}" == http://127.0.0.1:* ]] || die "health endpoint must bind loopback"
for var in STAGING_ADMISSION_PROBE STAGING_TRAFFIC_PROBE; do
  probe="${!var:-}"
  [[ -n "$probe" && "$probe" = /* && -f "$probe" && -x "$probe" ]] || die "$var missing absolute executable file"
done
[[ -n "${STAGING_NODE_UNITS:-}" ]] || die "two staging systemd unit names required"
read -ra units <<< "$STAGING_NODE_UNITS"
[[ "${#units[@]}" -eq 2 && "${units[0]}" != "${units[1]}" ]] || die "exactly two distinct staging units required"
for unit in "${units[@]}"; do
  [[ "$unit" =~ ^xboard-node-stage-[a-zA-Z0-9_-]+(\.service)?$ ]] || die "unsafe staging unit: $unit"
done
duration="${STAGING_DURATION_SECONDS:-86400}"
interval="${STAGING_INTERVAL_SECONDS:-60}"
[[ "$duration" =~ ^[0-9]+$ && "$interval" =~ ^[0-9]+$ ]] || die "duration/interval must be integer"
((duration >= 86400 && duration <= 172800)) || die "continuous acceptance must span at least 24h, at most 48h"
((interval >= 15 && interval <= 300)) || die "interval must be between 15 and 300 seconds"
[[ -n "${STAGING_OUTPUT_DIR:-}" && "$STAGING_OUTPUT_DIR" == /var/tmp/xboard-v105-stage-* ]] || die "new /var/tmp/xboard-v105-stage-* directory required"
[[ ! -e "$STAGING_OUTPUT_DIR" ]] || die "refuse existing evidence directory"
command -v redis-cli >/dev/null || die "redis-cli missing"
command -v curl >/dev/null || die "curl missing"
command -v systemctl >/dev/null || die "systemctl missing"
command -v timeout >/dev/null || die "timeout missing"
umask 077
mkdir -m 700 "$STAGING_OUTPUT_DIR"
out="$(realpath "$STAGING_OUTPUT_DIR")"
printf 'utc,elapsed_s,iteration,health_http,health_ms,redis_used_memory,redis_clients,redis_evictions,node_a_state,node_b_state,admission,traffic\n' > "$out/measurements.csv"
printf 'starting_utc=%s\nduration_s=%s\ninterval_s=%s\ncommit=%s\n' "$(date -u +%FT%TZ)" "$duration" "$interval" "${STAGING_CANDIDATE_COMMIT:-unrecorded}" > "$out/context.txt"
redis-cli -u "$STAGING_REDIS_URL" PING | grep -qx PONG || die "isolated Redis ping failed"
started="$(date +%s)"
deadline=$((started + duration))
fail=0
iteration=0
# Avoid set -e masking measurements on failures; instead record failure state.
while (( $(date +%s) < deadline )); do
  iteration=$((iteration + 1))
  utc="$(date -u +%FT%TZ)"
  status="$(curl --noproxy '*' -sS -o /dev/null --connect-timeout 2 --max-time 10 -w '%{http_code},%{time_total}' "$STAGING_HEALTH_URL" 2>/dev/null || printf '000,0')"
  http_code="${status%%,*}"
  http_time="${status#*,}"
  [[ "$http_code" == 200 ]] || fail=1
  info="$(redis-cli -u "$STAGING_REDIS_URL" INFO 2>/dev/null | tr -d '\r' || true)"
  field(){ printf '%s\n' "$info" | awk -F: -v k="$1" '$1==k {print $2;exit}'; }
  mem="$(field used_memory)"
  clients="$(field connected_clients)"
  evictions="$(field evicted_keys)"
  [[ -n "$mem" && -n "$clients" && -n "$evictions" && "$evictions" == 0 ]] || fail=1
  states=()
  for unit in "${units[@]}"; do
    st="$(systemctl is-active "$unit" 2>/dev/null || true)"
    states+=("$st")
    [[ "$st" == active ]] || fail=1
  done
  admission=PASS
  timeout 30 "$STAGING_ADMISSION_PROBE" > /dev/null 2>>"$out/probe-failures.log" || { admission=FAIL; fail=1; }
  traffic=PASS
  timeout 30 "$STAGING_TRAFFIC_PROBE" > /dev/null 2>>"$out/probe-failures.log" || { traffic=FAIL; fail=1; }
  printf '%s,%d,%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' "$utc" "$(( $(date +%s) - started ))" "$iteration" "$http_code" "$http_time" "${mem:-NA}" "${clients:-NA}" "${evictions:-NA}" "${states[0]}" "${states[1]}" "$admission" "$traffic" >> "$out/measurements.csv"
  sleep "$interval"
done
ended="$(date +%s)"
printf 'finished_utc=%s\nelapsed_seconds=%s\niterations=%d\nfailed=%d\n' "$(date -u +%FT%TZ)" "$((ended - started))" "$iteration" "$fail" >> "$out/context.txt"
(cd "$out"; sha256sum measurements.csv context.txt > SHA256SUMS)
if ((fail != 0)); then
  echo "FAIL: staging collector found availability, eviction, admission or transport errors. Evidence: $out" >&2
  exit 1
fi
echo "PASS: 24h staging collector with all configured live probes successful. Evidence: $out"
echo "NOTE: review probe implementations, system sizing and authorization scenarios before release signoff"
