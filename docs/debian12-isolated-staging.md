# Debian 12 isolated VPS acceptance — Node-Stable v1.0.4 candidate

This is a **staging-only** guide. v1.0.4 is not yet a GitHub Release. Do not use `releases/download/v1.0.4`, `--version latest` or the current production XBoard machine as staging.

## 1. Provision a disposable server

Create a **NEW** Debian 12 amd64 VPS (2 vCPU / 2–4 GiB RAM / 30 GiB disk are reasonable minimum staging specs, not a performance guarantee). Select an SSH key, keep a separate SSH session, and use a NEW public IP. Start with port **22/TCP restricted to your own IP**. Do not allow unrestricted inbound 11-protocol ports: open each needed test port and TCP/UDP transport deliberately.

The staging panel must have a **different HTTPS hostname and independent database, cache/Redis, admin login, machine token, machine ID, node assignments and test user accounts** from production. Running a separately configured panel in a container on the staging host is acceptable, but prevent panel/proxy TCP port collisions. This repository does **not** provision a production-compatible Laravel/XBoard panel automatically. Obtain a working isolated panel first; otherwise only run GitHub CI and offline mocks.

**Never** connect the test node to `https://soulook.org`, use production machine ID `24`, or import production database/Redis/credentials. Cloud firewall controls are separate from `ufw`/system firewalls.

## 2. Download the candidate built by GitHub (no Go toolchain needed on VPS)

1. Open [Actions — Build isolated v1.0.4 staging candidate](https://github.com/dockerps13/Node-Stable/actions/workflows/staging-candidate.yml).
2. Select the **latest successful run on `develop/v1.0.4-hardening`**. Under **Artifacts**, download the `linux-amd64` candidate ZIP (or `linux-arm64` for an ARM64 VPS). Confirm the run commit matches the intended reviewed PR commit.
3. Transfer that ZIP from your local computer to the **new test VPS**, e.g. with SSH/SCP. On a Windows PowerShell terminal:
   ```powershell
   scp "$env:USERPROFILE\Downloads\<ACTUAL_ARTIFACT_NAME>.zip" root@<STAGING_VPS_IP>:/root/node-stable-staging.zip
   ```
   Replace placeholders with the **actual download name and test VPS IP**. Do not upload SSH keys or panel tokens.

On the **new Debian 12 VPS** as root:
```bash
apt-get update
apt-get install -y unzip ca-certificates curl
install -d -m 700 /root/node-stable-staging
unzip -oq /root/node-stable-staging.zip -d /root/node-stable-staging
cd /root/node-stable-staging
sha256sum -c SHA256SUMS
cat STAGING_ONLY.txt
cat SOURCE_COMMIT
bash -n install.sh
bash -n staging-preflight.sh
bash -n staging-machine-smoke.sh
./xboard-node-linux-amd64 -v 2>/dev/null || true
```
Use `xboard-node-linux-arm64` on ARM64; it cannot execute on an AMD64 host. The manifest and `SOURCE_COMMIT` make the candidate auditable; they do not replace review of the GitHub workflow and code. The binary must show the **v1.0.4** candidate version. `STAGING_ONLY.txt` identifies it as **unreleased**, not an official signed package.

## 3. Check test-only identity, then install

Run the read-only guard. Replace these values with your **isolated** panel URL and machine ID:
```bash
cd /root/node-stable-staging
bash staging-preflight.sh --panel 'https://staging-panel.example.net' --machine-id 99
```

The preflight refuses production panel hostnames, production machine ID 24, non-Debian-12 hosts and machines with existing node credentials or config. It cannot prove the staging database is separate — verify that manually in your isolated panel.

Only on this **disposable test VPS**, use the packaged **guarded installer** (it checks checksums, panel hostname, OS, machine ID, existing installation, and asks for a new staging token interactively):

```bash
cd /root/node-stable-staging
bash staging-install.sh \
  --panel 'https://staging-panel.example.net' \
  --machine-id 99
```

Replace the example values with your **actual isolated** staging panel and ID. Do not use a production token. For ARM64, the script automatically chooses the `arm64` binary. The installer writes root-only credentials to this **test VPS**. The Token is passed to the original installer as a CLI option and may be briefly visible to local process inspectors; use a private dedicated VPS/SSH session. Keep the downloaded ZIP and its SHA256SUMS for repeatable rollback tests.

If the panel is not yet configured with matching machine ID and node assignments, the service may fail discovery and installation health checks. Configure the **isolated staging panel first**.

## 4. Verify 11 nodes, connections and accounting

Create the eleven staging nodes described in [full acceptance checklist](staging-acceptance-v104.md): VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS5, HTTP/HTTPS, AnyTLS, Mieru, Naive. Use **staging user credentials**, unique test ports and matching TLS certificates. Do not clone production users or certificate private keys.

```bash
systemctl is-active xboard-node
xboard-node -v
ss -lntup | grep xboard-node
# Replace example IDs/ports with values from YOUR staging panel:
bash /root/node-stable-staging/staging-machine-smoke.sh \
  --machine-id 99 --expected-nodes 11 \
  --expected-ids 101,102,103,104,105,106,107,108,109,110,111 \
  --expected-version v1.0.4 --since '20 minutes ago'
```

If verifying individual listeners, add `--ports PORT/tcp,PORT/udp` with actual staged port numbers. The checker only verifies **startup, reporting and socket presence**; you must additionally verify real client authentication, proxy traffic, UDP/QUIC, Naive TLS, user accounting and online state. `online=0` does not by itself imply the server is down.

Check how the machine responds to a **controlled restart**, delayed panel response and node removal/re-add. Keep the full protocol test and accounting results **in staging**, never in production.

## 5. Rehearse recovery and rollback, then keep production unchanged

Before any staging upgrade, back up and verify binaries, `xbctl`, `/etc/xboard-node` (including certificates, shared ACME cache and report journals) and the systemd service unit. Use the installer’s verified backup/rollback path; do not use `uninstall --purge` for rollback. Restore on this test VPS and re-run step 4. Retain the exact candidate `SOURCE_COMMIT` and SHA256 evidence.

**Do not merge or publish a release solely because offline mocks and CI are green.** Complete actual staged 11-node traffic, reporting, accounting and rollback verification first. Global device enforcement and Laravel idempotent billing remain out of scope and must not be marked production-ready without their own panel/database/Redis test plan.

## What cannot be performed from GitHub alone

GitHub Actions can build, checksum, and test the candidate in a CI runner. It cannot log in to your new VPS, provision an actual isolated XBoard panel, configure its domain/TLS/users or validate real network paths unless you explicitly connect an authorized remote execution capability. Never publish SSH private keys or long-lived server secrets as public Actions artifacts.
