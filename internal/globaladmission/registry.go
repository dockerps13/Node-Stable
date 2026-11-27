// Package globaladmission defines reference lease semantics for a future
// centralized device admission service. This in-memory implementation is NOT
// distributed and must not be used as an enforcement authority across VPSes.
package globaladmission

import (
	"net/netip"
	"sync"
	"time"
)

type account struct {
	scope string
	user  int
}
type owner struct {
	node string
	id   string
}
type lease struct {
	ip      string
	expires time.Time
}

// Registry is an in-memory concurrency-safe reference model.
// Distinct public IP addresses, not physical devices, consume slots.
type Registry struct {
	mu     sync.Mutex
	now    func() time.Time
	leases map[account]map[owner]lease
}

func New() *Registry {
	return &Registry{now: time.Now, leases: make(map[account]map[owner]lease)}
}

func canonicalIP(raw string) (string, bool) {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", false
	}
	return addr.WithZone("").Unmap().String(), true
}

func (r *Registry) prune(k account, at time.Time) map[owner]lease {
	leases := r.leases[k]
	for id, value := range leases {
		if !value.expires.After(at) {
			delete(leases, id)
		}
	}
	if len(leases) == 0 {
		delete(r.leases, k)
		return nil
	}
	return leases
}

// Acquire atomically reserves an owner lease. Different owners sharing the
// same IP consume a single slot. An existing owner cannot change its IP.
// A successful retry for the same owner and IP extends its lease.
func (r *Registry) Acquire(scope string, userID int, rawIP string, limit int, node, leaseID string, ttl time.Duration) bool {
	if r == nil || scope == "" || userID <= 0 || limit <= 0 || node == "" || leaseID == "" || ttl <= 0 {
		return false
	}
	ip, ok := canonicalIP(rawIP)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	at := r.now()
	k := account{scope, userID}
	id := owner{node, leaseID}
	owners := r.prune(k, at)
	if existing, exists := owners[id]; exists && existing.ip != ip {
		return false
	}

	ips := make(map[string]struct{}, len(owners))
	for _, value := range owners {
		ips[value.ip] = struct{}{}
	}
	if _, exists := ips[ip]; !exists && len(ips) >= limit {
		return false
	}
	if owners == nil {
		owners = make(map[owner]lease)
		r.leases[k] = owners
	}
	owners[id] = lease{ip: ip, expires: at.Add(ttl)}
	return true
}

// Renew extends only an existing, unexpired lease. It never creates a lease.
func (r *Registry) Renew(scope string, userID int, node, leaseID string, ttl time.Duration) bool {
	if r == nil || scope == "" || userID <= 0 || node == "" || leaseID == "" || ttl <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	k := account{scope, userID}
	id := owner{node, leaseID}
	at := r.now()
	owners := r.prune(k, at)
	value, exists := owners[id]
	if !exists {
		return false
	}
	value.expires = at.Add(ttl)
	owners[id] = value
	return true
}

// Release removes only the named owner's lease. Multiple connections on
// different nodes using the same IP must not release one another.
func (r *Registry) Release(scope string, userID int, node, leaseID string) {
	if r == nil || scope == "" || userID <= 0 || node == "" || leaseID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	k := account{scope, userID}
	owners := r.leases[k]
	delete(owners, owner{node, leaseID})
	if len(owners) == 0 {
		delete(r.leases, k)
	}
}

func (r *Registry) DistinctIPs(scope string, userID int) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	owners := r.prune(account{scope, userID}, r.now())
	ips := make(map[string]struct{}, len(owners))
	for _, value := range owners {
		ips[value.ip] = struct{}{}
	}
	return len(ips)
}
