package xray

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/http/httptest"
    "net/url"
    "sync/atomic"
    "testing"
    "time"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/kernel"
    "github.com/cedar2025/xboard-node/internal/model"
    xrayCore "github.com/xtls/xray-core/core"
    "github.com/xtls/xray-core/infra/conf/serial"
)

func reserveXrayLoopbackPort(t *testing.T) int {
    t.Helper()
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Fatal(err) }
    port := ln.Addr().(*net.TCPAddr).Port
    _ = ln.Close()
    return port
}

func TestRealXrayMuxTCPRevocation(t *testing.T) {
    const userID = 7
    const userUUID = "aaaaaaaa-1111-2222-3333-444444444444"
    target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/octet-stream")
        if r.URL.Path == "/stream" {
            _, _ = io.WriteString(w, "stream-start\n")
            if f, ok := w.(http.Flusher); ok { f.Flush() }
            <-r.Context().Done()
            return
        }
        _, _ = io.Copy(w, r.Body)
    }))
    defer target.Close()

    serverPort := reserveXrayLoopbackPort(t)
    clientPort := reserveXrayLoopbackPort(t)
    revocations := make(chan func(), 16)
    var releases atomic.Int64
    server := New(config.KernelConfig{Type: "xray", LogLevel: "error"})
    server.SetRevocableAdmissionFunc(func(uid int, sourceIP string, limit int, revoke func()) (func(), bool) {
        if uid != userID || limit != 1 || revoke == nil || sourceIP != "127.0.0.1" {
            return nil, false
        }
        select { case revocations <- revoke: default: }
        return func() { releases.Add(1) }, true
    })
    if err := server.Start(&model.NodeSpec{Protocol: "vless", ServerPort: serverPort, ListenIP: "127.0.0.1"},
        []model.UserSpec{{ID: userID, UUID: userUUID, DeviceLimit: 1}}, kernel.TLSCert{}); err != nil {
        t.Fatal(err)
    }
    defer server.Stop()

    clientCfg := M{
        "log": M{"loglevel": "error"},
        "inbounds": []M{{"listen":"127.0.0.1", "port": clientPort,
            "protocol":"socks", "settings": M{"auth":"noauth","udp":true}}},
        "outbounds": []M{{
            "tag":"remote", "protocol":"vless",
            "settings":M{"vnext":[]M{{"address":"127.0.0.1", "port":serverPort,
                "users":[]M{{"id":userUUID,"encryption":"none"}}}}},
            "streamSettings":M{"network":"tcp"},
            "mux":M{"enabled":true, "concurrency":4},
        }},
    }
    cfgBytes, err := json.Marshal(clientCfg)
    if err != nil { t.Fatal(err) }
    pb, err := serial.LoadJSONConfig(bytes.NewReader(cfgBytes))
    if err != nil { t.Fatalf("parse mux client config: %v", err) }
    clientInstance, err := xrayCore.New(pb)
    if err != nil { t.Fatalf("construct mux client: %v", err) }
    defer clientInstance.Close()
    if err := clientInstance.Start(); err != nil { t.Fatalf("start mux client: %v", err) }

    proxy, err := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", clientPort))
    if err != nil { t.Fatal(err) }
    transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
    defer transport.CloseIdleConnections()
    client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
    // Verify actual multiplexed-proxy TCP traffic, not a mock dispatcher.
    for i := 0; i < 3; i++ {
        payload := []byte(fmt.Sprintf("real-xray-mux-%d", i))
        response, err := client.Post(target.URL+"/echo", "application/octet-stream", bytes.NewReader(payload))
        if err != nil { t.Fatalf("MUX TCP roundtrip #%d: %v",i,err) }
        got, readErr := io.ReadAll(response.Body)
        _ = response.Body.Close()
        if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(got, payload) {
            t.Fatalf("MUX TCP roundtrip #%d: status=%d data=%q err=%v",i,response.StatusCode,got,readErr)
        }
    }
    // Hold a real stream open while the global lease is revoked.
    ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
    defer cancel()
    request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL+"/stream", nil)
    if err != nil { t.Fatal(err) }
    response, err := client.Do(request)
    if err != nil { t.Fatalf("open persistent MUX stream: %v", err) }
    defer response.Body.Close()
    head := make([]byte, len("stream-start\n"))
    if _, err := io.ReadFull(response.Body, head); err != nil || string(head) != "stream-start\n" {
        t.Fatalf("persistent MUX stream did not transfer initial data: %q err=%v",head,err)
    }
    var revoke func()
    select {
    case revoke = <-revocations:
    case <-time.After(3 * time.Second):
        t.Fatal("Xray strict global admission callback did not run")
    }
    // The callback must interrupt a live transport link after lease loss.
    revoke()
    done := make(chan error, 1)
    go func() {
        _, err := io.Copy(io.Discard, response.Body)
        done <- err
    }()
    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("live MUX stream not interrupted after global revocation")
    }
    t.Logf("Xray MUX real TCP revocation completed; released leases so far=%d",releases.Load())
}
