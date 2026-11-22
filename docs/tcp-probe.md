# TCP connect latency probe for Hysteria2 / TUIC

Hysteria2 and TUIC run on UDP/QUIC. Some clients use a **TCP port connect** test and display blank TCPing even when the actual proxy works.

**Since Node-Stable v1.0.3:** in **panel-managed machine mode**, a successfully running Hysteria2/TUIC node automatically exposes a same-numeric-port TCP handshake endpoint when `kernel.tcp_probe` is omitted. To disable it, explicitly set:

```yaml
kernel:
  tcp_probe: false
```

For non-machine modes, opt in with `tcp_probe: true`. Do not create a duplicate `kernel:` mapping in YAML. An existing TCP bind conflict is logged and does not stop the UDP inbound. The TCP socket closes accepted connections immediately; it is not a proxy and must not be advertised as QUIC RTT.

External TCP Ping requires the server/network/cloud firewall to allow **TCP** to that port; real Hysteria2/TUIC traffic additionally requires **UDP**. Always validate a real authenticated proxy/HTTP URL request separately.

[More about latency and QUIC troubleshooting](quic-latency-v103.md).
