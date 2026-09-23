package resilience

import (
	"sync"
	"testing"
	"time"
)

func TestHalfOpen_FullRecoveryLifecycle(t *testing.T) {
	cfg := Config{
		ResetTimeout:         10 * time.Second,
		HalfOpenMaxRequests:  2,
		ConsecutiveSuccesses: 2,
	}
	fsm := NewBreakerFSM("test-recovery", cfg)
	window := NewSlidingWindow()
	ctrl := NewHalfOpenController(fsm, window, cfg)

	// Trip to Open, then transition to Half-Open
	fsm.Trip()
	if err := fsm.TransitionTo(StateHalfOpen); err != nil {
		t.Fatalf("failed to transition to HalfOpen: %v", err)
	}

	// 1st canary trial admitted
	if !ctrl.Allow() {
		t.Fatalf("expected 1st canary to be admitted")
	}
	if ctrl.Inflight() != 1 {
		t.Fatalf("expected 1 inflight canary, got %d", ctrl.Inflight())
	}
	// 1st canary succeeds
	ctrl.RecordSuccess()
	if ctrl.Inflight() != 0 {
		t.Fatalf("expected 0 inflight canaries after completion, got %d", ctrl.Inflight())
	}
	if ctrl.ConsecutiveSuccesses() != 1 {
		t.Fatalf("expected 1 consecutive success, got %d", ctrl.ConsecutiveSuccesses())
	}
	// Still in Half-Open because 2 consecutive successes required
	if fsm.State() != StateHalfOpen {
		t.Fatalf("expected state HalfOpen after 1 success, got %v", fsm.State())
	}

	// 2nd canary trial admitted
	if !ctrl.Allow() {
		t.Fatalf("expected 2nd canary to be admitted")
	}
	// 2nd canary succeeds -> reaches threshold 2
	ctrl.RecordSuccess()

	// Circuit must now have recovered to StateClosed!
	if fsm.State() != StateClosed {
		t.Fatalf("expected circuit to recover to StateClosed, got %v", fsm.State())
	}
}

func TestHalfOpen_InstantReTripOnFailure(t *testing.T) {
	cfg := Config{
		ResetTimeout:         10 * time.Second,
		HalfOpenMaxRequests:  3,
		ConsecutiveSuccesses: 3,
	}
	fsm := NewBreakerFSM("test-retrip", cfg)
	window := NewSlidingWindow()
	ctrl := NewHalfOpenController(fsm, window, cfg)

	fsm.Trip()
	if err := fsm.TransitionTo(StateHalfOpen); err != nil {
		t.Fatalf("failed to transition to HalfOpen: %v", err)
	}

	// 1st canary succeeds
	if !ctrl.Allow() {
		t.Fatalf("expected 1st canary to be allowed")
	}
	ctrl.RecordSuccess()
	if fsm.State() != StateHalfOpen {
		t.Fatalf("expected StateHalfOpen")
	}

	// 2nd canary fails -> must immediately trip to StateOpen!
	if !ctrl.Allow() {
		t.Fatalf("expected 2nd canary to be allowed")
	}
	ctrl.RecordFailure()

	if fsm.State() != StateOpen {
		t.Fatalf("expected circuit to immediately re-trip to StateOpen on failure, got %v", fsm.State())
	}
	// Subsequent canary requests must be rejected
	if ctrl.Allow() {
		t.Fatalf("expected Allow() to return false when circuit re-tripped to Open")
	}
}

func TestHalfOpen_ConcurrencySaturationAndInflightRelease(t *testing.T) {
	cfg := Config{
		HalfOpenMaxRequests:  2,
		ConsecutiveSuccesses: 5,
	}
	fsm := NewBreakerFSM("test-concurrency-limit", cfg)
	window := NewSlidingWindow()
	ctrl := NewHalfOpenController(fsm, window, cfg)

	fsm.Trip()
	_ = fsm.TransitionTo(StateHalfOpen)

	// Admit Canary 1
	if !ctrl.Allow() {
		t.Fatalf("canary 1 rejected")
	}
	// Admit Canary 2
	if !ctrl.Allow() {
		t.Fatalf("canary 2 rejected")
	}
	// Canary 3 must be rejected (max requests = 2)
	if ctrl.Allow() {
		t.Fatalf("canary 3 should have been rejected due to saturation")
	}
	if ctrl.Inflight() != 2 {
		t.Fatalf("expected 2 inflight, got %d", ctrl.Inflight())
	}

	// Canary 1 finishes
	ctrl.RecordSuccess()
	if ctrl.Inflight() != 1 {
		t.Fatalf("expected 1 inflight after canary 1 completed, got %d", ctrl.Inflight())
	}

	// Now Canary 4 can be admitted
	if !ctrl.Allow() {
		t.Fatalf("canary 4 should be allowed after canary 1 released inflight slot")
	}
	if ctrl.Inflight() != 2 {
		t.Fatalf("expected 2 inflight, got %d", ctrl.Inflight())
	}

	// Clean up inflight canaries
	ctrl.RecordSuccess()
	ctrl.RecordSuccess()
	if ctrl.Inflight() != 0 {
		t.Fatalf("expected 0 inflight, got %d", ctrl.Inflight())
	}
}

func TestHalfOpen_ConcurrentStressRace(t *testing.T) {
	cfg := Config{
		HalfOpenMaxRequests:  5,
		ConsecutiveSuccesses: 10,
	}
	fsm := NewBreakerFSM("test-stress", cfg)
	window := NewSlidingWindow()
	ctrl := NewHalfOpenController(fsm, window, cfg)

	fsm.Trip()
	_ = fsm.TransitionTo(StateHalfOpen)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if ctrl.Allow() {
				time.Sleep(time.Duration(id%3) * time.Millisecond)
				if id%5 == 0 {
					ctrl.RecordFailure()
				} else {
					ctrl.RecordSuccess()
				}
			}
		}(i)
	}

	wg.Wait()

	// State should have transitioned to Closed or Open cleanly without deadlock
	st := fsm.State()
	if st != StateClosed && st != StateOpen && st != StateHalfOpen {
		t.Fatalf("unexpected state after concurrent trial: %v", st)
	}
}
