package globaladmission

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerCoalescesConnectionsAndLastRelease(t *testing.T) {
	f:=&leaseFake{ttl:30000}
	m:=NewManager(context.Background(),f)
	m.tick=time.Hour
	const n=4000
	var wg sync.WaitGroup
	var mu sync.Mutex
	releases:=make([]func(),0,n)
	for i:=0;i<n;i++ {
		wg.Add(1)
		go func(){
			defer wg.Done()
			release,ok:=m.Acquire(7,"::ffff:192.0.2.1",1,func(){})
			if !ok {t.Error("shared IP rejected");return}
			mu.Lock()
			releases=append(releases,release)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(releases)!=n {t.Fatalf("acquired %d want %d",len(releases),n)}
	f.mu.Lock()
	acquires:=0
	for _,action:=range f.actions {if action=="acquire" {acquires++}}
	f.mu.Unlock()
	if acquires!=1 {t.Fatalf("%d remote acquisitions for one IP, want 1",acquires)}

	for i:=0;i<n-1;i++ {releases[i]()}
	time.Sleep(20*time.Millisecond)
	f.mu.Lock()
	for _,action:=range f.actions {if action=="release" {t.Fatal("released before final connection")}}
	f.mu.Unlock()

	releases[n-1]()
	deadline:=time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		received:=false
		for _,action:=range f.actions {if action=="release" {received=true}}
		f.mu.Unlock()
		if received {return}
		time.Sleep(5*time.Millisecond)
	}
	t.Fatal("final connection did not release shared lease")
}

func TestManagerRevokesAllSharedConnections(t *testing.T) {
	f:=&leaseFake{ttl:300,denyRenew:true}
	m:=NewManager(context.Background(),f)
	m.tick=10*time.Millisecond
	var lost atomic.Int64
	release1,ok:=m.Acquire(7,"192.0.2.1",1,func(){lost.Add(1)})
	if !ok {t.Fatal("first acquire failed")}
	defer release1()
	release2,ok:=m.Acquire(7,"192.0.2.1",1,func(){lost.Add(1)})
	if !ok {t.Fatal("second acquire failed")}
	defer release2()
	deadline:=time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if lost.Load()==2 {return}
		time.Sleep(5*time.Millisecond)
	}
	t.Fatalf("lease denial revoked %d connections, want 2",lost.Load())
}
