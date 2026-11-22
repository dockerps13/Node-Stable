# Node-Stable deployment and operations

This is the operational guide for the **Node-Stable** distribution. The latest release is listed on [GitHub Releases](https://github.com/dockerps13/Node-Stable/releases); use a pinned version, not a moving `main` branch.

## Requirements

Linux with systemd, supported CPU architecture (published Linux amd64/arm64 binaries), outbound access to the configured XBoard panel, and correct inbound TCP/UDP cloud firewall rules. Keep an independent SSH connection while upgrading a proxy node.

## Release integrity and safe staging

For v1.0.3, retrieve the following files from the **same GitHub Release**: `install.sh`, `SOURCE_COMMIT`, `SHA256SUMS`, `xboard-node-linux-amd64` or `xboard-node-linux-arm64`, and the matching `xbctl` binary. Verify `sha256sum -c SHA256SUMS` in a directory containing all listed assets. The manifest protects against accidental corruption but does not replace independent signature verification.

```bash
curl -fL https://github.com/dockerps13/Node-Stable/releases/download/v1.0.3/install.sh -o /root/node-stable-v103.sh
bash -n /root/node-stable-v103.sh
```

Read the script before running it, and prefer its pinned-version checksum-verifying download path.

## Fresh install

Use the downloaded, reviewed installer:

```bash
# Example only: replace panel URL and machine ID.
# Supply the actual secret token without writing it to a repository.
bash /root/node-stable-v103.sh install --version v1.0.3 \
  --mode machine --panel https://panel.example.com \
  --machine-id 24 --token 'YOUR_MACHINE_TOKEN'
```

For individual-node mode, replace machine options with `--mode node --node-id ID`. Treat command-line tokens as sensitive: local process inspectors may briefly see them. Never share real tokens in screenshots or issue logs.

## Upgrade an existing systemd installation

1. Record the installed version, health, node identities, listening ports, and current service unit.
2. Preserve a **root-only backup** of binaries, `xbctl`, `/etc/xboard-node/config.yml`, credentials, service unit, the full `/etc/xboard-node/instances` tree (including ACME account and certificate cache), and report journals. Do not publish backups or private keys.
3. Run the reviewed pinned installer in a maintenance window:

```bash
bash /root/node-stable-v103.sh upgrade --version v1.0.3
```

4. Review installer backup/checksum and health output, then verify actual client operation. The upgrade process restarts the service and may briefly interrupt active connections.
5. If an upgrade fails, follow the installer rollback output and restore a verified matched backup. **Never use `uninstall --purge` as rollback.**

## Verify after install / upgrade

```bash
xboard-node -v
systemctl status xboard-node --no-pager
ss -lntup | grep xboard-node
journalctl -u xboard-node -n 100 --no-pager
```

For panel-managed machines with Hysteria2/TUIC:
- UDP on the configured port is the actual QUIC inbound.
- In v1.0.3, an **omitted** `kernel.tcp_probe` defaults to **on for machine mode** and provides a same-port TCP connect probe for TCPing; `tcp_probe: false` is a deliberate opt-out.
- The host/cloud firewall must permit **TCP** for TCPing and **UDP** for real protocol operation.
- Test real proxied HTTP traffic separately. A successful TCP probe or panel report does not prove QUIC authentication or traffic handling.

## Certificates

ACME certificates are bound to the requested hostname and matching private key. Never substitute a certificate for a different hostname. Keep machine-wide shared ACME storage intact across upgrades, and obey CA HTTP 429 `Retry-After`. See [ACME recovery](acme-rate-limit-recovery.md).

## Limits and integrations

Online device counts generally reflect observed public IPs, not hardware identities. Same-machine admission and global cross-machine admission are distinct. The optional PHP/Redis integration directories are **candidates**, not part of a production panel rollout. Keep them disabled until tested on a backed-up staging panel.

See [maintenance](maintenance.md) for review, tests, release updates, and ongoing ownership.
