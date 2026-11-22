package singbox

import (
    "net"
    "strconv"
    "testing"
    "time"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/kernel"
    "github.com/cedar2025/xboard-node/internal/model"
)

// Regression: a UDP-only QUIC service can expose an optional same-port TCP
// connection probe without disabling its actual UDP listener. TCP RTT does NOT
// verify Hysteria2/TUIC authentication, TLS, or the QUIC path.
func TestQUICUDPAndOptionalTCPProbeCoexist(t *testing.T) {
    for _, protocol := range []string{"hysteria2", "tuic"} {
        t.Run(protocol, func(t *testing.T) {
            reservation, err := net.ListenPacket("udp4", "127.0.0.1:0")
            if err != nil { t.Fatal(err) }
            port := reservation.LocalAddr().(*net.UDPAddr).Port
            _ = reservation.Close()

            cert := testSelfSignedTLS(t)
            node := &model.NodeSpec{
                Protocol: protocol, TLS: 1, ServerPort: port, ListenIP: "127.0.0.1",
            }
            server := New(config.KernelConfig{
                Type: "singbox", TCPProbe: true,
                ConfigDir: t.TempDir(), LogLevel: "error",
            })
            defer server.Stop()
            credential := "aaaaaaaa-1111-2222-3333-444444444444"
            if err := server.Start(node, []model.UserSpec{{ID: 1, UUID: credential}}, kernel.TLSCert{
                CertPEM: cert.CertPEM, KeyPEM: cert.KeyPEM,
            }); err != nil { t.Fatal(err) }

            address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
            tcpConn, err := net.DialTimeout("tcp", address, time.Second)
            if err != nil { t.Fatalf("TCP connect probe unavailable: %v", err) }
            _ = tcpConn.Close()

            udpConn, err := net.DialTimeout("udp", address, time.Second)
            if err != nil { t.Fatalf("QUIC UDP address unavailable: %v", err) }
            _ = udpConn.Close()
            if server.tcpProbe == nil {
                t.Fatal("TCP latency probe not installed alongside QUIC listener")
            }
        })
    }
}
