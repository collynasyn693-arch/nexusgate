package resilience

import (
	"sync/atomic"
)

// HalfOpenController manages canary trial requests when a circuit breaker transitions
// from StateOpen to StateHalfOpen. It enforces bounded concurrent trial execution,
// counts consecutive successful trials to restore StateClosed, and immediately
// re-trips back to StateOpen upon any single trial failure.
type HalfOpenController struct {
	fsm                 *BreakerFSM
	window              *SlidingWindow
	cfg                 Config
	inflight            atomic.Int64 // Active concurrent trial requests in flight
	consecutiveSuccess  atomic.Int64 // Cumulative consecutive successful trials in this cycle
	hasFailed           atomic.Bool  // Marked true if any trial in this cycle failed
}

// NewHalfOpenController constructs an initialized HalfOpenController.
func NewHalfOpenController(fsm *BreakerFSM, window *SlidingWindow, cfg Config) *HalfOpenController {
	if cfg.HalfOpenMaxRequests <= 0 {
		cfg.HalfOpenMaxRequests = 3
	}
	if cfg.ConsecutiveSuccesses <= 0 {
		cfg.ConsecutiveSuccesses = 3
	}

	return &HalfOpenController{
		fsm:    fsm,
		window: window,
		cfg:    cfg,
	}
}

// Inflight returns the number of active canary trial requests currently executing.
func (c *HalfOpenController) Inflight() int64 {
	return c.inflight.Load()
}

// ConsecutiveSuccesses returns the number of consecutive successful canary requests.
func (c *HalfOpenController) ConsecutiveSuccesses() int64 {
	return c.consecutiveSuccess.Load()
}

// Allow attempts to reserve a canary trial slot.
// If the circuit is not in StateHalfOpen, or if active inflight requests have reached
// HalfOpenMaxRequests, it returns false.
// Uses a bounded CAS loop to eliminate the unbounded counter leak vulnerability.
func (c *HalfOpenController) Allow() bool {
	if c.fsm == nil || c.fsm.State() != StateHalfOpen {
		return false
	}

	maxInflight := int64(c.cfg.HalfOpenMaxRequests)
	for {
		cur := c.inflight.Load()
		if cur >= maxInflight {
			return false // Concurrency quota reached
		}
		if c.inflight.CompareAndSwap(cur, cur+1) {
			return true // Permit granted
		}
	}
}

// ReleaseInflight decrements the active canary concurrency counter.
// Bounded at zero with CAS to eliminate any negative underflow hazard.
func (c *HalfOpenController) ReleaseInflight() {
	for {
		cur := c.inflight.Load()
		if cur <= 0 {
			return
		}
		if c.inflight.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// RecordSuccess records a successful canary trial response.
// Decrements active inflight concurrency, increments consecutive successes,
// and if threshold is met without any concurrent failures, transitions the FSM back to StateClosed.
func (c *HalfOpenController) RecordSuccess() {
	defer c.ReleaseInflight()

	if c.fsm == nil || c.fsm.State() != StateHalfOpen {
		return
	}

	// If another concurrent canary already failed, do not count success toward recovery
	if c.hasFailed.Load() {
		return
	}

	succ := c.consecutiveSuccess.Add(1)
	if int(succ) >= c.cfg.ConsecutiveSuccesses {
		// Attempt transition HalfOpen -> Closed
		if err := c.fsm.TransitionTo(StateClosed); err == nil {
			c.Reset()
			if c.window != nil {
				c.window.Reset()
			}
		}
	}
}

// RecordFailure records a failed canary trial response.
// Invariant: ANY failure in Half-Open immediately trips the breaker back to StateOpen.
func (c *HalfOpenController) RecordFailure() {
	defer c.ReleaseInflight()

	c.hasFailed.Store(true)

	if c.fsm != nil && c.fsm.State() == StateHalfOpen {
		_ = c.fsm.TransitionTo(StateOpen)
	}
	c.Reset()
	if c.window != nil {
		c.window.Reset()
	}
}

// RecordResult records a canary trial result based on the boolean success flag.
func (c *HalfOpenController) RecordResult(success bool) {
	if success {
		c.RecordSuccess()
	} else {
		c.RecordFailure()
	}
}

// Reset clears canary trial counters, inflight concurrency, and the failure flag for the next cycle.
func (c *HalfOpenController) Reset() {
	c.inflight.Store(0)
	c.consecutiveSuccess.Store(0)
	c.hasFailed.Store(false)
}
