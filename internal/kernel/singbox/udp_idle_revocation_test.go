package singbox

import (
    "context"
    "errors"
    "net"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/sagernet/sing/common/buf"
    M "github.com/sagernet/sing/common/metadata"
    N "github.com/sagernet/sing/common/network"
)

// deadlineBlockedPacketConn models an idle QUIC UDP reader that must not
// have its underlying Close called concurrently with a blocking ReadPacket.
type deadlineBlockedPacketConn struct {
    N.PacketConn
    started chan struct{}
    wake chan struct{}
    startOnce sync.Once
    wakeOnce sync.Once
    reading atomic.Bool
    closeCount atomic.Int32
    concurrentClose atomic.Bool
    deadlines atomic.Int32
}

func (p *deadlineBlockedPacketConn) ReadPacket(_ *buf.Buffer) (M.Socksaddr, error) {
    p.reading.Store(true)
    p.startOnce.Do(func() { close(p.started) })
    <-p.wake
    p.reading.Store(false)
    return M.Socksaddr{}, net.ErrClosed
}

func (p *deadlineBlockedPacketConn) SetReadDeadline(_ time.Time) error {
    p.deadlines.Add(1)
    p.wakeOnce.Do(func() { close(p.wake) })
    return nil
}

func (p *deadlineBlockedPacketConn) Close() error {
    if p.reading.Load() {
        p.concurrentClose.Store(true)
    }
    p.closeCount.Add(1)
    p.wakeOnce.Do(func() { close(p.wake) })
    return nil
}

func TestStrictUDPRevocationWakesIdleReadAndClosesOnce(t *testing.T) {
    for i := 0; i < 20; i++ {
        raw := &deadlineBlockedPacketConn{started: make(chan struct{}), wake: make(chan struct{})}
        tracker := NewConnTracker(0)
        var released atomic.Int32
        c := &trackedPacketConn{
            PacketConn: raw, tracker: tracker, strictRevocation: true,
            ctx: context.Background(),
            releaseAdmission: func() { released.Add(1) },
        }

        finished := make(chan error, 1)
        go func() {
            _, err := c.ReadPacket(buf.New())
            finished <- err
        }()
        select {
        case <-raw.started:
        case <-time.After(time.Second):
            t.Fatal("idle UDP read did not start")
        }

        if err := c.Close(); err != nil {
            t.Fatalf("revoke idle packet read: %v", err)
        }
        select {
        case err := <-finished:
            if !errors.Is(err, net.ErrClosed) {
                t.Fatalf("read after revocation = %v; want net.ErrClosed", err)
            }
        case <-time.After(time.Second):
            t.Fatal("idle UDP read remained blocked after lease revocation")
        }
        if err := c.Close(); err != nil {
            t.Fatalf("duplicate close: %v", err)
        }
        if n := raw.deadlines.Load(); n != 1 {
            t.Fatalf("read-deadline interruptions = %d, want 1", n)
        }
        if n := raw.closeCount.Load(); n != 1 {
            t.Fatalf("underlying closes = %d, want 1", n)
        }
        if raw.concurrentClose.Load() {
            t.Fatal("underlying QUIC packet connection closed during active read")
        }
        if n := released.Load(); n != 1 {
            t.Fatalf("lease releases = %d, want 1", n)
        }
    }
}
