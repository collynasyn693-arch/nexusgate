package resilience

import (
	"net/http"
	"sync"
	"sync/atomic"
)

// Breaker is the unified, production-grade CircuitBreaker implementation.
// It integrates the Three-State FSM, sliding-window ring buffer, dual trip triggers,
// bounded Half-Open canary limiter, and custom fallback handler.
type Breaker struct {
	state    atomic.Int32 // 0-offset direct atomic state for sub-5ns hot-path Allow()
	name     string
	cfg      Config
	fsm      *BreakerFSM
	window   *SlidingWindow
	tripper  *Tripper
	halfOpen *HalfOpenController
	fallback http.Handler
	mu       sync.RWMutex
}

// NewBreaker constructs and wires together a complete CircuitBreaker instance.
func NewBreaker(name string, cfg Config) *Breaker {
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = DefaultConfig().ResetTimeout
	}
	if cfg.ConsecutiveFailures <= 0 {
		cfg.ConsecutiveFailures = DefaultConfig().ConsecutiveFailures
	}
	if cfg.FailureRateThreshold <= 0.0 {
		cfg.FailureRateThreshold = DefaultConfig().FailureRateThreshold
	}
	if cfg.MinRequests <= 0 {
		cfg.MinRequests = DefaultConfig().MinRequests
	}
	if cfg.HalfOpenMaxRequests <= 0 {
		cfg.HalfOpenMaxRequests = DefaultConfig().HalfOpenMaxRequests
	}
	if cfg.ConsecutiveSuccesses <= 0 {
		cfg.ConsecutiveSuccesses = DefaultConfig().ConsecutiveSuccesses
	}

	fsm := NewBreakerFSM(name, cfg)
	window := NewSlidingWindow()
	tripper := NewTripper(fsm, window, cfg)
	halfOpen := NewHalfOpenController(fsm, window, cfg)

	b := &Breaker{
		name:     name,
		cfg:      cfg,
		fsm:      fsm,
		window:   window,
		tripper:  tripper,
		halfOpen: halfOpen,
		fallback: cfg.Fallback,
	}
	b.state.Store(int32(StateClosed))
	return b
}

// Name returns the identifier of this circuit breaker.
func (b *Breaker) Name() string {
	return b.name
}

// State returns the current State (Closed, Half-Open, Open).
func (b *Breaker) State() State {
	st := b.fsm.State()
	b.state.Store(int32(st))
	return st
}

// Trip manually transitions the circuit breaker to StateOpen.
func (b *Breaker) Trip() bool {
	res := b.fsm.Trip()
	b.state.Store(int32(b.fsm.State()))
	return res
}

// TransitionTo attempts a state transition on the underlying FSM and syncs the atomic state cache.
func (b *Breaker) TransitionTo(st State) error {
	err := b.fsm.TransitionTo(st)
	b.state.Store(int32(b.fsm.State()))
	return err
}

// Allow reports whether a new request is permitted to proceed upstream.
// On the hot path (StateClosed), this performs a single direct atomic load (<2.5ns, 0 allocs).
func (b *Breaker) Allow() bool {
	if b.state.Load() == int32(StateClosed) {
		return true
	}
	return b.allowSlow()
}

// allowSlow handles evaluation when circuit is in Open or Half-Open state.
func (b *Breaker) allowSlow() bool {
	st := b.fsm.State()
	b.state.Store(int32(st))

	if st == StateClosed {
		return true
	}

	if st == StateOpen {
		if !b.fsm.allowSlow() {
			return false
		}
		st = b.fsm.State()
		b.state.Store(int32(st))
		if st == StateClosed {
			return true
		}
	}

	res := b.halfOpen.Allow()
	b.state.Store(int32(b.fsm.State()))
	return res
}

// RecordSuccess records a successful upstream response.
func (b *Breaker) RecordSuccess() {
	st := b.fsm.State()
	switch st {
	case StateClosed:
		b.tripper.RecordSuccess()
	case StateHalfOpen:
		b.halfOpen.RecordSuccess()
	}
	b.state.Store(int32(b.fsm.State()))
}

// RecordFailure records an upstream failure or error.
func (b *Breaker) RecordFailure() {
	st := b.fsm.State()
	switch st {
	case StateClosed:
		b.tripper.RecordFailure()
	case StateHalfOpen:
		b.halfOpen.RecordFailure()
	}
	b.state.Store(int32(b.fsm.State()))
}

// RecordResult records a result based on the boolean success flag.
func (b *Breaker) RecordResult(success bool) {
	if success {
		b.RecordSuccess()
	} else {
		b.RecordFailure()
	}
}

// Counts returns a current snapshot of sliding-window and sequential counters.
func (b *Breaker) Counts() Counts {
	snap := b.window.Snapshot()
	snap.ConsecutiveFailures = b.tripper.ConsecutiveFailures()
	snap.ConsecutiveSuccess = b.halfOpen.ConsecutiveSuccesses()
	return snap
}

// Reset resets all internal FSM state, sliding window buckets, and canary counters.
func (b *Breaker) Reset() {
	b.fsm.Reset()
	b.window.Reset()
	b.tripper.ResetConsecutiveFailures()
	b.halfOpen.Reset()
	b.state.Store(int32(StateClosed))
}

// FallbackHandler returns the HTTP handler used when traffic is rejected.
func (b *Breaker) FallbackHandler() http.Handler {
	b.mu.RLock()
	fb := b.fallback
	b.mu.RUnlock()
	return fb
}

// SetFallbackHandler updates the fallback HTTP handler.
func (b *Breaker) SetFallbackHandler(h http.Handler) {
	b.mu.Lock()
	b.fallback = h
	b.mu.Unlock()
}
