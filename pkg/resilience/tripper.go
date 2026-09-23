package resilience

import (
	"sync/atomic"
)

// Tripper evaluates failure thresholds and orchestrates transitioning the FSM to StateOpen.
// Supports dual tripping policies:
// 1. Consecutive Failures: Trips when N successive requests fail.
// 2. Percentage Failure Rate: Trips when failure percentage exceeds threshold within
//    the active sliding window, gated by a minimum request volume (MinRequests).
type Tripper struct {
	fsm                 *BreakerFSM
	window              *SlidingWindow
	cfg                 Config
	consecutiveFailures atomic.Int64
}

// NewTripper constructs an initialized Tripper bound to the specified FSM and sliding window.
func NewTripper(fsm *BreakerFSM, window *SlidingWindow, cfg Config) *Tripper {
	if cfg.ConsecutiveFailures <= 0 {
		cfg.ConsecutiveFailures = 5
	}
	if cfg.FailureRateThreshold <= 0.0 {
		cfg.FailureRateThreshold = 0.5
	}
	if cfg.MinRequests <= 0 {
		cfg.MinRequests = 10
	}

	return &Tripper{
		fsm:    fsm,
		window: window,
		cfg:    cfg,
	}
}

// ConsecutiveFailures returns the current streak of sequential failures in Closed state.
func (t *Tripper) ConsecutiveFailures() int64 {
	return t.consecutiveFailures.Load()
}

// ResetConsecutiveFailures clears the consecutive failure streak.
func (t *Tripper) ResetConsecutiveFailures() {
	t.consecutiveFailures.Store(0)
}

// RecordSuccess records a success event in the sliding window and clears consecutive failures.
func (t *Tripper) RecordSuccess() {
	t.consecutiveFailures.Store(0)
	if t.window != nil {
		t.window.RecordSuccess()
	}
}

// RecordFailure records a failure event, increments consecutive failures, and evaluates
// whether the circuit breaker should trip from Closed to Open.
// Returns true if the circuit was tripped as a result of this failure.
func (t *Tripper) RecordFailure() bool {
	cf := t.consecutiveFailures.Add(1)
	if t.window != nil {
		t.window.RecordFailure()
	}

	if t.fsm == nil || t.fsm.State() != StateClosed {
		return false
	}

	// 1. Check Consecutive Failures Trigger
	if int(cf) >= t.cfg.ConsecutiveFailures {
		t.tripCircuit()
		return true
	}

	// 2. Check Percentage Failure Rate Trigger (gated by MinRequests)
	if t.window != nil {
		total, failures := t.window.Counts()
		if total >= int64(t.cfg.MinRequests) && total > 0 {
			// Integer basis-points comparison avoids floating-point precision flaws on ARM64:
			// (failures / total) >= threshold  <=>  failures * 10000 >= thresholdBps * total
			thresholdBps := int64(t.cfg.FailureRateThreshold * 10000)
			if (failures * 10000) >= (thresholdBps * total) {
				t.tripCircuit()
				return true
			}
		}
	}

	return false
}

// RecordResult records a result based on the boolean success flag.
func (t *Tripper) RecordResult(success bool) bool {
	if success {
		t.RecordSuccess()
		return false
	}
	return t.RecordFailure()
}

// ShouldTrip evaluates whether the current sliding window counts or consecutive failures
// satisfy trip criteria, without mutating any state.
func (t *Tripper) ShouldTrip() bool {
	if int(t.consecutiveFailures.Load()) >= t.cfg.ConsecutiveFailures {
		return true
	}

	if t.window != nil {
		total, failures := t.window.Counts()
		if total >= int64(t.cfg.MinRequests) && total > 0 {
			thresholdBps := int64(t.cfg.FailureRateThreshold * 10000)
			if (failures * 10000) >= (thresholdBps * total) {
				return true
			}
		}
	}
	return false
}

// tripCircuit transitions the FSM to StateOpen and resets the consecutive failures.
func (t *Tripper) tripCircuit() {
	if t.fsm != nil {
		t.fsm.Trip()
	}
	t.consecutiveFailures.Store(0)
}
