#!/usr/bin/env bash
# Non-destructive 24h observation of a currently running v1.0.5 node.
# Does NOT validate real authenticated transport, global device admission,
# two-node behavior, fault injection, or the release 24h staging gate.
set -Eeuo pipefail
umask 077

if (( EUID != 0 )); then echo "Run as root for access to systemd and process metrics." >&2; exit 2; fi
command -v systemctl >/dev/null && command -v journalctl >/dev/null && command -v ss >/dev/null || exit 2
systemctl is-active --quiet xboard-node || { echo "node inactive"; exit 2; }
version=$(/usr/local/bin/xboard-node -v 2>&1)
[[ "$version" == *"ec3b009"* ]] || { echo "Unexpected binary version: $version" >&2; exit 2; }
base=/root/xboard-v105-test
[[ -d "$base" ]] || { echo "Missing $base" >&2; exit 2; }
out="$base/single-node-observe-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 700 "$out"
echo "$out" > "$base/latest-single-node-observe.path"
csv="$out/samples.csv"
printf 'utc,elapsed_s,pid,restarts,rss_kib,cpu_percent,fd_count,tcp_listeners,udp_listeners,panel_reports_3m,critical_errors_3m,healthy\n' > "$csv"
start=$(date +%s)
end=$((start+86400))
initial_pid=$(systemctl show xboard-node -p MainPID --value)
initial_restarts=$(systemctl show xboard-node -p NRestarts --value)
printf 'start_utc=%s\nversion=%s\ninitial_pid=%s\ninitial_restarts=%s\ntarget_duration_seconds=86400\nscope=SINGLE_NODE_READ_ONLY_OBSERVATION\n' "$(date -u +%FT%TZ)" "$version" "$initial_pid" "$initial_restarts" > "$out/context.txt"
tcp_ports=(443 1443 6443 8080 8443 9443 8833 9081 8123 7334 5443)
udp_ports=(8080 8443 9443)
fail=0
samples=0
failures=0
max_rss=0
max_fds=0
# Limit the report file to small summary lines; journal output stays on host.
while (( $(date +%s) < end )); do
  now=$(date +%s)
  elapsed=$((now-start))
  utc=$(date -u +%FT%TZ)
  healthy=1
  pid=$(systemctl show xboard-node -p MainPID --value 2>/dev/null || echo 0)
  restarts=$(systemctl show xboard-node -p NRestarts --value 2>/dev/null || echo -1)
  [[ "$pid" =~ ^[0-9]+$ ]] || pid=0
  [[ "$restarts" =~ ^[0-9]+$ ]] || restarts=-1
  if ! systemctl is-active --quiet xboard-node || ((pid==0)) ||
     [[ "$pid" != "$initial_pid" ]] || [[ "$restarts" != "$initial_restarts" ]]; then
    healthy=0
  fi
  rss=0
  cpu=0
  fds=0
  if ((pid>0)) && [[ -r "/proc/$pid/status" ]]; then
    rss=$(awk '/^VmRSS:/ {print $2; exit}' "/proc/$pid/status")
    rss=${rss:-0}
    cpu=$(ps -p "$pid" -o %cpu= 2>/dev/null | tr -d ' ' || echo 0)
    fds=$(find "/proc/$pid/fd" -mindepth 1 -maxdepth 1 -printf '.\n' 2>/dev/null | wc -l)
  else
    healthy=0
  fi
  ((rss>max_rss)) && max_rss=$rss
  ((fds>max_fds)) && max_fds=$fds
  socket_snapshot=$(ss -H -lntup 2>/dev/null || true)
  tcp_ok=0
  for port in "${tcp_ports[@]}"; do
    if printf '%s\n' "$socket_snapshot" | awk -v p="$port" '$1=="tcp" && $5 ~ (":"p"$") {found=1} END{exit !found}'; then
      tcp_ok=$((tcp_ok+1))
    fi
  done
  udp_ok=0
  for port in "${udp_ports[@]}"; do
    if printf '%s\n' "$socket_snapshot" | awk -v p="$port" '$1=="udp" && $5 ~ (":"p"$") {found=1} END{exit !found}'; then
      udp_ok=$((udp_ok+1))
    fi
  done
  [[ "$tcp_ok" -eq 11 && "$udp_ok" -eq 3 ]] || healthy=0

  reports=0
  errors=0
  if ((elapsed>=180)); then
    recent=$(journalctl -u xboard-node --since '3 minutes ago' --no-pager -o cat 2>/dev/null || true)
    for port in "${tcp_ports[@]}"; do
      if printf '%s\n' "$recent" | grep -F 'journal report tick succeeded' | grep -Fq ":$port]"; then
        reports=$((reports+1))
      fi
    done
    [[ "$reports" -eq 11 ]] || healthy=0
    errors=$(printf '%s\n' "$recent" | grep -Eic 'panic:|fatal error:|DATA RACE|journal report tick failed' || true)
    [[ "$errors" -eq 0 ]] || healthy=0
  fi
  printf '%s,%d,%s,%s,%s,%s,%s,%d,%d,%d,%d,%d\n' "$utc" "$elapsed" "$pid" "$restarts" "$rss" "$cpu" "$fds" "$tcp_ok" "$udp_ok" "$reports" "$errors" "$healthy" >> "$csv"
  samples=$((samples+1))
  if ((healthy==0)); then
    fail=1
    failures=$((failures+1))
    printf '%s elapsed=%ss pid=%s restarts=%s TCP=%s/11 UDP=%s/3 reports=%s/11 errors=%s\n' "$utc" "$elapsed" "$pid" "$restarts" "$tcp_ok" "$udp_ok" "$reports" "$errors" >> "$out/failures.log"
  fi
  remain=$((end-$(date +%s)))
  if ((remain>60)); then sleep 60; elif ((remain>0)); then sleep "$remain"; fi
done
elapsed_total=$(($(date +%s)-start))
printf 'finish_utc=%s\nelapsed_seconds=%s\nsamples=%s\nfailed_samples=%s\nmax_rss_kib=%s\nmax_fd_count=%s\n' "$(date -u +%FT%TZ)" "$elapsed_total" "$samples" "$failures" "$max_rss" "$max_fds" >> "$out/context.txt"
(cd "$out"; sha256sum context.txt samples.csv > SHA256SUMS)
if ((fail==0 && elapsed_total>=86400)); then
  echo "RESULT: 24H_SINGLE_NODE_OBSERVATION_PASS"
else
  echo "RESULT: 24H_SINGLE_NODE_OBSERVATION_FAIL"
fi
echo "Evidence directory: $out"
echo "NOT RUN: real protocol authenticated traffic, global cross-node lease load, production-equivalent staging, injected systemd rollback."
((fail==0 && elapsed_total>=86400))
