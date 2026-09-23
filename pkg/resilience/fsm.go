package resilience

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// monotonicEpoch anchors the gateway process start time for strictly monotonic duration calculations.
// This guarantees immunity against mobile Android wall-clock shifts (NTP, cellular NITZ adjustments).
var monotonicEpoch = time.Now()

// monotonicNano returns nanoseconds elapsed since monotonicEpoch.
func monotonicNano() int64 {
	return time.Since(monotonicEpoch).Nanoseconds()
}

// BreakerFSM implements the Three-State Finite State Machine (Closed, Half-Open, Open)
// with atomic state transitions, 64-bit monotonic timestamping, and generation tracking.
type BreakerFSM struct {
	name          string
	cfg           Config
	state         atomic.Int32  // StateClosed(0), StateHalfOpen(1), StateOpen(2)
	generation    atomic.Uint64 // Monotonically incremented on each state change
	stateChangeAt atomic.Int64  // Monotonic nanoseconds of last state transition
	openUntil     atomic.Int64  // Monotonic nanoseconds until Open state may transition to Half-Open

	// Canary trial request accounting in Half-Open state
	halfOpenInflight atomic.Int64 // Current active concurrent canary requests
	halfOpenSucc     atomic.Int64 // Consecutive successful canaries in current Half-Open cycle

	// Closed state consecutive failure counter
	consecutiveF atomic.Int64

	// Synchronization for coordinated multi-field state transitions
	mu sync.Mutex

	// Fallback handler
	fallback http.Handler
}

// NewBreakerFSM constructs an initialized BreakerFSM in StateClosed.
func NewBreakerFSM(name string, cfg Config) *BreakerFSM {
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = 15 * time.Second
	}
	if cfg.HalfOpenMaxRequests <= 0 {
		cfg.HalfOpenMaxRequests = 3
	}
	if cfg.ConsecutiveSuccesses <= 0 {
		cfg.ConsecutiveSuccesses = 3
	}
	if cfg.ConsecutiveFailures <= 0 {
		cfg.ConsecutiveFailures = 5
	}
	if cfg.FailureRateThreshold <= 0.0 {
		cfg.FailureRateThreshold = 0.5
	}
	if cfg.MinRequests <= 0 {
		cfg.MinRequests = 10
	}

	now := monotonicNano()
	fsm := &BreakerFSM{
		name:     name,
		cfg:      cfg,
		fallback: cfg.Fallback,
	}
	fsm.state.Store(int32(StateClosed))
	fsm.stateChangeAt.Store(now)
	fsm.generation.Store(1)
	return fsm
}

// Name returns the identifier of this circuit breaker FSM.
func (fsm *BreakerFSM) Name() string {
	return fsm.name
}

// State returns the current State (Closed, Half-Open, Open).
func (fsm *BreakerFSM) State() State {
	return State(fsm.state.Load())
}

// Generation returns the current state generation counter.
func (fsm *BreakerFSM) Generation() uint64 {
	return fsm.generation.Load()
}

// StateChangeAt returns the monotonic timestamp when the current state was entered.
func (fsm *BreakerFSM) StateChangeAt() int64 {
	return fsm.stateChangeAt.Load()
}

// OpenUntil returns the monotonic timestamp until which the breaker remains Open.
func (fsm *BreakerFSM) OpenUntil() int64 {
	return fsm.openUntil.Load()
}

// Trip transitions the circuit breaker from Closed or Half-Open to Open.
// Pre-condition invariant: openUntil is stored BEFORE the state CAS is published,
// completely eliminating the "Open-Bypass" race vector where concurrent Allow() calls
// could see an uninitialized or expired openUntil timestamp.
func (fsm *BreakerFSM) Trip() bool {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	cur := State(fsm.state.Load())
	if cur == StateOpen {
		return false // Already Open (idempotent)
	}

	now := monotonicNano()
	fsm.openUntil.Store(now + fsm.cfg.ResetTimeout.Nanoseconds())
	fsm.halfOpenInflight.Store(0)
	fsm.halfOpenSucc.Store(0)
	fsm.stateChangeAt.Store(now)
	fsm.generation.Add(1)
	fsm.state.Store(int32(StateOpen))
	return true
}

