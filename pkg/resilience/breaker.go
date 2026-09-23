package resilience

import (
	"net/http"
	"sync"
)

// Breaker is the unified, production-grade CircuitBreaker implementation.
// It integrates the Three-State FSM, sliding-window ring buffer, dual trip triggers,
// bounded Half-Open canary limiter, and custom fallback handler.
type Breaker struct {
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

	return &Breaker{
		name:     name,
		cfg:      cfg,
		fsm:      fsm,
		window:   window,
		tripper:  tripper,
		halfOpen: halfOpen,
		fallback: cfg.Fallback,
	}
}

// Name returns the identifier of this circuit breaker.
func (b *Breaker) Name() string {
	return b.name
}

// State returns the current State (Closed, Half-Open, Open).
func (b *Breaker) State() State {
	return b.fsm.State()
}

// Allow reports whether a new request is permitted to proceed upstream.
// On the hot path (StateClosed), this performs a single atomic load (<3ns, 0 allocs).
func (b *Breaker) Allow() bool {
	st := b.fsm.State()
	if st == StateClosed {
		return true
	}
	if st == StateOpen {
		// allowSlow will evaluate whether reset timeout elapsed and transition to HalfOpen
		if !b.fsm.allowSlow() {
			return false
		}
		// Transition succeeded -> fall through to HalfOpen controller
	}
	return b.halfOpen.Allow()
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
