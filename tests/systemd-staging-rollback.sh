#!/usr/bin/env bash
# Disposable systemd supervisor drill for pre-release binary lifecycle.
# Runs real xboard-node -v under systemd, NOT an authenticated node service.
# Never touches /usr/local/bin, /etc/xboard-node, or existing systemd units.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <baseline-binary> <candidate-binary> <new-staging-dir>" >&2
  exit 2
fi
baseline="$(realpath "$1")"
candidate="$(realpath "$2")"
staging="$(realpath -m "$3")"
[[ -x "$baseline" && -x "$candidate" ]] || { echo "missing executable" >&2; exit 2; }
[[ ! -e "$staging" && "$staging" == /home/runner/work/_temp/* ]] || {
  echo "stage must be a new GitHub runner temporary directory" >&2; exit 2;
}
[[ "$staging" != *' '* ]] || { echo "staging path contains spaces" >&2; exit 2; }
[[ "$(id -u)" != 0 ]] || { echo "run as unprivileged runner, not root" >&2; exit 2; }
[[ "$(ps -p 1 -o comm=)" == systemd ]] || {
  echo "systemd is not running as PID 1; cannot claim systemd acceptance" >&2; exit 1;
}
suffix="${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
[[ "$suffix" =~ ^[0-9]+-[0-9]+$ ]] || { echo "unsafe unit suffix" >&2; exit 2; }
unit="v105-accept-${suffix}.service"
unit_file="/run/systemd/system/$unit"
[[ ! -e "$unit_file" ]] || { echo "systemd unit already exists" >&2; exit 2; }

mkdir -m 700 -p "$staging"
cp "$baseline" "$staging/xboard-node"
cp "$baseline" "$staging/old-xboard-node"
chmod 755 "$staging/xboard-node"
(
  cd "$staging"
  sha256sum old-xboard-node > baseline.sha256
)
cleanup() {
  sudo systemctl stop "$unit" >/dev/null 2>&1 || true
  sudo systemctl reset-failed "$unit" >/dev/null 2>&1 || true
  sudo rm -f "$unit_file"
  sudo systemctl daemon-reload >/dev/null 2>&1 || true
}
trap cleanup EXIT
# Dedicated disposable Type=oneshot service: actual xboard-node executable
# launches and returns version without connecting to live services.
cat > "$staging/unit" <<EOF
[Unit]
Description=Temporary v1.0.5 binary supervision and rollback acceptance
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$staging/xboard-node -v
StandardOutput=journal
StandardError=journal
EOF
sudo install -m 644 "$staging/unit" "$unit_file"
sudo systemctl daemon-reload

check_start() {
  sudo systemctl start "$unit"
  sudo systemctl is-active --quiet "$unit"
  sudo systemctl show "$unit" -p ExecMainStatus -p Result -p ActiveState \
    | tee -a "$staging/systemd-evidence.log"
}
echo "baseline_start" | tee -a "$staging/systemd-evidence.log"
check_start
sudo systemctl stop "$unit"

echo "candidate_upgrade_start" | tee -a "$staging/systemd-evidence.log"
cp "$candidate" "$staging/xboard-node.next"
chmod 755 "$staging/xboard-node.next"
mv -f "$staging/xboard-node.next" "$staging/xboard-node"
check_start
sudo systemctl stop "$unit"
(
  cd "$staging"
  sha256sum xboard-node > candidate.sha256
)

echo "injected_start_failure" | tee -a "$staging/systemd-evidence.log"
cat > "$staging/xboard-node.next" <<'EOF'
#!/bin/sh
exit 42
EOF
chmod 755 "$staging/xboard-node.next"
mv -f "$staging/xboard-node.next" "$staging/xboard-node"
if sudo systemctl start "$unit"; then
  echo "FAIL: injected broken binary unexpectedly started" >&2
  exit 1
fi
[[ "$(sudo systemctl show "$unit" -p ExecMainStatus --value)" == 42 ]] || {
  echo "FAIL: incorrect simulated failure exit code" >&2; exit 1;
}
sudo systemctl show "$unit" -p ExecMainStatus -p Result -p ActiveState \
  | tee -a "$staging/systemd-evidence.log"
sudo systemctl reset-failed "$unit"

echo "baseline_rollback_restart" | tee -a "$staging/systemd-evidence.log"
cp "$staging/old-xboard-node" "$staging/xboard-node.next"
chmod 755 "$staging/xboard-node.next"
mv -f "$staging/xboard-node.next" "$staging/xboard-node"
check_start
cmp -s "$staging/old-xboard-node" "$staging/xboard-node"
printf "baseline_sha256=%s\n" "$(sha256sum "$staging/xboard-node" | cut -d' ' -f1)" | tee -a "$staging/systemd-evidence.log"
echo "PASS: isolated systemd version-run upgrade / injected failure / rollback"
echo "SCOPE: NOT a real network daemon, database migration, Redis, panel, or production deployment"
