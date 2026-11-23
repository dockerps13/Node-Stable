#!/usr/bin/env bash
# Read-only validation of a DEDICATED STAGING machine-mode node.
# Prints only summarized status, node IDs, ports and failure counts.
set -Eeuo pipefail

usage() {
  cat <<'EOF'
Usage: staging-machine-smoke.sh --machine-id ID --expected-nodes N
       [--expected-ids 14,18,25] [--ports 8123/tcp,8443/udp,9443/udp]
       [--expected-version v1.0.4] [--since "20 minutes ago"]

Run on a dedicated staging host AFTER its node service has been restarted.
The script does not modify services, configuration, firewall, Redis, or DB.
EOF
}
die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
machine_id=""; expected_nodes=""; expected_ids=""; ports=""; expected_version=""; since="20 minutes ago"
while (($#)); do
  case "$1" in
    --machine-id|--expected-nodes|--expected-ids|--ports|--expected-version|--since)
      (($# >= 2)) || die "missing value for $1"
      case "$1" in
        --machine-id) machine_id="$2";;
        --expected-nodes) expected_nodes="$2";;
        --expected-ids) expected_ids="$2";;
        --ports) ports="$2";;
        --expected-version) expected_version="$2";;
        --since) since="$2";;
      esac
      shift 2;;
    --help|-h) usage; exit 0;;
    *) die "unsupported argument: $1";;
  esac
done
[[ "$machine_id" =~ ^[1-9][0-9]*$ ]] || die "--machine-id requires a positive integer"
[[ "$expected_nodes" =~ ^[1-9][0-9]*$ ]] || die "--expected-nodes requires a positive integer"
if [[ -n "$expected_ids" ]]; then
  [[ "$expected_ids" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || die "invalid --expected-ids"
  IFS=, read -ra ids <<< "$expected_ids"
  (("${#ids[@]}" == expected_nodes)) || die "--expected-ids count must match --expected-nodes"
fi
if [[ -n "$ports" ]]; then
  [[ "$ports" =~ ^[0-9]+/(tcp|udp)(,[0-9]+/(tcp|udp))*$ ]] || die "invalid --ports format"
fi
if [[ -n "$expected_version" ]]; then
  [[ "$expected_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid --expected-version"
fi

command -v systemctl >/dev/null || die "systemctl missing"
command -v journalctl >/dev/null || die "journalctl missing"
command -v ss >/dev/null || die "ss missing"
command -v xboard-node >/dev/null || die "xboard-node missing"

[[ "$(systemctl is-active xboard-node)" == active ]] || die "xboard-node service is not active"
version="$(xboard-node -v 2>&1)" || die "unable to read node version"
printf 'PASS: xboard-node active\n'
if [[ -n "$expected_version" ]]; then
  [[ "$version" == *"$expected_version"* ]] || die "binary version does not match $expected_version"
  printf 'PASS: binary version matches %s\n' "$expected_version"
fi

# Never print raw journal lines: they can contain user IPs and other details.
logs="$(journalctl -u xboard-node --since "$since" -o cat --no-pager)" || die "cannot read service journal"
discovered="$(printf '%s\n' "$logs" | sed -nE "s/.*machine ${machine_id}: discovered ([0-9]+) nodes.*/\1/p" | tail -n 1)"
[[ -n "$discovered" ]] || die "no initial discovery for machine $machine_id in window ($since)"
[[ "$discovered" == "$expected_nodes" ]] || die "discovered $discovered nodes, expected $expected_nodes"
printf 'PASS: machine %s initially discovered %s nodes\n' "$machine_id" "$discovered"

# Require an actual per-node report from each expected ID. For a full audit,
# pass the exact staging panel machine's IDs rather than just a count.
if [[ -n "$expected_ids" ]]; then
  for id in "${ids[@]}"; do
    printf '%s\n' "$logs" | grep -Eq "machine: starting node ${id} \(" ||
      die "missing startup attempt for node $id"
    printf '%s\n' "$logs" | grep -Eq "report pushed node_id=${id} machine_id=${machine_id}([^0-9]|$)" ||
      die "missing successful report for node $id"
    printf 'PASS: node %s started and sent a successful report\n' "$id"
  done
else
  count="$(printf '%s\n' "$logs" | sed -nE "s/.*report pushed node_id=([0-9]+) machine_id=${machine_id}([^0-9].*)?$/\1/p" | sort -nu | wc -l | tr -d ' ')"
  [[ "$count" == "$expected_nodes" ]] || die "$count unique nodes reported, expected $expected_nodes"
  printf 'PASS: %s distinct node IDs reported\n' "$count"
fi

if [[ -n "$ports" ]]; then
  sockets="$(ss -H -lntup)" || die "cannot inspect listening sockets"
  IFS=, read -ra requested_ports <<< "$ports"
  for item in "${requested_ports[@]}"; do
    port="${item%/*}"; proto="${item#*/}"
    [[ "$port" -ge 1 && "$port" -le 65535 ]] || die "invalid port $port"
    if [[ "$proto" == tcp ]]; then
      printf '%s\n' "$sockets" | awk -v p=":$port" '$1=="tcp" && $2=="LISTEN" && index($5,p)>0 {split($5,a,":"); if(a[length(a)] == substr(p,2)) found=1} END{exit !found}' ||
        die "TCP port $port not listening"
    else
      printf '%s\n' "$sockets" | awk -v p=":$port" '$1=="udp" && index($5,p)>0 {split($5,a,":"); if(a[length(a)] == substr(p,2)) found=1} END{exit !found}' ||
        die "UDP port $port not listening"
    fi
    printf 'PASS: %s/%s listener present\n' "$port" "$proto"
  done
fi
printf 'PASS: read-only staging smoke checks completed. Verify REAL client auth, traffic accounting, and rollback separately.\n'
