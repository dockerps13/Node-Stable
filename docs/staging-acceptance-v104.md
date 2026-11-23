# v1.0.4 staging acceptance (machine mode)

**Scope:** Before a v1.0.4 release, exercise code built from the reviewed PR on an **isolated non-production machine and test XBoard panel**. This is a procedure, not evidence that production tests have already passed.

## Isolation requirements

- Use a separate staging VPS, staging panel endpoint, staging machine ID, staging users and staging database/Redis (or read-only fixtures). **Do not reuse** the production `https://soulook.org` machine ID 24, its token, node IDs, credentials, or its running ports.
- Never point a development node at the live panel: machine discovery could attach real users or ports and send reports that affect real accounting.
- Keep v1.0.3 production running unchanged. Before changing any staging installation, take a root-only, verified backup of `/etc/xboard-node`, systemd unit, binaries, `xbctl`, shared ACME files and report journals. Protect staging tokens in root-only files, not shell history, screenshots, git or CI artifacts.
- Avoid testing public certificate issuance repeatedly: use a valid staging domain/cert and respect ACME rate limits. Do not disable TLS verification merely to force green tests.

## 1. Build and review a staging candidate

The tagged v1.0.4 Release does **not** exist yet. Do not use `releases/download/v1.0.4` until publication. From the reviewed `develop/v1.0.4-hardening` commit, compile the Linux binary and CLI (with the Go toolchain from `go.mod`):

```bash
go test -race -count=1 ./internal/machine ./internal/controlplane ./internal/panel ./internal/reportjournal ./internal/devicegate
go test -tags "with_quic with_utls with_wireguard with_acme with_clash_api" ./...
bash tests/installer-safety.sh
bash tests/limits-acceptance.sh
bash tests/release-version-policy.sh
make build-linux VERSION=v1.0.4
./xboard-node-linux-amd64 -v
```

`VERSION` is still `v1.0.3` on the development branch to avoid inadvertently announcing a new release. The explicit build override labels an **unreleased staging candidate**; record its exact commit SHA and checksum. Do not publish it as an official release or run the release workflow.

For the first acceptance run, prefer an isolated staging service and controlled local binary deployment over an unreviewed `curl | bash` upgrade. The installer supports a **local binary** through `--binary`; consult its help and the [deployment guide](deployment.md), and do not run it against production.

## 2. Reproduce the machine workload

In staging, create **11 throwaway nodes** (matching the production protocol mix, not the production node identities), with test accounts only:

| Protocol | Transport expectation |
| --- | --- |
| VLESS | TCP/TLS/REALITY according to test setup |
| VMess | Configured TCP or WS |
| Trojan | TLS/TCP |
| Shadowsocks | TCP/UDP and any explicit plugin |
| Hysteria2 | UDP/QUIC plus optional same-port TCP ping |
| TUIC | UDP/QUIC plus optional same-port TCP ping |
| SOCKS5 | Current kernel selection, test authentication |
| HTTP/HTTPS | TLS certificate, correct hostname and auth |
| AnyTLS | Valid TLS cert and user handshake |
| Mieru | Native transport and authentication |
| Naive | TLS certificate, Naive client and TCP port |

Test actual authorized proxy traffic, not merely an open port or reported status. Hysteria2/TUIC **TCPing is not proof of UDP/QUIC connectivity**. Never treat `online=0` as proof that a node failed to start.

## 3. Automated read-only checks

After a controlled start/restart, on the **staging** VPS run:

```bash
sudo bash tests/staging-machine-smoke.sh \
  --machine-id STAGING_MACHINE_ID --expected-nodes 11 \
  --expected-ids STAGING_NODE_IDS_COMMA_SEPARATED \
  --ports 8123/tcp,8443/udp,8443/tcp,9443/udp,9443/tcp \
  --expected-version v1.0.4 --since "20 minutes ago"
```

Replace **all** placeholders, node IDs and ports with the actual isolated staging values. The script reads `systemctl`, `journalctl`, `ss` and the installed binary version; it never edits the host or sends traffic. It must see initial discovery and one successful report **from each expected node** in the selected journal window. Port checks are optional; do not use the example port list unless the staging configuration uses those ports.

Run the checks again after a service restart. Record summarized PASS/FAIL status, exact candidate SHA and version. Do **not** paste unredacted journals or tokens into issues.

## 4. Failure-path acceptance

- Start and stop a node while its initial panel config request is deliberately delayed. Removal must cancel the request promptly, not hang or leave a stale handle.
- Trigger repeated node discovery and remove/re-add the same staging node ID. The retired generation must not erase the replacement mailbox or status channel; only the active generation reports.
- With the staging panel temporarily returning 503 or being unreachable, verify running nodes are not silently removed by a failed discovery call; recovery re-establishes reports.
- Restart the staging node service. Classify deliberate shutdown from panel removal, and require all 11 staging nodes to start and report before marking recovered.
- Verify Naive TLS hostname, full authentication and a real proxied request; check valid test certificates. Do not use a successful raw TCP connection alone as acceptance.
- With a staging-user traffic counter, measure upload/download before and after short outages and replays; **do not sign off idempotent billing** until a staging Laravel DB reconciliation demonstrates no duplicate or missing debits.
- Test IPv4 and IPv6 reported-IP normalization and device admission. **Do not sign off cross-machine enforcement** based on this single-machine run.

## 5. Rollback and release gate

On staging only, exercise a documented backup restore of the matching binaries, service unit, config, credentials, ACME shared storage and journals. Verify the restored version, all node listeners, successful reports, and client traffic. Do not use `uninstall --purge` as rollback.

Mark release acceptance complete only when:
1. The **latest** development commit passes CI and race tests;
2. All 11 staging protocols start, authenticate, proxy and report with test users;
3. Start/stop/re-add, failed API, restart and rollback tests pass;
4. Credentials and cert caches survive, traffic accounting matches the staging DB, and the signed-off commit SHA is recorded;
5. Any remaining limitations (notably global device leases and optional Laravel billing) are explicitly kept disabled and documented.

Production promotion requires a separate decision and controlled maintenance window; merging a PR or pushing a tag must not silently touch production.
