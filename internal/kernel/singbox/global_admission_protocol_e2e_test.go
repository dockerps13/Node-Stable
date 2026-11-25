package singbox

import (
    "context"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/http/httptest"
    "net/url"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/kernel"
    "github.com/cedar2025/xboard-node/internal/model"
)

// Exercises an actual authenticated sing-box HTTP inbound, rather than calling
// ConnTracker directly. All traffic stays on loopback; no production node,
// credentials or panel is involved. A real Laravel/Redis-backed protocol
// acceptance is still required before releasing v1.0.5.
func TestRealHTTPProxyStrictGlobalAdmission(t *testing.T) {
    const credential = "aaaaaaaa-1111-2222-3333-444444444444"
    var reached atomic.Int32
    target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reached.Add(1)
        _, _ = io.WriteString(w, "admitted-proxy-payload")
    }))
    defer target.Close()

    reservation, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Fatal(err) }
    port := reservation.Addr().(*net.TCPAddr).Port
    _ = reservation.Close()

    proxy := New(config.KernelConfig{
        Type: "singbox", LogLevel: "error", ConfigDir: t.TempDir(),
        CustomRoute: []map[string]any{{"ip_cidr": []string{"127.0.0.1/32"}, "outbound": "direct"}},
    })
    proxy.SetDeviceLimitFunc(func(uuid string) (int, bool) {
        return 1, uuid == credential
    })

    var allowed atomic.Bool
    var mu sync.Mutex
    var revoke func()
    var admissions atomic.Int32
    var releases atomic.Int32
    proxy.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, closeConnection func()) (func(), bool) {
        if uid != 7 || ip != "127.0.0.1" || limit != 1 {
            t.Errorf("unexpected authenticated admission: uid=%d ip=%q limit=%d", uid, ip, limit)
            return nil, false
        }
        admissions.Add(1)
        if !allowed.Load() { return nil, false }
        mu.Lock()
        revoke = closeConnection
        mu.Unlock()
        return func() { releases.Add(1) }, true
    })
    if err := proxy.Start(
        &model.NodeSpec{Protocol:"http", ListenIP:"127.0.0.1", ServerPort:port},
        []model.UserSpec{{ID:7, UUID:credential}}, kernel.TLSCert{},
    ); err != nil { t.Fatal(err) }
    defer proxy.Stop()

    proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%s@127.0.0.1:%d", credential, credential, port))
    if err != nil { t.Fatal(err) }
    request := func() (string, error) {
        transport := &http.Transport{Proxy:http.ProxyURL(proxyURL), DisableKeepAlives:true}
        defer transport.CloseIdleConnections()
        client := &http.Client{Transport:transport, Timeout:3*time.Second}
        req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target.URL, nil)
        if err != nil { return "", err }
        resp, err := client.Do(req)
        if err != nil { return "", err }
        defer resp.Body.Close()
        body, err := io.ReadAll(resp.Body)
        if err != nil { return "", err }
        if resp.StatusCode != http.StatusOK { return "", fmt.Errorf("proxy returned HTTP %d",resp.StatusCode) }
        return string(body), nil
    }

    // A refused remote lease must never allow the target to receive data.
    if _, err := request(); err == nil {
        t.Fatal("denied global admission passed through the real HTTP proxy")
    }
    if got := reached.Load(); got != 0 {
        t.Fatalf("denied request reached upstream %d times",got)
    }
    allowed.Store(true)
    payload, err := request()
    if err != nil || payload != "admitted-proxy-payload" {
        t.Fatalf("allowed real proxy roundtrip: payload=%q err=%v",payload,err)
    }
    if got := reached.Load(); got != 1 { t.Fatalf("expected exactly one delivered request, got %d",got) }
    if admissions.Load() < 2 { t.Fatalf("expected denied and permitted lease checks, got %d",admissions.Load()) }

    mu.Lock()
    savedRevoke := revoke
    mu.Unlock()
    if savedRevoke == nil { t.Fatal("admitted session did not register a remote revoke callback") }
    // The request has finished, so the release callback must already have run.
    // Calling revoke after disconnect must remain idempotent and not restore access.
    savedRevoke()
    allowed.Store(false)
    if _, err := request(); err == nil {
        t.Fatal("remote quota revocation allowed a new proxy connection")
    }
    if got := reached.Load(); got != 1 {
        t.Fatalf("request reached upstream after quota withdrawal: %d",got)
    }
    deadline := time.Now().Add(2*time.Second)
    for releases.Load() < 1 && time.Now().Before(deadline) { time.Sleep(10*time.Millisecond) }
    if releases.Load() < 1 { t.Fatal("admitted proxy connection never released its lease") }
}
