# ACME rate-limit recovery (Node-Stable)

## Incident: machine 24, 2026-11-22

Six TLS listeners remained disabled while the panel continued receiving periodic reports.
The ACME issuer returned HTTP 429: five certificates for the exact same identifier set in the prior 168 hours, with a retry-after of **2026-11-22 14:24:51 UTC** for the affected hostname. The shared cache on the affected instance had a valid certificate only for **another** hostname. A valid certificate for another DNS name cannot substitute for the missing one.

## Operational response

1. Stop re-installing, deleting `/etc/xboard-node/instances`, or repeatedly forcing renewal. The issuer's limit is external to the node.
2. Preserve the entire `instances` directory and its permissions when upgrading or migrating machines. The shared ACME directory includes account data, issued pairs, and a persistent per-domain cooldown marker.
3. Before performing any restore, locate an existing certificate **and its matching private key** for the affected hostname, e.g. from the previous server's ACME cache or a verified backup. Inspect only certificate SAN, issuer, and expiration using `openssl x509 -in cert.crt -noout -subject -dates -ext subjectAltName`. Never print, upload, or commit the private key.
4. Only restore a complete, validated certificate/key pair into the intended node configuration using its supported `file` or `content` mode, with correct path permissions. Do not copy a different-hostname pair; do not disable TLS validation; do not overwrite current shared ACME files without a validated migration plan.
5. If no valid matching pair exists, leave the affected listeners disabled until the CA's `Retry-After` and verify DNS-01 permissions, account limits and the selected hostname before allowing another request. The quoted UTC deadline applies only to that exact identifier set and is not a guarantee of issuance.
6. Confirm recovery with per-node logs and `ss -lntup` (check UDP separately). A successful `report pushed` does not mean a TLS listener is active.
7. Review whether reinstallations or fresh machine-scoped storage directories lost issued certificates; preserve the shared cache across upgrades.

## Implementation controls

- The service fails closed when the node requires certificate TLS and no valid material exists.
- A machine-wide storage lock serializes issuance, and per-domain cooldown markers survive restarts.
- Diagnostics on the stable branch now include node ID, protocol, certificate mode, and certificate domain for the first listener-disable transition. Secret DNS provider credentials must never be included in logs.
- Build/test success is not equivalent to end-to-end runtime acceptance; test issuance and multi-node reuse on staging before rolling out.

Do not change the production XBoard database to handle an ACME 429.
