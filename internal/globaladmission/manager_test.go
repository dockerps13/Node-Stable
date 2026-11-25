package globaladmission

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/panel"
)

type leaseFake struct {
	mu sync.Mutex
	actions []string
	renewErr error
	denyRenew bool
	ttl int
}

func (f *leaseFake) DeviceLease(_ context.Context, action string, _ int, _, _ string) (panel.DeviceLeaseReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	if action == "renew" {
		if f.renewErr != nil { return panel.DeviceLeaseReply{}, f.renewErr }
		if f.denyRenew {return panel.DeviceLeaseReply{Allowed:false}, nil}
	}
	return panel.DeviceLeaseReply{Allowed:true, LeaseTTLMS:f.ttl, DistinctIPs:1}, nil
}

func TestManagerRenewsAndReleases(t *testing.T) {
	f := &leaseFake{ttl:150}
	m := NewManager(context.Background(), f)
	m.tick = 15*time.Millisecond
	var lost atomic.Int64
	release, ok := m.Acquire(7,"192.0.2.1",1,func(){lost.Add(1)})
	if !ok {t.Fatal("acquire denied")}
	time.Sleep(65*time.Millisecond)
	release()
	time.Sleep(40*time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	var renewed, released bool
	for _, action := range f.actions {
		if action=="renew" {renewed=true}
		if action=="release" {released=true}
	}
	if !renewed || !released || lost.Load()!=0 { t.Fatalf("actions=%v lost=%d",f.actions,lost.Load()) }
}

func TestManagerDenialRevokesConnection(t *testing.T) {
	f := &leaseFake{ttl:300, denyRenew:true}
	m := NewManager(context.Background(),f)
	m.tick = 10*time.Millisecond
	lost := make(chan struct{},1)
	release, ok := m.Acquire(7,"192.0.2.1",1,func(){select {case lost<-struct{}{}:default:}})
	if !ok {t.Fatal("acquire denied")}
	defer release()
	select {
	case <-lost:
	case <-time.After(time.Second): t.Fatal("lease denial did not revoke connection")
	}
}

func TestManagerTransportFailureExpires(t *testing.T) {
	f := &leaseFake{ttl:70,renewErr:errors.New("redis unavailable")}
	m := NewManager(context.Background(),f)
	m.tick = 10*time.Millisecond
	lost := make(chan struct{},1)
	release, ok := m.Acquire(7,"192.0.2.1",1,func(){select {case lost<-struct{}{}:default:}})
	if !ok {t.Fatal("acquire denied")}
	defer release()
	select {
	case <-lost:
	case <-time.After(time.Second): t.Fatal("lease was extended on transport failure")
	}
}

func TestManagerRequiresRevoker(t *testing.T) {
	f := &leaseFake{ttl:100}
	m := NewManager(context.Background(),f)
	if _,ok:=m.Acquire(7,"192.0.2.1",1,nil);ok {t.Fatal("missing revoke callback admitted")}
}

func TestManagerExpiresWithoutRenewTick(t *testing.T) {
	f := &leaseFake{ttl: 90}
	m := NewManager(context.Background(), f)
	m.tick = time.Hour // longer than Redis lease TTL
	lost := make(chan struct{}, 1)
	release, ok := m.Acquire(7, "192.0.2.1", 1, func() {
		select {
		case lost <- struct{}{}:
		default:
		}
	})
	if !ok {
		t.Fatal("initial lease denied")
	}
	defer release()
	select {
	case <-lost:
		// Successful: expiry is independent of renewal scheduling.
	case <-time.After(750 * time.Millisecond):
		t.Fatal("lease persisted beyond confirmed TTL without a renewal tick")
	}
}

type blockedRenewClient struct{}

func (blockedRenewClient) DeviceLease(ctx context.Context, action string, _ int, _, _ string) (panel.DeviceLeaseReply, error) {
	if action == "acquire" {
		return panel.DeviceLeaseReply{Allowed: true, LeaseTTLMS: 110}, nil
	}
	if action == "renew" {
		<-ctx.Done()
		return panel.DeviceLeaseReply{}, ctx.Err()
	}
	return panel.DeviceLeaseReply{Allowed: true}, nil
}

func TestManagerSlowRenewCannotExtendExpiredLease(t *testing.T) {
	m := NewManager(context.Background(), blockedRenewClient{})
	m.tick = 15 * time.Millisecond
	m.timeout = 2 * time.Second
	revoked := make(chan struct{}, 1)
	release, ok := m.Acquire(7, "192.0.2.1", 1, func() {
		select {
		case revoked <- struct{}{}:
		default:
		}
	})
	if !ok {
		t.Fatal("initial short lease denied")
	}
	defer release()
	select {
	case <-revoked:
	case <-time.After(600 * time.Millisecond):
		t.Fatal("stalled renewal outlived confirmed lease deadline")
	}
}

 
// Multiple real connections behind one canonical IP must share a single
// remote lease. Closing one connection must not free the remote slot while
// another connection still owns it.
func TestManagerSharedIPReleasesOnlyFinalOwner(t *testing.T) {
	f := &leaseFake{ttl: 5000}
	m := NewManager(context.Background(), f)
	m.tick = time.Hour

	releaseA, ok := m.Acquire(7, "192.0.2.1", 1, func() {})
	if !ok { t.Fatal("first connection denied") }
	releaseB, ok := m.Acquire(7, "::ffff:192.0.2.1", 1, func() {})
	if !ok {
		releaseA()
		t.Fatal("same canonical IP denied")
	}

	f.mu.Lock()
	acquires := 0
	for _, action := range f.actions {
		if action == "acquire" { acquires++ }
	}
	f.mu.Unlock()
	if acquires != 1 { t.Fatalf("expected one remote acquisition, got %d", acquires) }

	releaseA()
	releaseA() // connection cleanup must be idempotent
	f.mu.Lock()
	releases := 0
	for _, action := range f.actions {
		if action == "release" { releases++ }
	}
	f.mu.Unlock()
	if releases != 0 { t.Fatalf("premature remote release while second connection remains: %d", releases) }

	releaseB()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.WaitReleases(ctx); err != nil { t.Fatalf("release RPC did not finish: %v", err) }
	f.mu.Lock()
	defer f.mu.Unlock()
	releases = 0
	for _, action := range f.actions {
		if action == "release" { releases++ }
	}
	if releases != 1 { t.Fatalf("expected exactly one final remote release, got %d; actions=%v", releases, f.actions) }
}

// Shutdown is not an authorization grant: cancelling the owning node context
// must revoke an active connection even if the next renewal is far away.
func TestManagerContextCancellationRevokesActiveLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &leaseFake{ttl: 5000}
	m := NewManager(ctx, f)
	m.tick = time.Hour

	lost := make(chan struct{}, 1)
	release, ok := m.Acquire(7, "192.0.2.20", 1, func() {
		select { case lost <- struct{}{}: default: }
	})
	if !ok { t.Fatal("acquire denied") }
	defer release()
	cancel()

	select {
	case <-lost:
	case <-time.After(time.Second):
		t.Fatal("node context cancellation left connection admitted")
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := m.WaitReleases(waitCtx); err != nil { t.Fatalf("shutdown remote release did not finish: %v", err) }
	f.mu.Lock()
	defer f.mu.Unlock()
	releases := 0
	for _, action := range f.actions {
		if action == "release" { releases++ }
	}
	if releases != 1 { t.Fatalf("expected one release after shutdown, got %d; actions=%v", releases, f.actions) }
}
