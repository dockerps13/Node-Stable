# Node-Stable

**Independent, owner-maintained XBoard node service** providing Linux releases with sing-box / Xray integration, protocol compatibility tests, and controlled upgrades.

[Latest stable release](https://github.com/dockerps13/Node-Stable/releases/latest) · [Changelog](CHANGELOG.md) · [Deployment guide](docs/deployment.md) · [Maintenance guide](docs/maintenance.md)

## Stable release

**v1.0.3** — Linux `amd64` and `arm64` release assets, published 2026-11-22. Downloads are built from an immutable release commit and include `SOURCE_COMMIT` and `SHA256SUMS`. Check the release page for subsequent versions before installing.

### What's new in v1.0.3

- In **panel-managed machine mode**, Hysteria2 and TUIC automatically expose an optional TCP connection probe on the same numeric port as their UDP/QUIC inbound. Existing configurations **without** a `tcp_probe` setting need no manual change after upgrading.
- Explicit `kernel.tcp_probe: false` disables that behavior. Non-machine modes retain opt-in behavior.
- Regression coverage for QUIC clients, TCP probe lifecycle, machine-mode defaults, and TLS certificate recovery diagnostics.
- The probe is **TCP connect RTT only**, not a QUIC, certificate-authentication, or real HTTP proxy latency test. Allow both TCP (probe) and UDP (protocol) ports in the cloud firewall if needed.

## Capabilities

- Inbounds: VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS5, HTTP/HTTPS, AnyTLS, Mieru, Naive (subject to kernel/transport compatibility).
- Modes: individual panel node, panel-managed machine, multi-instance and standalone.
- REST / WebSocket synchronization; certificate modes including ACME DNS-01; user traffic and online-IP reporting.
- CLI `xbctl`, systemd service, verified release artifacts and installer backup/rollback.

**Scope:** This is a maintained distribution, not a guarantee that every client/protocol pairing has been verified in production. Cross-server strict device admission and the optional Laravel idempotent-billing extensions are **not enabled or validated by this release**. See [integration notes](#technical-documentation).

## Install or upgrade

For Linux systemd hosts, download the **pinned v1.0.3 release installer**:

```bash
curl -fL https://github.com/dockerps13/Node-Stable/releases/download/v1.0.3/install.sh -o install-v1.0.3.sh
bash -n install-v1.0.3.sh
```

Review the installer and the release's `SHA256SUMS` before running it. For an **existing installation**, keep a verified backup and use the installer upgrade action with `--version v1.0.3` during a maintenance window. For a **new machine installation**, use `install --mode machine --panel ... --machine-id ... --token ... --version v1.0.3` with your own secret token (never commit or paste credentials). Full step-by-step instructions and verification commands: [deployment guide](docs/deployment.md).

**Upgrade is not automatic.** The production service, panel, database and existing credentials are not changed merely by updating this repository.

## Operating checks

```bash
xboard-node -v
systemctl is-active xboard-node
ss -lntup | grep xboard-node
journalctl -u xboard-node -n 60 --no-pager
```

For Hysteria2/TUIC, the UDP listener carries real proxy traffic, while an enabled same-port TCP listener supports TCPing. Check the cloud security group separately.

## Technical documentation

- [Deployment, backups and rollback](docs/deployment.md)
- [Maintaining this independent project](docs/maintenance.md)
- [Hysteria2/TUIC latency and TCP probe](docs/quic-latency-v103.md)
- [ACME certificate recovery and rate limits](docs/acme-rate-limit-recovery.md)
- [DNS provider configuration](docs-dns-providers.md)
- [Custom routes](docs-custom-routes.md) · [Custom outbounds](docs-custom-outbounds.md) · [Auto-kernel notes](docs-autokernel.md)
- Optional panel integration candidates (not automatically deployed): [device snapshots](integrations/xboard-device-snapshots/README.md), [idempotent billing](integrations/xboard-idempotent/README.md), [protocol compatibility patch](integrations/xboard-panel-compat/README.md)

## Development

```bash
go test -tags "with_quic with_utls with_wireguard with_acme with_clash_api" ./...
make build-linux build-linux-arm64 VERSION=v1.0.3
bash tests/installer-safety.sh
```

GitHub Actions also validates certificate recovery, QUIC compatibility and multi-platform compilation. A successful build alone does not prove production compatibility. See the [maintenance guide](docs/maintenance.md) for the release checklist.

## License and provenance

MPL-2.0; preserve upstream notices and the vendored `third_party/sing-anytls/LICENSE`. The distribution originated from an Xboard-Node source snapshot, then evolved independently; **Node-Stable is not an upstream auto-sync mirror**. The original snapshot commit is recorded in `UPSTREAM_SOURCE_COMMIT` for traceability.
