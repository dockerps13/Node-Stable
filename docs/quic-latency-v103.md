# Hysteria2 / TUIC TCP latency compatibility (v1.0.3 stable)

**Do not interpret a missing TCP-ping number as proof that a UDP/QUIC proxy is down.** Hysteria2 and TUIC have UDP-only QUIC transport. A TCP connect test is not a protocol-authenticated RTT measurement. It is not equivalent to latency measured through a working proxy tunnel.

## v1.0.3 machine-mode default

Beginning with v1.0.3, **panel-managed machine mode automatically enables the optional TCP connect probe on running Hysteria2 and TUIC nodes**, even when an older machine config does not contain a `tcp_probe` field. Existing YAML with `kernel.tcp_probe: true` remains enabled. To opt out explicitly, set `kernel.tcp_probe: false`. Multi-instance configurations inherit the top-level setting unless an instance explicitly overrides it. Standalone and non-machine modes retain their previous opt-in behavior.

This opens TCP listeners matching the QUIC UDP ports; the server firewall and cloud security group must allow **TCP**, in addition to the **UDP** ports that carry the real proxy. The TCP RTT does not prove UDP or QUIC functionality. An upgrade only takes effect after the new binary is installed and the service is restarted using the normal verified upgrade procedure; never overwrite production configuration merely to enable the new default.

## Diagnosis (read-only, Linux)

1. Check actual UDP listeners: `ss -lunp | grep -E ':(8443|9443)\\b'` (replace ports with your configured UDP ports).
2. Check the service startup log for successful kernel start, certificate status, configuration errors and bind failures. A `report pushed` entry proves only panel reporting.
3. Confirm the server firewall and cloud security group allow **inbound UDP** on each port, not only TCP. Check client network support for UDP and matching TLS SNI, authentication/UUID/password and ALPN.
4. Test a real Hysteria2/TUIC proxy connection via a compatible client using an HTTP URL latency probe. If that works but TCP connect ping reports missing, the missing number is a probe-type mismatch.

## Optional TCP-only connectivity RTT for clients that test TCP ports

The node already supports the following setting in `/etc/xboard-node/config.yml`:

```yaml
kernel:
  tcp_probe: true
```

In machine mode, the top-level kernel option is inherited by each managed node. When enabled, a running Hysteria2 or TUIC UDP listener may additionally expose a **bare TCP accept/close socket on the same numeric port**. This can satisfy clients measuring TCP connect RTT. It does **not** test UDP delivery, QUIC, TLS, credentials, proxy egress or real user latency. TCP probe startup failure never interrupts the UDP listener; logs include `TCP latency probe unavailable` if the TCP port is occupied.

Enabling this opens another public TCP listening socket; it is **enabled by default in machine mode as of v1.0.3**, unless explicitly disabled, and must be reviewed against local firewall/security posture. For non-machine modes, opt in with the existing kernel section after taking a backup; validate YAML and restart during a maintenance window. Never add a duplicate `kernel:` block or overwrite panel tokens.

## Release acceptance gate

- Real sing-box Hysteria2 and TUIC protocol client/server roundtrip tests on loopback, including TLS and authentication.
- Probe enabled and disabled lifecycle tests, with QUIC UDP and optional same-port TCP coexisting.
- Verify UDP port bindings with `ss -lunp` and external UDP reachability on a VPS plus a Windows/v2rayN URL latency test, separately from TCP connect ping.
- Do not label the issue repaired or ship v1.0.3 based solely on build success.
