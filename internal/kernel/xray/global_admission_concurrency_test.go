package xray

import (
    "fmt"
    "sync"
    "sync/atomic"
    "testing"
)

// This tests the Xray dispatcher's strict lease callback contract under
// concurrent admission, revocation, and final release. It is NOT a real
// MUX/XUDP packet-flow test; the latter needs a running Xray client/server.
func TestStrictXrayConcurrentLeaseRevocationLifecycle(t *testing.T) {
    const (
        workers = 24
        rounds = 40
    )
    d := newTestDispatcher()
    d.UpdateLimits(map[string]int{"user@7": 7}, map[string]int{"user@7": 2}, nil)

    var callbacks sync.Map
    var acquisitions atomic.Int64
    var revocations atomic.Int64
    var releases atomic.Int64
    d.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, revoke func()) (func(), bool) {
        if uid != 7 || limit != 2 || ip == "" || revoke == nil {
            return nil, false
        }
        acquisitions.Add(1)
        callbacks.Store(ip, revoke)
        var once sync.Once
        return func() {
            once.Do(func() { releases.Add(1) })
        }, true
    })

    var wg sync.WaitGroup
    failures := make(chan string, workers)
    for worker := 0; worker < workers; worker++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for i := 0; i < rounds; i++ {
                ip := fmt.Sprintf("198.18.%d.%d", id/250, id%250+1)
                release, ok := d.reserveMachineSlot("user@7", ip, func() {
                    revocations.Add(1)
                })
                if !ok || release == nil {
                    failures <- "strict authority rejected an expected grant"
                    return
                }
                stored, found := callbacks.LoadAndDelete(ip)
                if !found {
                    failures <- "strict authority did not retain revoke callback"
                    return
                }
                revokeFn := stored.(func())
                var concurrent sync.WaitGroup
                concurrent.Add(2)
                go func() { defer concurrent.Done(); revokeFn() }()
                go func() { defer concurrent.Done(); release() }()
                concurrent.Wait()
                release() // idempotent cleanup contract on the authority side
            }
        }(worker)
    }
    wg.Wait()
    close(failures)
    for failure := range failures {
        t.Error(failure)
    }
    expected := int64(workers * rounds)
    if got := acquisitions.Load(); got != expected {
        t.Fatalf("admission callback calls=%d, want %d", got, expected)
    }
    if got := revocations.Load(); got != expected {
        t.Fatalf("revocation notifications=%d, want %d", got, expected)
    }
    if got := releases.Load(); got != expected {
        t.Fatalf("lease release count=%d, want %d", got, expected)
    }
}
