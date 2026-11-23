#!/usr/bin/env bash
# Offline self-test for staging-machine-smoke.sh. Never contacts a real panel
# or systemd service; all host commands are mocked in an ephemeral PATH.
set -Eeuo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'EOF'
#!/bin/sh
[ "$1" = "is-active" ] && [ "$2" = "xboard-node" ] && { echo active; exit 0; }
exit 2
EOF
cat > "$tmp/bin/xboard-node" <<'EOF'
#!/bin/sh
[ "$1" = "-v" ] && { echo 'xboard-node v1.0.4 (staging fixture)'; exit 0; }
exit 2
EOF
cat > "$tmp/bin/journalctl" <<'EOF'
#!/bin/sh
cat "$STAGING_FIXTURE_LOG"
EOF
cat > "$tmp/bin/ss" <<'EOF'
#!/bin/sh
cat "$STAGING_FIXTURE_SOCKETS"
EOF
chmod +x "$tmp/bin/"*
cat > "$tmp/journal" <<'EOF'
06:21:02.817 INFO [core] machine 99: discovered 2 nodes
06:21:07.942 INFO [core] machine: starting node 11 (naive/Naive)
06:21:07.943 INFO [core] machine: starting node 12 (hysteria/Hysteria2)
06:22:26.966 INFO [naive:8123] report pushed node_id=11 machine_id=99 traffic_users=0 online=0
06:22:26.967 INFO [hysteria2:8443] report pushed node_id=12 machine_id=99 traffic_users=0 online=0
EOF
cat > "$tmp/sockets" <<'EOF'
tcp LISTEN 0 128 0.0.0.0:8123 0.0.0.0:* users:(("xboard-node",pid=1,fd=1))
udp UNCONN 0 0 0.0.0.0:8443 0.0.0.0:* users:(("xboard-node",pid=1,fd=2))
tcp LISTEN 0 128 0.0.0.0:8443 0.0.0.0:* users:(("xboard-node",pid=1,fd=3))
EOF
export PATH="$tmp/bin:$PATH"
export STAGING_FIXTURE_LOG="$tmp/journal" STAGING_FIXTURE_SOCKETS="$tmp/sockets"
args=(--machine-id 99 --expected-nodes 2 --expected-ids 11,12 --ports 8123/tcp,8443/udp,8443/tcp --expected-version v1.0.4 --since "20 minutes ago")
bash "$here/staging-machine-smoke.sh" "${args[@]}" > "$tmp/output"
grep -Fq 'PASS: read-only staging smoke checks completed' "$tmp/output" || {
  echo "valid staging fixture rejected" >&2; exit 1;
}
# A missing report must fail rather than being mistaken for online=0.
grep -v 'report pushed node_id=12' "$tmp/journal" > "$tmp/journal-truncated"
export STAGING_FIXTURE_LOG="$tmp/journal-truncated"
if bash "$here/staging-machine-smoke.sh" "${args[@]}" > /dev/null 2>&1; then
  echo 'missing node report accepted' >&2; exit 1
fi
export STAGING_FIXTURE_LOG="$tmp/journal"
sed '/0.0.0.0:8123/d' "$tmp/sockets" > "$tmp/sockets-missing"
export STAGING_FIXTURE_SOCKETS="$tmp/sockets-missing"
if bash "$here/staging-machine-smoke.sh" "${args[@]}" > /dev/null 2>&1; then
  echo 'missing TCP listener accepted' >&2; exit 1
fi
if bash "$here/staging-machine-smoke.sh" --machine-id wrong --expected-nodes 2 >/dev/null 2>&1; then
  echo 'invalid machine id accepted' >&2; exit 1
fi
echo 'Staging smoke parser tests passed'
