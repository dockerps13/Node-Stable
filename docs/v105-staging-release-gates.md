# v1.0.5 production-style staging release gates

**Status: release blocked until the two staging-only gates below are evidenced.**
Do not merge PR #7 or tag/deploy v1.0.5 based only on GitHub CI success.

## Test isolation

Use dedicated staging resources: staging-only XBoard/Laravel panel, staging-only Redis
instance/database, two staging-only node hosts and credentials, and staging-only
protocol users. Do not run traffic generators against production endpoints.
Use production-equivalent Linux, CPU, memory, kernel, TLS and bandwidth settings.
Record sanitized environment specifications (never API keys, machine tokens,
private keys, or full configuration files).

## Gate A: 24-hour real-panel/two-node lease soak

The existing GitHub Actions `v1.0.5 isolated Redis HTTP soak` is a short Go
HTTP test-double + Redis Lua capacity probe. It is **not** this gate.

Run a separately supervised 24-hour acceptance against actual staging Laravel,
Redis, and two xboard-node processes (with separate machine identity and panel
authentication). Include a realistic mix of real connections for the enabled
protocols and authorized / denied machine leases, and exercise:

1. Normal acquisition and TTL renewal with concurrent node traffic.
2. Global device limit reached on one node while the other holds an active lease.
3. Explicit revocation and immediate transfer interruption, including UDP/XUDP.
4. Disconnect, offline empty-list reporting, failed report recovery, and lease
   expiry/reacquisition.
5. Redis disconnect/reconnect and Laravel HTTP 5xx/timeouts without silent
   limit bypass or leaked leases.
6. Staging service restarts under load and stable memory/CPU/FD usage.

Capture every minute: Redis `INFO` memory/stats/clients,
HTTP request counts and p50/p95/p99 latency, node RSS/FD/goroutines,
lease cardinality/expiry, rejected/accepted lease counts, restart count,
and actual protocol response success/error rates. Store sanitized raw logs
and sampled metrics in artifacts with a SHA256 manifest.

**PASS requires**: 24 hours continuous evidence, no unauthorized extra device,
no revoked session continuing payload forwarding, no unexplained process crash,
no lease leak / permanent stale active record, no Redis eviction of active lease
keys, and performance within explicitly approved pre-test SLOs.
Any missing evidence or SLO definition is **NOT RUN / INCOMPLETE**, never PASS.

## Gate B: real staging systemd upgrade, health, fault and rollback

GitHub Actions' offline binary rollback and disposable systemd `-v` unit tests
verify binary execution, staging path swaps, supervision and checksum recovery.
They **do not** authenticate with the actual staging panel or transport traffic.

On an isolated staging host using its actual xboard-node systemd service unit:

1. Record SHA256/version and `systemctl cat/status` of the baseline binary,
   exact config and credentials file permissions, and a known-good health and
   real protocol traffic test. Keep secrets out of the evidence archive.
2. Take an independently verifiable backup of baseline binary, service unit,
   config and credentials in an access-controlled path. Confirm restore paths.
3. Install the candidate artifact using an atomic swap. Restart the staging
   service and verify `systemctl is-active`, live `/healthz` (if configured),
   panel authentication, both nodes' lease admission, and real TCP/UDP traffic.
4. Inject a staging-only candidate failure. Confirm recovery is triggered and
   does not silently degrade into accepting unauthorized devices.
5. Restore the preserved baseline binary and any required original unit/config,
   restart, confirm baseline SHA256 matches and all previous live checks PASS.
6. Repeat candidate deployment from clean staging state and validate intended
   steady state. Retain timestamps, incident/restore logs and release hashes.

**PASS requires**: complete live-health/traffic/authorization checks before
upgrade, after upgrade, and after rollback; no missing or incorrect credentials;
bit-for-bit recoverable baseline; documented operator and restore procedure.

## Release decision

Record a table with `gate / environment / tested commit / start-end UTC /
measurement artifact link / approver / PASS|FAIL|NOT RUN`.
The candidate SHA must match the code actually built into the staging executable.
CI results for commits **before** a changed dependency or workflow do not count
for the new head. Preserve PR as Draft until all mandatory gates PASS.
