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
  # Always restore original on exit. Never leave the injected fault installed.
  set +e
  sudo -n install -m 755 "$saved" "$bin.rollback"
  sudo -n mv -f "$bin.rollback" "$bin"
  sudo -n systemctl reset-failed "$unit"
  sudo -n systemctl restart "$unit"
  cmp -s "$bin" "$saved"
  ok=$?
  printf 'rollback_utc=%s\nrollback_binary_match=%s\n' "$(date -u +%FT%TZ)" "$([ "$ok" -eq 0 ] && echo true || echo false)" >> "$stage/evidence.txt"
}
trap restore EXIT

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
if sudo -n systemctl restart "$unit"; then
  die "the deliberately bad executable unexpectedly restarted"
fi
printf 'fault_detected_utc=%s\n' "$(date -u +%FT%TZ)" >> "$stage/evidence.txt"
# Trigger exit trap for deterministic automatic rollback.
echo "PASS: candidate live staging checks and injected start failure confirmed; restoring baseline now"
