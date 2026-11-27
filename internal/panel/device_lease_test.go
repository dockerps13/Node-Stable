package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func leaseClient(server *httptest.Server) *Client {
	return &Client{
		baseURL: server.URL, token: "machine-secret", machineID: 24, nodeID: 16,
		httpClient: &http.Client{Timeout: time.Second},
	}
}

func TestDeviceLeaseAuthAndReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/server/device-lease" || r.Method != http.MethodPost {
			t.Errorf("wrong endpoint: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["token"] != "machine-secret" || body["machine_id"] != float64(24) ||
			body["node_id"] != float64(16) || body["user_id"] != float64(7) ||
			body["ip"] != "192.0.2.1" || body["action"] != "acquire" {
			t.Errorf("missing machine credentials or request fields: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":true,"distinct_ips":1,"lease_ttl_ms":30000}`))
	}))
	defer server.Close()
	reply, err := leaseClient(server).DeviceLease(context.Background(), "acquire", 7, "random-lease-123456", "192.0.2.1")
	if err != nil || !reply.Allowed || reply.DistinctIPs != 1 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
}

func TestDeviceLeaseFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"forbidden", 403, `{"allowed":true,"distinct_ips":1,"lease_ttl_ms":30000}`},
		{"unavailable", 503, `{"allowed":true,"distinct_ips":1,"lease_ttl_ms":30000}`},
		{"empty", 200, ""},
		{"missing", 200, `{"allowed":true}`},
		{"nonpositiveTTL", 200, `{"allowed":true,"distinct_ips":1,"lease_ttl_ms":0}`},
		{"malformed", 200, "{bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			reply, err := leaseClient(server).DeviceLease(context.Background(), "acquire", 7, "random-lease-123456", "192.0.2.1")
			if err == nil || reply.Allowed {
				t.Fatalf("unexpected grant reply=%+v err=%v", reply, err)
			}
		})
	}
}

func TestDeviceLeaseRequiresMachineAuth(t *testing.T) {
	c := &Client{machineID: 0, nodeID: 16}
	if grant, err := c.DeviceLease(context.Background(), "acquire", 7, "lease", "192.0.2.1"); err == nil || grant.Allowed {
		t.Fatalf("expected machine-auth failure, got %+v %v", grant, err)
	}
}

func TestDeviceLeaseRespectsDeadline(t *testing.T) {
	// The handler simulates a stalled panel. A separate stop channel makes
	// teardown deterministic even when an HTTP request's server-side context
	// is not canceled promptly after the client disconnects.
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer func() {
		close(stop)
		server.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if reply, err := leaseClient(server).DeviceLease(ctx, "acquire", 7, "random-lease-123456", "192.0.2.1"); err == nil || reply.Allowed {
		t.Fatalf("unexpected grant reply=%+v err=%v", reply, err)
	}
	if time.Since(start) > 700*time.Millisecond {
		t.Fatal("deadline not respected")
	}
}