// TransitionTo attempts a controlled transition to newState, strictly validating
// legal state transitions:
//
//	Closed   -> Open
//	Open     -> HalfOpen
//	HalfOpen -> Closed
//	HalfOpen -> Open
//
// Illegal transitions (e.g. Closed -> HalfOpen directly, or Open -> Closed directly)
// are strictly rejected with ErrInvalidStateTransition.
func (fsm *BreakerFSM) TransitionTo(target State) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	current := State(fsm.state.Load())
	if current == target {
		return nil // Idempotent no-op
	}

	switch current {
	case StateClosed:
		if target != StateOpen {
			return ErrInvalidStateTransition
		}
	case StateOpen:
		if target != StateHalfOpen {
			return ErrInvalidStateTransition
		}
	case StateHalfOpen:
		if target != StateClosed && target != StateOpen {
			return ErrInvalidStateTransition
		}
	default:
		return ErrInvalidStateTransition
	}

	now := monotonicNano()
	if target == StateOpen {
		fsm.openUntil.Store(now + fsm.cfg.ResetTimeout.Nanoseconds())
	}
	fsm.halfOpenInflight.Store(0)
	fsm.halfOpenSucc.Store(0)
	fsm.consecutiveF.Store(0)
	fsm.stateChangeAt.Store(now)
	fsm.generation.Add(1)
	fsm.state.Store(int32(target))
	return nil
}

// Reset restores the FSM to StateClosed and clears all failure and canary counters.
func (fsm *BreakerFSM) Reset() {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	now := monotonicNano()
	fsm.state.Store(int32(StateClosed))
	fsm.stateChangeAt.Store(now)
	fsm.openUntil.Store(0)
	fsm.consecutiveF.Store(0)
	fsm.halfOpenInflight.Store(0)
	fsm.halfOpenSucc.Store(0)
	fsm.generation.Add(1)
}

// Allow reports whether a new request is permitted to proceed upstream.
// On the hot path (StateClosed), this executes a single atomic load (<3ns, 0 allocs).
func (fsm *BreakerFSM) Allow() bool {
	if fsm.state.Load() == int32(StateClosed) {
		return true
	}
	return fsm.allowSlow()
}

// allowSlow executes off the hot path when the circuit is in Open or Half-Open state.
func (fsm *BreakerFSM) allowSlow() bool {
	now := monotonicNano()
	st := State(fsm.state.Load())

	// 1. Open state: check cooldown expiry
	if st == StateOpen {
		if now < fsm.openUntil.Load() {
			return false // Still in cooldown period
		}

		// Cooldown elapsed: attempt transition Open -> HalfOpen
		fsm.mu.Lock()
		if State(fsm.state.Load()) == StateOpen && now >= fsm.openUntil.Load() {
			fsm.halfOpenInflight.Store(0)
			fsm.halfOpenSucc.Store(0)
			fsm.stateChangeAt.Store(now)
			fsm.generation.Add(1)
			fsm.state.Store(int32(StateHalfOpen))
		}
		fsm.mu.Unlock()

		st = State(fsm.state.Load())
		if st == StateOpen {
			return false
		}
	}

	// 2. Half-Open state: strictly bounded canary concurrency
	if st == StateHalfOpen {
		maxInflight := int64(fsm.cfg.HalfOpenMaxRequests)
		for {
			cur := fsm.halfOpenInflight.Load()
			if cur >= maxInflight {
				return false // Canary trial quota reached
			}
			if fsm.halfOpenInflight.CompareAndSwap(cur, cur+1) {
				return true // Canary permit granted
			}
		}
	}

	return st == StateClosed
}
