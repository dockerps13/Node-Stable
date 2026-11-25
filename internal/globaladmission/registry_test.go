package globaladmission

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTwoNodesCompeteForTwoSlots(t *testing.T) {
	r := New()
	const requests = 100
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := "vps-a"
			if i%2 == 1 {
				node = "vps-b"
			}
			if r.Acquire("panel-a", 7, fmt.Sprintf("198.51.100.%d", i+1),
				2, node, fmt.Sprintf("conn-%d", i), time.Minute) {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := accepted.Load(); got != 2 {
		t.Fatalf("accepted=%d, want 2", got)
	}
	if got := r.DistinctIPs("panel-a", 7); got != 2 {
		t.Fatalf("distinct=%d, want 2", got)
	}
}

func TestSameIPAcrossNodesSharesSlot(t *testing.T) {
	r := New()
	for _, node := range []string{"vps-a", "vps-b"} {
		if !r.Acquire("panel-a", 7, "192.0.2.1", 1, node, "conn-1", time.Minute) {
			t.Fatalf("%s failed to share slot", node)
		}
	}
	if r.DistinctIPs("panel-a", 7) != 1 {
		t.Fatal("same IP counted twice")
	}
	r.Release("panel-a", 7, "vps-a", "conn-1")
	if r.Acquire("panel-a", 7, "192.0.2.2", 1, "vps-c", "conn-1", time.Minute) {
		t.Fatal("one VPS released another VPS's slot")
	}
	r.Release("panel-a", 7, "vps-b", "conn-1")
	if !r.Acquire("panel-a", 7, "192.0.2.2", 1, "vps-c", "conn-1", time.Minute) {
		t.Fatal("slot was not released")
	}
}

func TestExpiredLeaseAndRenewal(t *testing.T) {
	r := New()
	current := time.Unix(1000, 0)
	r.now = func() time.Time { return current }
	if !r.Acquire("panel-a", 7, "192.0.2.1", 1, "vps-a", "lease-a", 10*time.Second) {
		t.Fatal("initial acquire failed")
	}
	current = current.Add(5 * time.Second)
	if !r.Renew("panel-a", 7, "vps-a", "lease-a", 10*time.Second) {
		t.Fatal("renewal failed")
	}
	current = current.Add(6 * time.Second)
	if r.Acquire("panel-a", 7, "192.0.2.2", 1, "vps-b", "lease-b", 10*time.Second) {
		t.Fatal("renewed lease lost its slot")
	}
	current = current.Add(5 * time.Second)
	if r.Renew("panel-a", 7, "vps-a", "lease-a", time.Minute) {
		t.Fatal("expired lease was resurrected")
	}
	if !r.Acquire("panel-a", 7, "192.0.2.2", 1, "vps-b", "lease-b", 10*time.Second) {
		t.Fatal("expired slot was not recovered")
	}
}

func TestFailClosedAndNormalizeIP(t *testing.T) {
	r := New()
	if r.Acquire("panel-a", 7, "invalid-ip", 1, "vps-a", "x", time.Minute) {
		t.Fatal("invalid IP admitted")
	}
	if r.Acquire("", 7, "192.0.2.1", 1, "vps-a", "x", time.Minute) {
		t.Fatal("missing scope admitted")
	}
	if r.Acquire("panel-a", 0, "192.0.2.1", 1, "vps-a", "x", time.Minute) {
		t.Fatal("invalid user admitted")
	}
	if r.Acquire("panel-a", 7, "192.0.2.1", 0, "vps-a", "x", time.Minute) {
		t.Fatal("invalid limit admitted")
	}
	if !r.Acquire("panel-a", 7, "::ffff:192.0.2.1", 1, "vps-a", "x", time.Minute) {
		t.Fatal("IPv4-mapped IPv6 rejected")
	}
	if !r.Acquire("panel-a", 7, "192.0.2.1", 1, "vps-b", "y", time.Minute) {
		t.Fatal("normalized identical IP rejected")
	}
	if r.Acquire("panel-a", 7, "192.0.2.2", 1, "vps-a", "x", time.Minute) {
		t.Fatal("owner reused with another IP")
	}
	if r.DistinctIPs("panel-a", 7) != 1 {
		t.Fatal("normalized IP counted twice")
	}
}

func TestIsolationAndIdempotency(t *testing.T) {
	r := New()
	if !r.Acquire("panel-a", 7, "192.0.2.1", 1, "node-1", "lease-1", time.Minute) {
		t.Fatal("first acquire failed")
	}
	if !r.Acquire("panel-a", 7, "192.0.2.1", 1, "node-1", "lease-1", time.Minute) {
		t.Fatal("idempotent retry failed")
	}
	for _, pair := range []struct {
		scope string
		user  int
	}{{"panel-b", 7}, {"panel-a", 8}} {
		if !r.Acquire(pair.scope, pair.user, "192.0.2.2", 1, "node-1", "lease-1", time.Minute) {
			t.Fatalf("scope/user isolation failed: %+v", pair)
		}
	}
	r.Release("panel-a", 7, "node-1", "lease-1")
	r.Release("panel-a", 7, "node-1", "lease-1")
	if r.DistinctIPs("panel-a", 7) != 0 {
		t.Fatal("idempotent release failed")
	}
	if r.DistinctIPs("panel-b", 7) != 1 {
		t.Fatal("release affected another panel")
	}
}

func TestMissingLeaseCannotBeRenewed(t *testing.T) {
	r := New()
	if r.Renew("panel-a", 7, "node-1", "missing", time.Minute) {
		t.Fatal("missing lease renewed")
	}
	r.Release("panel-a", 7, "node-1", "missing")
}
