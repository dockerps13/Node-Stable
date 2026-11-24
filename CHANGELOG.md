# Changelog

Release artifacts, source SHAs and checksums: [Node-Stable Releases](https://github.com/dockerps13/Node-Stable/releases). This file describes the maintained **Node-Stable** distribution; historical candidate documents are preserved in Git history, not published as current guidance.

## v1.0.4 — 2026-11-25 (release candidate)

- Refresh online device snapshots promptly after connection changes using a bounded, coalesced event notification; preserve billing traffic counters during the refresh.
- Send an explicit empty `online: {}` map to clear stale per-node online counts; a missing (`nil`) snapshot still omits the field.
- Preserve shared machine device admission callbacks and improve TLS certificate outage diagnostics without repetitive identical warnings.
- Retain durable report IDs, verified idempotent billing receipts, crash/restart recovery, and safe handling of ambiguous legacy reports.
- Release validation: Go tests with production QUIC/uTLS/WireGuard/ACME/Clash tags; PHP panel idempotent billing integration validated separately in an isolated test database. The optional Laravel patch is **not included** in the node release and is **not deployed by this workflow**.
- Known limitation: Go race detection has not yet run in the production test environment because GCC was unavailable. Do not describe race coverage as passed.

## v1.0.3 — 2026-11-22 (stable)

- **Machine mode:** automatically expose same-port TCP handshake latency probes for running Hysteria2 and TUIC UDP/QUIC nodes when `kernel.tcp_probe` is omitted. An explicit `false` disables the probe; single-node and standalone modes remain opt-in.
- Add QUIC client/server protocol roundtrip tests, same-port TCP/UDP coexistence tests, machine-mode config inheritance/opt-out tests and GitHub Actions regression checks.
- Improve TLS missing-certificate logging with node ID, protocol, certificate mode/domain and failure state; avoid repeated identical warnings and guard missing config references.
- Preserve the ACME per-domain HTTP 429 cooldown and certificate validity checks. A missing certificate cannot be bypassed by the TCP probe.
- **Reminder:** TCPing is TCP connect RTT, not authenticated Hysteria2/TUIC QUIC delay. Real UDP and TCP probe ports require separate network/cloud firewall permissions.

Linux amd64/arm64 binaries, `xbctl`, installer, `SOURCE_COMMIT` and `SHA256SUMS` are published together. Panel integration experiments are not included as a validated Laravel production upgrade.

## v1.0.2 — 2026-11-22 (stable)

- Introduced **opt-in** `kernel.tcp_probe: true` for Hysteria2 and TUIC TCP-only connection checks; UDP/QUIC operation was unchanged.
- Added lifecycle and port-conflict regression coverage for the TCP listener.
- Added fast WebSocket connection/device snapshot updates with fallback reporting.
- Improved Xray SOCKS5+TLS/HTTP traffic counter association, device admission within a machine, report journaling and protocol compatibility tests.
- Continued certificate manager reliability work, installer verification and backup/rollback coverage.
- Included opt-in, **not production-deployed**, XBoard Laravel/Redis examples for global device snapshots and idempotent billing. Their end-to-end deployment and cross-machine semantics still require separate acceptance.

## Historical provenance

Node-Stable was initialized from an immutable Xboard-Node source snapshot identified in `UPSTREAM_SOURCE_COMMIT`. Subsequent releases are produced by this repository's own CI and release process. Refer to Git history for v1.0.1/v1.0.0 development notes and older acceptance plans; they are not current release gates.
