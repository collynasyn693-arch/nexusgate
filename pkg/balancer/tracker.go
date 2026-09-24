package balancer

import (
	"sync"
	"sync/atomic"
	"time"
)

// connGuardPool recycles ConnGuard instances to guarantee 0 heap allocations/op in steady-state.
var connGuardPool = sync.Pool{
	New: func() any {
		return &ConnGuard{}
	},
}

// ConnGuard provides RAII-style connection tracking with idempotent atomic guards.
// Designed for seamless `defer guard.Release()` or `defer guard.Done(...)` usage to
// prevent connection counter leaks under panics, timeouts, or early returns.
type ConnGuard struct {
	backend  *Backend
	released atomic.Bool
}

// AcquireGuard attempts to reserve an active connection on backend b and wraps it in a ConnGuard.
func AcquireGuard(b *Backend) (*ConnGuard, error) {
	if b == nil {
		return nil, ErrBackendNotFound
	}

	if err := b.AcquireConn(); err != nil {
		return nil, err
	}

	g := connGuardPool.Get().(*ConnGuard)
	g.backend = b
	g.released.Store(false)
	return g, nil
}

// AcquireGuard attempts to reserve an active connection on this backend and returns a ConnGuard.
func (b *Backend) AcquireGuard() (*ConnGuard, error) {
	return AcquireGuard(b)
}

// Backend returns the underlying backend target managed by this guard.
func (g *ConnGuard) Backend() *Backend {
	if g == nil {
		return nil
	}
	return g.backend
}

// Release idempotently decrements the active connection counter and returns the guard to the pool.
// Safe for multiple calls or redundant execution in defers.
func (g *ConnGuard) Release() {
	if g == nil || !g.released.CompareAndSwap(false, true) {
		return
	}

	b := g.backend
	g.backend = nil
	if b != nil {
		b.ReleaseConn()
	}
	connGuardPool.Put(g)
}

// Record updates the request latency into Peak-EWMA and increments totalErrors if err != nil,
// without releasing the connection. This allows safe latency tracking when using defer guard.Release().
func (g *ConnGuard) Record(latency time.Duration, err error) {
	if g == nil || g.released.Load() {
		return
	}
	b := g.backend
	if b != nil {
		if err != nil {
			b.totalErrors.Add(1)
		}
		b.RecordLatency(latency)
	}
}

// Done releases the connection, records the request latency into Peak-EWMA, and increments
// totalErrors if err != nil. If the guard was already released, Done is an idempotent no-op.
func (g *ConnGuard) Done(latency time.Duration, err error) {
	if g == nil || g.released.Load() {
		return
	}
	g.Record(latency, err)
	g.Release()
}
