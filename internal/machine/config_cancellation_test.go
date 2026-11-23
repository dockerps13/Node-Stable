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

// The panel may hang before sending any config response. Removing this node
// must cancel its HTTP request instead of waiting for the global 30s timeout.
func TestStopNodeCancelsPendingInitialConfigRequest(t *testing.T) {
	requested := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/server/config" {
			http.NotFound(w, r)
			return
		}
		close(requested)
		<-r.Context().Done()
		close(handlerDone)
	}))
	defer server.Close()

	cfg := &config.Config{
		Panel: config.PanelConfig{URL: server.URL},
		Machine: &config.MachineConfig{MachineID: 24, Token: "test-token"},
		Kernel: config.KernelConfig{Type: "singbox"},
	}
	o := New(cfg)
	startFinished := make(chan struct{})
	go func() {
		defer close(startFinished)
		o.startNode(context.Background(), panel.MachineNode{ID: 25, Type: "naive", Name: "Naive"})
	}()
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("initial config fetch not started")
	}

	stopFinished := make(chan struct{})
	go func() {
		o.stopNode(25, "absent_from_panel_discovery")
		close(stopFinished)
	}()
	for name, done := range map[string]<-chan struct{}{
		"start": startFinished,
		"stop": stopFinished,
		"http": handlerDone,
	} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not terminate after node removal", name)
		}
	}
	o.mu.Lock()
	_, running := o.nodes[25]
	o.mu.Unlock()
	if running {
		t.Fatal("removed node retained its handle after cancellation")
	}
	o.eventsMu.RLock()
	_, registered := o.mailboxes[25]
	o.eventsMu.RUnlock()
	if registered {
		t.Fatal("removed node retained its mailbox after cancellation")
	}
}
