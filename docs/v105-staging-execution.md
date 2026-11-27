# v1.0.5 staging execution — operator handoff

Status: **NOT RUN against a dedicated live staging installation**.

This is an operational procedure, not evidence of successful execution. The GitHub-hosted
CI tests use a disposable test Redis, simulated HTTP endpoints, and a temporary
systemd version-only service. They cannot substitute for the two release gates here.

## Prerequisites and isolation

- A **dedicated, explicitly approved staging environment** with staging Laravel
  XBoard, an isolated Redis instance, two independently authenticated staging
  node services, test-only users and test-only protocol destinations.
- The dedicated host must have no production credentials or workloads.
- A low-privilege operator account with scoped sudo access to **staging units only**.
- A locked-down marker at `/etc/xboard-node/STAGING-ONLY` with the exact contents
  `I_CONFIRM_DEDICATED_STAGING_NOT_PRODUCTION`.
- All probe scripts must exercise actual staging Laravel authorization, global
  device limits on both nodes, revoked TCP/UDP connections, and real traffic.
  Each script must fail nonzero on a violation. Do **not** substitute `/bin/true`.
- Never commit panel credentials or Redis secrets to GitHub and do not upload
  unsanitized logs.

## Gate A: 24-hour live staging collection

The file `tests/staging-24h-collector.sh` collects one row per interval, including
actual node unit state, HTTP probe latency, Redis memory/client/eviction counters,
and the results of mandatory external admission and traffic probes. It **does not
generate load**, so an independent staging traffic generator must be configured.

Its environment variables and usage are printed with:
```bash
bash tests/staging-24h-collector.sh --help
```

Run from the dedicated staging host in a protected supervised session with the
required `STAGING_*` variables defined, including a **new** evidence path,
`STAGING_DURATION_SECONDS=86400`, and
`STAGING_NODE_UNITS="xboard-node-stage-a xboard-node-stage-b"`.
All probes must be executable, real and idempotent. A collector with dummy
probes or without meaningful staging load is **NOT a passing test**.

Record additional production-equivalent metrics (node RSS/FD/goroutines,
panel p50/p95/p99, active lease counts and Redis TTLs) using existing trusted
monitoring tools. Maintain the timeline and preserve sanitized export with
SHA256 hashes. The release reviewer must inspect the underlying probe logic and
approve load shape/SLOs *before* calling this gate PASS.

## Gate B: live systemd staged upgrade / injected fault / rollback

The file `tests/staging-live-rollback.sh` operates on one staging service only.
It requires the executable to be installed under the dedicated staging path
`/opt/xboard-node-staging/bin/xboard-node` and the systemd unit's ExecStart to
point there. It refuses a non-staging unit or missing staging marker.

**It intentionally interrupts the staging service** by installing a script
that exits 42, then restores the baseline binary and checks real health,
authorization and protocol traffic. Only run after arranging the staging
maintenance window. It must never be executed on a production host.

Required operator-supplied environment variables:
`STAGING_SENTINEL`, `STAGING_NODE_UNIT`,
`STAGING_CANDIDATE_BIN`, `STAGING_HEALTH_PROBE`,
`STAGING_ADMISSION_PROBE`, `STAGING_TRAFFIC_PROBE`,
`STAGING_EVIDENCE_DIR`.

```bash
bash tests/staging-live-rollback.sh --help
```

A passing GitHub static shell check means the script parses and has staging
guards. It does **not** prove the live rollback succeeded. Only mark PASS after
the real staging execution and manual review of unit logs, panel state,
network tests, and SHA256 recovery.

## Signoff and release decision

Record: staging host anonymized ID; Git commit SHA and build SHA256;
baseline SHA256; start/end UTC; versions of panel, Redis, Linux and kernel;
load level; raw sanitized evidence URI; p50/p95/p99; Redis evictions; all
failed authorization attempts; recovery duration; reviewer.

The remaining gates are **NOT RUN** until those artifacts exist and cover real
staging. Keep PR #7 Draft, do not merge, tag or deploy v1.0.5 on the strength
of CI-only checks.
