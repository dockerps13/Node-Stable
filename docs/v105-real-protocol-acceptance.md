# Node-Stable v1.0.5 — real-protocol release gate

**Status: BLOCKED.** This is an acceptance plan, not evidence of a passed test. Keep the Node-Stable and Xboard PRs in draft and keep v1.0.4 production unchanged.

## Latest CI evidence (not a release approval)

At Node-Stable commit `6930bd981c562e06faed4b0690116241ae312898`, the three CI checks passed:
- [v1.0.5 global device admission](https://github.com/dockerps13/Node-Stable/actions/runs/45695969897)
- [Real Laravel and two-node E2E](https://github.com/dockerps13/Node-Stable/actions/runs/45695969851)
- [v1.0.4 pre-release acceptance](https://github.com/dockerps13/Node-Stable/actions/runs/45695969903)

The next commit adds a twelve-iteration real Hysteria2/TUIC UDP revoke/reconnect race run. **Its result is not yet recorded here.** Green CI does not replace real packaged-service, soak, rollback, Xray MUX/XUDP, or panel-plugin compatibility evidence.

### Open resource-lifecycle risk

`trackedPacketConn.Close` in strict-revocation mode defers the underlying QUIC UDP close while `ReadPacket` is in progress. A permanently blocked underlying read may therefore delay cleanup indefinitely. Before release, verify idle-session revocation drains the underlying reader, releases the socket/session, and removes the global lease with bounded timing; exercise this under Go race detection without suppressing failures. Do not claim this is covered by the existing roundtrip test.

## Scope and test topology

Use an **isolated** XBoard Laravel 12 panel, disposable SQL fixtures, a dedicated Redis 7 instance, and **two separate packaged Node-Stable processes** with unique machine IDs/tokens. The panel and both nodes must be reachable by real proxy clients. Do not reuse production database, Redis, tokens, domains, listener ports, or live node IDs.

Record: both repository commit SHAs; workflow run URLs; exact Xray and sing-box versions/build flags; panel/PHP/Redis versions; target OS; configuration hashes (with secrets redacted); command lines; test logs; packet/connection counts; and rollback results.

## Mandatory live test matrix

| Area | Setup and stimulus | Required observation |
| --- | --- | --- |
| Xray TCP / MUX | Open real authenticated proxy sessions, multiplex streams, exhaust per-user global quota from a second machine | Quota is enforced across both nodes, not per process; no unauthorized extra stream |
| Xray UDP / XUDP | Send and receive real UDP payloads through XUDP; revoke admission while packets are flowing | New unauthorized sessions are rejected; existing session closure/recovery follows documented semantics; no stale lease |
| sing-box TCP | Real client and server sockets, revoke an active global lease | Remote revocation closes the actual client connection, not only an internal tracker |
| sing-box UDP / QUIC | Real bidirectional UDP and QUIC traffic, then expire/revoke quota | Correct admission and cleanup, with no indefinitely renewed orphan ownership |
| Sharing and limits | Two client connections sharing one canonical IP, then a different IP; repeat from two machine tokens | Same-IP slot is shared as designed; extra IP is denied; last owner release makes the slot available |
| IPv4 / IPv6 | IPv4, IPv4-mapped IPv6, canonical IPv6 and duplicate spellings | Deterministic canonicalization and expected global slot counts |
| Crash and restart | Kill one node process during traffic, restart with same machine identity | Bounded recovery from leases after TTL; no duplicate ownership or permanent denial |
| Failure injection | Redis outage, panel HTTP outage, HTTP 401/403, timeout and lost response | No bypass of strict enforcement; explicit fail-closed and recovery behavior with bounded resource use |
| Quota reduction | Reduce a connected user's global quota from 2 to 1 | Deterministic excess-owner eviction and no unauthorized renewals |
| Accounting | Retry and disconnect during byte accounting with real Laravel DB/queues | No duplicated or lost charges; report ID and ledger remain consistent |

For every row, preserve an **actual client-visible** observation and independently inspect Redis lease state. A mock unit test, a TCP loopback-only test or separate Go test processes alone do **not** satisfy a real-protocol row.

## Performance and stability

1. Measure sustained HTTP/Redis throughput and P50/P95/P99 admission/renew/release latency with realistic active-lease cardinality; capture Redis CPU, memory, command latency and Lua runtime.
2. Confirm fair rejection without unbounded retry storms on both nodes under quota contention.
3. Run repeatable multi-hour load and a **24-hour staging soak** with connection churn, service restart, network disruption and quota changes. Define and record pass/fail thresholds before running tests.
4. Rehearse packaged binary rollback and verify the old production-compatible release can be restored with clean lease handling and no panel schema/data loss.
5. Review XBoard third-party `server.users.get` hook compatibility for plugins that require the full collection before deployment.

## Gate to release

- [ ] All live rows above pass with attached, reproducible evidence.
- [ ] Performance and 24-hour soak acceptance passes.
- [ ] Third-party panel hook compatibility decision is documented.
- [ ] Packaged staging upgrade and rollback passes.
- [ ] CI for the **final frozen SHA** passes in both repositories.
- [ ] Release owner explicitly approves merge, tag and deployment.

**Never interpret a green unit-test workflow as proof that the live-protocol release gate has passed.**
