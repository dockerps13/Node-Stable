# Node-Stable independent maintenance guide

This repository is maintained as an **independent distribution** by its GitHub owner. Its tagged releases are built here, not fetched from the legacy Xboard-Node repository. `UPSTREAM_SOURCE_COMMIT` is a historical provenance marker, **not** an auto-update dependency.

## Boundaries and safety

- GitHub source/CI changes do **not** edit a running AWS node or the XBoard Laravel database.
- Production rollout is a separate, explicit operation with backups and rollback.
- Preserve all license/attribution material (including the vendored AnyTLS license).
- Never commit panel tokens, ACME account keys, certificate private keys, credentials, operational backups or user traffic.
- Optional `integrations/` PHP/Redis panel code is **not deployed** or validated by the node release; keep it opt-in.

## Main source areas

| Path | Responsibility |
| --- | --- |
| `internal/machine/` | Panel-managed multi-node orchestration |
| `internal/service/` | Per-node lifecycle, config sync, accounting and TLS safety |
| `internal/kernel/singbox/`, `internal/kernel/xray/` | Protocol engines and transport configuration |
| `internal/cert/` | ACME/cache, cert validation and renewal |
| `internal/tcpprobe/` | Optional TCP connect RTT endpoint for UDP-only QUIC protocols |
| `internal/config/` | YAML parsing, machine defaults and compatibility |
| `cmd/xbctl/`, `install.sh` | CLI, installation and upgrades |
| `.github/workflows/` | CI, build validation and release packaging |

## Routine development

1. Make changes on a development branch and keep `main` release-ready. Avoid editing already tagged release content.
2. Run and inspect locally or on CI:
   ```bash
   go test -tags "with_quic with_utls with_wireguard with_acme with_clash_api" ./...
   go vet ./...
   bash tests/installer-safety.sh
   bash tests/limits-acceptance.sh
   make build-linux build-linux-arm64 VERSION=vX.Y.Z
   ```
3. Require successful **Certificate safety regression**, **QUIC protocol and latency compatibility**, and **Multi-platform build validation** workflows where relevant.
4. For any change involving TLS/ACME, billing, admission, certificate reuse, or config upgrades, test failure and rollback cases, not only a green happy-path.
5. Use the [v1.0.4 staging acceptance checklist](staging-acceptance-v104.md) on an isolated panel and VPS: verify real clients for all affected protocols, restart/recovery, successful per-node reports, and a backed-up rollback. Verify actual UDP traffic for Hysteria2/TUIC separately from TCPing.
6. Review changelog, READMEs, docs and version metadata in the same change.

## Future releases

- Update the root `VERSION` file and the changelog in a reviewed release preparation PR. CI enforces consistent version metadata with `tests/release-version-policy.sh`.
- The release workflow only runs when a new semantic version tag is pushed, and verifies that tag equals `VERSION`. Editing a workflow file on `main` is not itself permission to publish. Never move or overwrite an existing release tag.
- Make sure the final source commit is clean, tests and builds pass, and no same-tag release exists.
- The release workflow builds Linux amd64/arm64 binaries and `xbctl`, creates `SOURCE_COMMIT`, packages `install.sh`, `CHANGELOG` and `SHA256SUMS`, then publishes a new immutable-version tag. The release *files* are tied to one source SHA; do not overwrite a released tag.
- Independently verify GitHub release asset names, checksums, version output, and published tag SHA after the workflow finishes.
- Roll out with `install.sh upgrade --version vX.Y.Z` only after an approved maintenance window. Do not auto-upgrade production from `main` or `latest`.

## Support and incident procedure

When a node reports TCP Ping `-1`, first distinguish TCP-only probe absence from a real UDP/QUIC failure. Check `ss -lntup`, inbound security-group protocol rules, TLS/SNI and a real URL latency test. For CA 429, preserve shared ACME cache, respect the issuer retry time and do not repeatedly delete/recreate cert directories. Report bugs with redacted logs, exact binary version, protocol, node mode and reproduction steps.

## Documentation retention policy

Keep user-facing documentation current. Do not reinstate old candidate or historical acceptance documents as current policy. Important release history lives in git tags, commit history and GitHub Releases. Keep configuration guides, operational safety documentation, license notices and opt-in integration descriptions.
