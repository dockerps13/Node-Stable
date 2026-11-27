#!/usr/bin/env bash
# Destructive staging-only real-service rehearsal. NEVER run on production.
# Restores the original binary at the end, including on failures/signals.
set -euo pipefail
die(){ echo "ERROR: $*" >&2; exit 2; }
[[ "${1:-}" != "--help" ]] || {
  echo 'Set STAGING_SENTINEL, STAGING_NODE_UNIT, STAGING_CANDIDATE_BIN, STAGING_HEALTH_PROBE, STAGING_ADMISSION_PROBE, STAGING_TRAFFIC_PROBE, STAGING_EVIDENCE_DIR'; exit 0;
}
[[ "$(id -u)" != 0 ]] || die "run as unprivileged staging operator with sudo -n"
[[ "${STAGING_SENTINEL:-}" == /etc/xboard-node/STAGING-ONLY ]] || die "staging marker required"
[[ -f "$STAGING_SENTINEL" ]] || die "marker not found"
[[ "$(cat "$STAGING_SENTINEL")" == I_CONFIRM_DEDICATED_STAGING_NOT_PRODUCTION ]] || die "staging marker mismatch"
unit="${STAGING_NODE_UNIT:-}"
[[ "$unit" =~ ^xboard-node-stage-[a-zA-Z0-9_-]+(\.service)?$ ]] || die "unit must have staging prefix"
bin=/opt/xboard-node-staging/bin/xboard-node
[[ -x "$bin" ]] || die "dedicated staging binary missing at $bin"
candidate="${STAGING_CANDIDATE_BIN:-}"
[[ "$candidate" == /* && -x "$candidate" ]] || die "candidate must be executable at absolute path"
[[ "$(realpath "$candidate")" != "$(realpath "$bin")" ]] || die "candidate must differ from installed binary path"
stage="${STAGING_EVIDENCE_DIR:-}"
[[ "$stage" == /var/tmp/xboard-v105-stage-rollback-* && ! -e "$stage" ]] || die "use a new staging evidence directory"
for v in STAGING_HEALTH_PROBE STAGING_ADMISSION_PROBE STAGING_TRAFFIC_PROBE; do
  p="${!v:-}"
  [[ "$p" == /* && -x "$p" && -f "$p" ]] || die "missing executable $v"
done
command -v sudo >/dev/null || die "sudo missing"
sudo -n true || die "noninteractive sudo permission required on dedicated staging host"
# Guard unit wiring, not just its name. The designated staging service must
# execute the dedicated staging binary; reject relative/shared production paths.
exec_start="$(systemctl show "$unit" -p ExecStart --value)"
[[ "$exec_start" == *"$bin"* ]] || die "unit ExecStart must point to $bin"
[[ "$(systemctl is-active "$unit")" == active ]] || die "staging unit is not active"
umask 077
mkdir -m 700 "$stage"
stage="$(realpath "$stage")"
saved="$stage/original-xboard-node"
cp "$bin" "$saved"
sha256sum "$saved" > "$stage/original.sha256"
printf 'unit=%s\nstarted_utc=%s\ncandidate_sha256=%s\n' "$unit" "$(date -u +%FT%TZ)" "$(sha256sum "$candidate" | awk '{print $1}')" > "$stage/evidence.txt"

restore() {
  original_exit="$1"
  trap - EXIT
  set +e
  sudo -n install -m 755 "$saved" "$bin.rollback"
  install_rc=$?
  if (( install_rc == 0 )); then
    sudo -n mv -f "$bin.rollback" "$bin"
    install_rc=$?
  fi
  sudo -n systemctl reset-failed "$unit"
  sudo -n systemctl restart "$unit"
  restart_rc=$?
  cmp -s "$bin" "$saved"
  cmp_rc=$?
  systemctl is-active --quiet "$unit"
  active_rc=$?
  check_rc=0
  if (( restart_rc == 0 && active_rc == 0 )); then
    timeout 20 "$STAGING_HEALTH_PROBE" >/dev/null && \
    timeout 30 "$STAGING_ADMISSION_PROBE" >/dev/null && \
    timeout 30 "$STAGING_TRAFFIC_PROBE" >/dev/null || check_rc=1
  else
    check_rc=1
  fi
  printf 'rollback_utc=%s\\nrollback_binary_match=%s\\nrollback_service_restart=%s\\nrollback_probe_result=%s\\n' \
    "$(date -u +%FT%TZ)" "$([ "$cmp_rc" -eq 0 ] && echo true || echo false)" \
    "$([ "$restart_rc" -eq 0 ] && echo PASS || echo FAIL)" \
    "$([ "$check_rc" -eq 0 ] && echo PASS || echo FAIL)" >> "$stage/evidence.txt"
  (cd "$stage" && sha256sum original-xboard-node evidence.txt > SHA256SUMS)
  if (( install_rc != 0 || restart_rc != 0 || cmp_rc != 0 || active_rc != 0 || check_rc != 0 )); then
    echo "FAIL: baseline recovery incomplete; inspect dedicated staging host" >&2
    exit 1
  fi
  if (( original_exit != 0 )); then
    echo "FAIL: exercise failed; baseline restored" >&2
    exit "$original_exit"
  fi
  echo "PASS: baseline binary restored; live staging service, admission and protocol traffic recovered"
  exit 0
}
trap 'restore "$?"' EXIT

check() {
  phase="$1"
  systemctl is-active --quiet "$unit" || { echo "$phase service inactive" >&2; return 1; }
  timeout 20 "$STAGING_HEALTH_PROBE" >/dev/null || { echo "$phase health probe failure" >&2; return 1; }
  timeout 30 "$STAGING_ADMISSION_PROBE" >/dev/null || { echo "$phase admission probe failure" >&2; return 1; }
  timeout 30 "$STAGING_TRAFFIC_PROBE" >/dev/null || { echo "$phase traffic probe failure" >&2; return 1; }
  printf '%s_utc=%s\n%s_status=PASS\n' "$phase" "$(date -u +%FT%TZ)" "$phase" >> "$stage/evidence.txt"
}
check baseline
# Atomic same-filesystem replacement and real systemd restart.
sudo -n install -m 755 "$candidate" "$bin.next"
sudo -n mv -f "$bin.next" "$bin"
sudo -n systemctl restart "$unit"
check candidate
# Staging-only fault injection. This intentionally interrupts the staging node.
printf '#!/bin/sh\nexit 42\n' > "$stage/injected-failure"
chmod 755 "$stage/injected-failure"
sudo -n install -m 755 "$stage/injected-failure" "$bin.next"
sudo -n mv -f "$bin.next" "$bin"
# Type=simple systemd units can return success from systemctl restart
# before the executable has exited. Confirm the injected exit status instead
# of treating restart's return code as evidence of a working service.
sudo -n systemctl restart "$unit" || true
injected_failure_observed=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if [[ "$(systemctl show "$unit" -p ExecMainStatus --value)" == 42 ]]; then
    injected_failure_observed=true
    break
  fi
  sleep 1
done
[[ "$injected_failure_observed" == true ]] || die "injected failure exit 42 was not observed; refusing false PASS"
printf 'fault_detected_utc=%s\nfault_exit_status=42\n' "$(date -u +%FT%TZ)" >> "$stage/evidence.txt"
# Trigger exit trap for deterministic automatic rollback.
echo "PASS: candidate live staging checks and injected start failure confirmed; restoring baseline now"
