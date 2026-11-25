package singbox

import (
    "context"
    "encoding/json"
    "fmt"
    "net"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    box "github.com/sagernet/sing-box"
    "github.com/sagernet/sing-box/adapter"
    "github.com/sagernet/sing-box/include"
    "github.com/sagernet/sing-box/option"
    singJSON "github.com/sagernet/sing/common/json"
    Mdata "github.com/sagernet/sing/common/metadata"
    "github.com/sagernet/sing/service"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/model"
)

// Real QUIC transport + proxied UDP payload with the strict admission hook.
// Unlike tracker-only tests, an authenticated Hysteria2/TUIC client must
// deliver a datagram through the actual sing-box server. The lease authority
// is deliberately local and synthetic: this is NOT a Laravel/Redis E2E test.
func TestRealQUICUDPStrictGlobalAdmission(t *testing.T) {
    const credential = "aaaaaaaa-1111-2222-3333-444444444444"
    for _, protocol := range []string{"hysteria2", "tuic"} {
        t.Run(protocol, func(t *testing.T) {
            target, err := net.ListenPacket("udp", "127.0.0.1:0")
            if err != nil { t.Fatal(err) }
            defer target.Close()

            var received atomic.Int32
            done := make(chan struct{})
            go func() {
                defer close(done)
                buf := make([]byte, 512)
                for {
                    _ = target.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
                    n, from, readErr := target.ReadFrom(buf)
                    if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
                        continue
                    }
                    if readErr != nil { return }
                    received.Add(1)
                    if string(buf[:n]) == "authorized-datagram" {
                        _, _ = target.WriteTo([]byte("authorized-response"), from)
                    }
                }
            }()
            defer func() { _ = target.Close(); <-done }()

            portProbe, err := net.ListenPacket("udp", "127.0.0.1:0")
            if err != nil { t.Fatal(err) }
            port := portProbe.LocalAddr().(*net.UDPAddr).Port
            _ = portProbe.Close()
            cert := testSelfSignedTLS(t)
            server := New(config.KernelConfig{
                Type: "singbox", LogLevel: "error", ConfigDir: t.TempDir(),
                CustomRoute: []map[string]any{{"ip_cidr": []string{"127.0.0.1/32"}, "outbound": "direct"}},
            })
            server.SetDeviceLimitFunc(func(uuid string) (int, bool) {
                return 1, uuid == credential
            })
            var allow atomic.Bool
            var attempts atomic.Int32
            var released atomic.Int32
            var revokeMu sync.Mutex
            var liveRevoke func()
            server.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, revoke func()) (func(), bool) {
                attempts.Add(1)
                if uid != 7 || ip != "127.0.0.1" || limit != 1 || revoke == nil {
                    t.Errorf("unexpected global admission identity uid=%d ip=%q limit=%d", uid, ip, limit)
                    return nil, false
                }
                if !allow.Load() { return nil, false }
                revokeMu.Lock()
                liveRevoke = revoke
                revokeMu.Unlock()
                return func() { released.Add(1) }, true
            })
            if err := server.Start(
                &model.NodeSpec{Protocol: protocol, ServerPort: port, ListenIP: "127.0.0.1", TLS: 1},
                []model.UserSpec{{ID: 7, UUID: credential}}, cert,
            ); err != nil { t.Fatal(err) }
            defer server.Stop()

            send := func(payload string, revokeActive bool) (string, error) {
                ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
                defer cancel()
                ctx = include.Context(ctx)
                outboundConfig := M{
                    "type": protocol,
                    "tag": "test-client",
                    "server": "127.0.0.1",
                    "server_port": port,
                    "password": credential,
                    "tls": M{"enabled": true, "server_name": "localhost", "certificate": string(cert.CertPEM)},
                }
                if protocol == "tuic" {
                    outboundConfig["uuid"] = credential
                    outboundConfig["tls"].(M)["alpn"] = []string{"h3"}
                }
                raw, err := json.Marshal(M{
                    "log": M{"level": "error"},
                    "outbounds": []M{outboundConfig},
                    "route": M{"final": "test-client"},
                })
                if err != nil { return "", err }
                opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, raw)
                if err != nil { return "", err }
                clientBox, err := box.New(box.Options{Context: ctx, Options: opts})
                if err != nil { return "", err }
                defer clientBox.Close()
                if err := clientBox.Start(); err != nil { return "", err }
                manager := service.FromContext[adapter.OutboundManager](ctx)
                if manager == nil { return "", fmt.Errorf("missing outbound manager") }
                outbound, ok := manager.Outbound("test-client")
                if !ok { return "", fmt.Errorf("missing client outbound") }
                packet, err := outbound.ListenPacket(ctx, Mdata.ParseSocksaddr(target.LocalAddr().String()))
                if err != nil { return "", err }
                defer packet.Close()
                if _, err := packet.WriteTo([]byte(payload), target.LocalAddr()); err != nil { return "", err }
                _ = packet.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
                response := make([]byte, 512)
                n, _, err := packet.ReadFrom(response)
                if err != nil { return "", err }
                if revokeActive {
                    // Revoke the still-open UDP session after a successful
                    // real QUIC round-trip. Further packets from that same
                    // client session must not reach the destination.
                    revokeMu.Lock()
                    stop := liveRevoke
                    revokeMu.Unlock()
                    if stop == nil { return "", fmt.Errorf("missing active QUIC lease revocation callback") }
                    stop()
                    // The client may observe a write error or a silent drop;
                    // either is acceptable, but delivering the packet is not.
                    _, _ = packet.WriteTo([]byte("after-revoke"), target.LocalAddr())
                    time.Sleep(350 * time.Millisecond)
                    if count := received.Load(); count != 1 {
                        return "", fmt.Errorf("revoked UDP session still forwarded packets: destination count=%d", count)
                    }
                }
                return string(response[:n]), nil
            }

            // Refused remote lease must prevent the proxied payload reaching
            // the UDP echo target. QUIC authentication itself may still run.
            if got, err := send("denied-datagram", false); err == nil {
                t.Fatalf("denied QUIC UDP payload unexpectedly returned %q", got)
            }
            if count := received.Load(); count != 0 {
                t.Fatalf("denied UDP datagram reached destination %d times (strict admission callback calls=%d; 0 indicates this protocol path bypassed the connection tracker)", count, attempts.Load())
            }
            if n := attempts.Load(); n == 0 {
                t.Fatal("QUIC UDP session never reached strict global admission callback")
            }

            allow.Store(true)
            got, err := send("authorized-datagram", true)
            if err != nil || got != "authorized-response" {
                t.Fatalf("authorized QUIC UDP roundtrip: got=%q err=%v", got, err)
            }
            if count := received.Load(); count != 1 {
                t.Fatalf("expected one authorized UDP datagram, got %d", count)
            }
            if attempts.Load() < 2 { t.Fatalf("missing remote admission checks: %d", attempts.Load()) }
            if released.Load() == 0 { t.Log("lease cleanup is asynchronous; final release is checked separately by manager tests") }
        })
    }
}
