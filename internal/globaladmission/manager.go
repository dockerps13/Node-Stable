package globaladmission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/cedar2025/xboard-node/internal/panel"
)

// LeaseClient is the authenticated per-node panel API. This is not a Redis
// client: machine credentials never give VPS access to Redis itself.
type LeaseClient interface {
	DeviceLease(ctx context.Context, action string, userID int, leaseID, ip string) (panel.DeviceLeaseReply, error)
}

type leaseKey struct {
	uid int
	ip string
}

type leaseState struct {
	ready chan struct{} // closed when the initial request has finished
	done chan struct{}
	once sync.Once
	id string
	deadline time.Time
	callbacks map[uint64]func()
}

// Manager multiplexes all connections sharing one node/user/IP onto one
// remote owner lease, preventing HTTP requests per TCP/UDP connection.
// Every connection has its own idempotent release and revocation callback.
type Manager struct {
	client LeaseClient
	ctx context.Context
	timeout time.Duration
	tick time.Duration
	mu sync.Mutex
	slots map[leaseKey]*leaseState
	next uint64
}

func NewManager(ctx context.Context, client LeaseClient) *Manager {
	return &Manager{
		ctx:ctx, client:client, timeout:2*time.Second, tick:5*time.Second,
		slots:make(map[leaseKey]*leaseState),
	}
}

func newLeaseID() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {return "",err}
	return hex.EncodeToString(b[:]),nil
}

// Acquire obtains a shared lease, attaching revoke to exactly this connection.
// An incoming request for an IP with an acquisition in flight waits for that
// result rather than making another panel request.
func (m *Manager) Acquire(uid int, rawIP string, limit int, revoke func()) (func(),bool) {
	if m==nil || m.client==nil || m.ctx==nil || revoke==nil || uid<=0 || limit<=0 {return nil,false}
	ip,valid:=canonicalIP(rawIP)
	if !valid || m.ctx.Err()!=nil {return nil,false}
	key:=leaseKey{uid,ip}
	for {
		m.mu.Lock()
		if m.slots==nil {m.slots=make(map[leaseKey]*leaseState)}
		if state:=m.slots[key];state!=nil {
			wait:=state.ready
			m.mu.Unlock()
			select {
			case <-wait:
			case <-m.ctx.Done():return nil,false
			}
			m.mu.Lock()
			if m.slots[key]!=state {
				// Fail all waiters together on a denied/unavailable authority.
				// Never serially stampede the panel with repeated acquisitions.
				m.mu.Unlock()
				return nil,false
			}
			if state.id=="" || !time.Now().Before(state.deadline) {
				m.mu.Unlock()
				return nil,false
			}
			m.next++
			cid:=m.next
			state.callbacks[cid]=revoke
			m.mu.Unlock()
			return m.releaseFunc(key,state,cid),true
		}
		state:=&leaseState{ready:make(chan struct{}),done:make(chan struct{}),callbacks:make(map[uint64]func())}
		m.slots[key]=state
		m.mu.Unlock()

		id,err:=newLeaseID()
		if err!=nil {m.abort(key,state);return nil,false}
		started:=time.Now()
		ctx,cancel:=context.WithTimeout(m.ctx,m.timeout)
		resp,err:=m.client.DeviceLease(ctx,"acquire",uid,id,ip)
		cancel()
		if err!=nil || !resp.Allowed || resp.LeaseTTLMS<=0 || m.ctx.Err()!=nil {
			m.abort(key,state)
			return nil,false
		}
		expires:=started.Add(time.Duration(resp.LeaseTTLMS)*time.Millisecond)
		if !time.Now().Before(expires) {
			m.abort(key,state)
			return nil,false
		}

		m.mu.Lock()
		if m.slots[key]!=state {
			m.mu.Unlock()
			return nil,false
		}
		state.id=id
		state.deadline=expires
		m.next++
		cid:=m.next
		state.callbacks[cid]=revoke
		close(state.ready)
		m.mu.Unlock()
		go m.renew(key,state)
		return m.releaseFunc(key,state,cid),true
	}
}

func (m *Manager) abort(key leaseKey,state *leaseState) {
	m.mu.Lock()
	if m.slots[key]==state {delete(m.slots,key)}
	close(state.ready)
	m.mu.Unlock()
}

func (m *Manager) releaseFunc(key leaseKey,state *leaseState,cid uint64) func() {
	var once sync.Once
	return func(){
		once.Do(func(){
			m.mu.Lock()
			if m.slots[key]!=state {m.mu.Unlock();return}
			delete(state.callbacks,cid)
			empty:=len(state.callbacks)==0
			if empty {delete(m.slots,key);state.once.Do(func(){close(state.done)})}
			m.mu.Unlock()
			if empty {m.releaseRemote(key,state.id)}
		})
	}
}

func (m *Manager) releaseRemote(key leaseKey,id string) {
	if id=="" {return}
	go func(){
		ctx,cancel:=context.WithTimeout(context.Background(),m.timeout)
		defer cancel()
		_,_=m.client.DeviceLease(ctx,"release",key.uid,id,"")
	}()
}

func (m *Manager) lose(key leaseKey,state *leaseState) {
	m.mu.Lock()
	if m.slots[key]!=state {m.mu.Unlock();return}
	delete(m.slots,key)
	state.once.Do(func(){close(state.done)})
	callbacks:=make([]func(),0,len(state.callbacks))
	for _,revoke:=range state.callbacks {callbacks=append(callbacks,revoke)}
	state.callbacks=nil
	m.mu.Unlock()
	for _,revoke:=range callbacks {revoke()}
	m.releaseRemote(key,state.id)
}

func (m *Manager) renew(key leaseKey,state *leaseState) {
	ticker:=time.NewTicker(m.tick)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		if m.slots[key]!=state {m.mu.Unlock();return}
		deadline:=state.deadline
		m.mu.Unlock()
		remaining:=time.Until(deadline)
		if remaining<=0 {m.lose(key,state);return}

		// An independent expiry timer is mandatory: the renewal cadence
		// may exceed the granted TTL (especially during tests or config
		// changes). Never keep a connection authorized past its deadline.
		expiry:=time.NewTimer(remaining)
		select {
		case <-state.done:
			expiry.Stop()
			return
		case <-m.ctx.Done():
			expiry.Stop()
			m.lose(key,state)
			return
		case <-expiry.C:
			m.lose(key,state)
			return
		case <-ticker.C:
			expiry.Stop()
			started:=time.Now()
			ctx,cancel:=context.WithTimeout(m.ctx,m.timeout)
			reply,err:=m.client.DeviceLease(ctx,"renew",key.uid,state.id,"")
			cancel()
			if err==nil && !reply.Allowed {m.lose(key,state);return}
			m.mu.Lock()
			if m.slots[key]!=state {m.mu.Unlock();return}
			if err==nil && reply.Allowed && reply.LeaseTTLMS>0 {
				updated:=started.Add(time.Duration(reply.LeaseTTLMS)*time.Millisecond)
				if updated.After(state.deadline) {state.deadline=updated}
			}
			expired:=!time.Now().Before(state.deadline)
			m.mu.Unlock()
			if expired {m.lose(key,state);return}
		}
	}
}
