package machine

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/panel"
)

// A node handle is inserted before the initial panel-config HTTP request.
// A concurrent removal must never block forever waiting for its completion
// if the request returns an error before the service goroutine launches.
func TestStopNodeDuringFailedInitialConfigAlwaysCompletes(t *testing.T) {
    requested:=make(chan struct{})
    release:=make(chan struct{})
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path!="/api/v2/server/config"{
            t.Errorf("unexpected request: %s",r.URL.Path)
            http.NotFound(w,r)
            return
        }
        close(requested)
        <-release
        http.Error(w,"temporary upstream error",http.StatusServiceUnavailable)
    }))
    defer server.Close()

    cfg:=&config.Config{
        Panel:config.PanelConfig{URL:server.URL},
        Machine:&config.MachineConfig{MachineID:24,Token:"test-token"},
        Kernel:config.KernelConfig{Type:"singbox"},
    }
    o:=New(cfg)
    startFinished:=make(chan struct{})
    go func(){
        o.startNode(context.Background(),panel.MachineNode{ID:25,Type:"naive",Name:"Naive"})
        close(startFinished)
    }()
    select{
    case <-requested:
    case <-time.After(3*time.Second):
        t.Fatal("node config request did not start")
    }

    stopFinished:=make(chan struct{})
    go func(){
        o.stopNode(25,"absent_from_panel_discovery")
        close(stopFinished)
    }()
    // The HTTP request is still in progress, so the stop operation must
    // wait for this in-flight startup to complete.
    select{
    case <-stopFinished:
        t.Fatal("stop unexpectedly finished before the fetch returned")
    default:
    }
    close(release)

    for name,finished:=range map[string]<-chan struct{}{
        "start":startFinished,
        "stop":stopFinished,
    } {
        select{
        case <-finished:
        case <-time.After(5*time.Second):
            t.Fatalf("%s hung while canceling a pre-service startup",name)
        }
    }
    o.mu.Lock()
    _,exists:=o.nodes[25]
    o.mu.Unlock()
    if exists {t.Fatal("failed config startup left a stale handle")}
    o.eventsMu.RLock()
    _,exists=o.mailboxes[25]
    o.eventsMu.RUnlock()
    if exists {t.Fatal("failed config startup left a stale mailbox")}
}
