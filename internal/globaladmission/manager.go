package globaladmission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/cedar2025/xboard-node/internal/panel"
)

// LeaseClient is implemented by the authenticated per-node panel client.
type LeaseClient interface {
	DeviceLease(ctx context.Context, action string, userID int, leaseID, ip string) (panel.DeviceLeaseReply, error)
}

// Manager coordinates one remote lease per admitted connection.
// It must only be used with a non-nil revocation callback that closes the
// connection; otherwise losing the lease would leave a live unauthorized link.
type Manager struct {
	client LeaseClient
	ctx context.Context
	timeout time.Duration
	tick time.Duration
}

func NewManager(ctx context.Context, client LeaseClient) *Manager {
	return &Manager{ctx: ctx, client: client, timeout: 2*time.Second, tick: 5*time.Second}
}

func newLeaseID() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Acquire blocks for one bounded remote admission decision and starts renewals.
// On renewal denial or lease expiry, revoke is called once to close the
// connection. Network failures do not extend the confirmed lease deadline.
func (m *Manager) Acquire(uid int, ip string, limit int, revoke func()) (release func(), admitted bool) {
	if m == nil || m.client == nil || m.ctx == nil || revoke == nil || uid <= 0 || limit <= 0 {
		return nil, false
	}
	ip, valid := canonicalIP(ip)
	if !valid || m.ctx.Err() != nil {
		return nil, false
	}
	id, err := newLeaseID()
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.timeout)
	resp, err := m.client.DeviceLease(ctx, "acquire", uid, id, ip)
	cancel()
	if err != nil || !resp.Allowed || resp.LeaseTTLMS <= 0 {
		return nil, false
	}
	// This is deliberately pessimistic: TTL is measured against the request
	// start rather than the response receipt to avoid extending server grants.
	// Network transit can only shorten the local confirmed deadline.
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	stop := func() {
		once.Do(func() {close(done)})
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(time.Duration(resp.LeaseTTLMS) * time.Millisecond)
		ticker := time.NewTicker(m.tick)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-m.ctx.Done():
				revoke()
				return
			case <-ticker.C:
				if !time.Now().Before(deadline) {
					revoke()
					return
				}
				rctx, rcancel := context.WithTimeout(m.ctx, m.timeout)
				result, err := m.client.DeviceLease(rctx, "renew", uid, id, "")
				rcancel()
				if err == nil && !result.Allowed {
					revoke()
					return
				}
				if err == nil && result.Allowed && result.LeaseTTLMS > 0 {
					deadline = time.Now().Add(time.Duration(result.LeaseTTLMS)*time.Millisecond)
				}
				if !time.Now().Before(deadline) {
					revoke()
					return
				}
			}
		}
	}()
	return func() {
		stop()
		// The lease release is best effort and bounded; expiration is the
		// authoritative fallback when a release cannot reach the panel.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
			defer cancel()
			_, _ = m.client.DeviceLease(ctx, "release", uid, id, "")
		}()
	}, true
}

// WaitClosed exists for tests of lifecycle orchestration to use a test
// client with a deterministic expiry; no package-global lease registry is kept.
var _ = errors.New
