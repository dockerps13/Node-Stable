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
