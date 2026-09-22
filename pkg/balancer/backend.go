package balancer

import (
	"math"
	"net/url"
	"sync"
	"time"
)

const (
	// DefaultTauNanos is the exponential decay time constant (tau = 10s) in nanoseconds.
	DefaultTauNanos = 10 * float64(time.Second)
	// MinRTTNanos is the baseline floor latency (1ms) in nanoseconds to prevent zero-cost stampedes.
	MinRTTNanos = 1_000_000
)

// NewBackend constructs and initializes a new Backend target with atomic state counters.
func NewBackend(u *url.URL, weight int64, maxConns int64) *Backend {
	if weight <= 0 {
		weight = 1
	}
	raw := ""
	if u != nil {
		raw = u.String()
	}
	b := &Backend{
		URL:              u,
		RawURL:           raw,
		ConfiguredWeight: weight,
		MaxConns:         maxConns,
		drainDone:        make(chan struct{}),
	}
	b.effectiveWeight.Store(weight)
	b.healthy.Store(true)
	b.latencyEWMA.Store(MinRTTNanos)
	b.lastUpdateNanos.Store(time.Now().UnixNano())
	return b
}

// Inflight returns the current number of in-flight active connections.
func (b *Backend) Inflight() int64 {
	return b.activeConns.Load()
}

// TotalRequests returns the lifetime count of requests processed by this backend.
func (b *Backend) TotalRequests() int64 {
	return b.totalRequests.Load()
}

// TotalErrors returns the lifetime count of errors encountered on this backend.
func (b *Backend) TotalErrors() int64 {
	return b.totalErrors.Load()
}

// EffectiveWeight returns the current effective weight used in weighted load balancing.
func (b *Backend) EffectiveWeight() int64 {
	w := b.effectiveWeight.Load()
	if w <= 0 {
		return 1
	}
	return w
}

// SetEffectiveWeight adjusts the effective weight.
func (b *Backend) SetEffectiveWeight(w int64) {
	if w <= 0 {
		w = 1
	}
	b.effectiveWeight.Store(w)
}

// IsHealthy reports whether the backend is considered healthy for routing.
func (b *Backend) IsHealthy() bool {
	return b.healthy.Load()
}

// SetHealthy updates the backend health status.
func (b *Backend) SetHealthy(h bool) {
	b.healthy.Store(h)
}

// IsDraining reports whether the backend is currently draining connections.
func (b *Backend) IsDraining() bool {
	return b.draining.Load()
}

// SetDraining marks the backend as draining or not draining.
func (b *Backend) SetDraining(d bool) {
	b.draining.Store(d)
}

// Latency returns the current Peak-EWMA latency estimate as a time.Duration.
func (b *Backend) Latency() time.Duration {
	return time.Duration(b.latencyEWMA.Load())
}

// EffectiveLatency returns the time-decayed EWMA estimate in nanoseconds as of nowNanos.
// This prevents cold-spike starvation: if a node spiked and received zero subsequent traffic,
// its score will gradually decay back toward MinRTTNanos as time elapses.
func (b *Backend) EffectiveLatency(nowNanos int64) int64 {
	ewma := b.latencyEWMA.Load()
	last := b.lastUpdateNanos.Load()
	dt := nowNanos - last
	if dt <= 0 {
		return ewma
	}
	factor := math.Exp(-float64(dt) / DefaultTauNanos)
	decayed := float64(MinRTTNanos) + (float64(ewma)-float64(MinRTTNanos))*factor
	if decayed < float64(MinRTTNanos) {
		decayed = float64(MinRTTNanos)
	}
	return int64(decayed)
}

// AcquireConn attempts to reserve an active connection on this backend.
// Enforces maxConns saturation limits and rejects draining backends with atomic CAS.
func (b *Backend) AcquireConn() error {
	if b.draining.Load() {
		return ErrBackendDraining
	}
	for {
		cur := b.activeConns.Load()
		if b.MaxConns > 0 && cur >= b.MaxConns {
			return ErrBackendOverloaded
		}
		if b.activeConns.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	// Post-increment verification to prevent drain-slip race condition
	if b.draining.Load() {
		rem := b.activeConns.Add(-1)
		if rem <= 0 {
			b.signalDrainDone()
		}
		return ErrBackendDraining
	}
	b.totalRequests.Add(1)
	return nil
}

// ReleaseConn decrements the active connection counter and signals completion if draining.
func (b *Backend) ReleaseConn() {
	rem := b.activeConns.Add(-1)
	if rem <= 0 && b.draining.Load() {
		b.signalDrainDone()
	}
}

// RecordLatency records a latency observation and updates the Peak-EWMA estimate.
// Spikes are absorbed immediately to steer traffic away, while recovery decays smoothly.
func (b *Backend) RecordLatency(d time.Duration) {
	measuredNanos := d.Nanoseconds()
	if measuredNanos <= 0 {
		measuredNanos = 1_000 // 1µs minimum
	}
	now := time.Now().UnixNano()
	last := b.lastUpdateNanos.Swap(now)
	dt := now - last
	if dt <= 0 {
		dt = 1
	}

	oldEWMA := b.latencyEWMA.Load()
	var newEWMA int64
	if measuredNanos >= oldEWMA {
		// Instant reaction to latency spikes
		newEWMA = measuredNanos
	} else {
		// Exponential moving average decay
		alpha := 1.0 - math.Exp(-float64(dt)/DefaultTauNanos)
		newEWMA = int64((1.0-alpha)*float64(oldEWMA) + alpha*float64(measuredNanos))
	}
	if newEWMA < MinRTTNanos {
		newEWMA = MinRTTNanos
	}
	b.latencyEWMA.Store(newEWMA)
}

// RecordDone decrements the active connection count, updates error counters if err != nil,
// and records the round-trip latency.
func (b *Backend) RecordDone(d time.Duration, err error) {
	b.ReleaseConn()
	if err != nil {
		b.totalErrors.Add(1)
	}
	b.RecordLatency(d)
}

// signalDrainDone idempotently signals that all in-flight connections have reached zero.
func (b *Backend) signalDrainDone() {
	b.drainOnce.Do(func() {
		close(b.drainDone)
	})
}

// ResetDrain restores the backend to an un-drained state with a fresh drainDone channel.
func (b *Backend) ResetDrain() {
	b.drainMu.Lock()
	defer b.drainMu.Unlock()
	b.draining.Store(false)
	b.drainOnce = sync.Once{}
	b.drainDone = make(chan struct{})
}
