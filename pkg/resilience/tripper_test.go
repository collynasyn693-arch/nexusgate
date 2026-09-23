package resilience

import (
	"sync"
	"testing"
	"time"
)

func TestTripper_ConsecutiveFailures(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures:  5,
		FailureRateThreshold: 0.9, // High so percentage trip doesn't trigger first
		MinRequests:          100,
		ResetTimeout:         10 * time.Second,
	}
	fsm := NewBreakerFSM("test-consecutive", cfg)
	window := NewSlidingWindow()
	tripper := NewTripper(fsm, window, cfg)

	// Record 4 failures (less than threshold 5)
	for i := 0; i < 4; i++ {
		tripped := tripper.RecordFailure()
		if tripped {
			t.Fatalf("circuit tripped prematurely at failure %d", i+1)
		}
	}
	if fsm.State() != StateClosed {
		t.Fatalf("expected circuit to remain Closed after 4 failures, got %v", fsm.State())
	}
	if tripper.ConsecutiveFailures() != 4 {
		t.Fatalf("expected 4 consecutive failures, got %d", tripper.ConsecutiveFailures())
	}

	// Record 1 success -> should reset consecutive failures to 0
	tripper.RecordSuccess()
	if tripper.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures after success, got %d", tripper.ConsecutiveFailures())
	}

	// Now record 4 failures again
	for i := 0; i < 4; i++ {
		if tripper.RecordFailure() {
			t.Fatalf("unexpected trip on failure %d", i+1)
		}
	}
	// 5th failure must trip the circuit to Open
	tripped := tripper.RecordFailure()
	if !tripped {
		t.Fatalf("expected circuit to trip on 5th failure")
	}
	if fsm.State() != StateOpen {
		t.Fatalf("expected FSM to be in StateOpen, got %v", fsm.State())
	}
}

func TestTripper_PercentageFailureRate(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures:  100, // Very high to isolate percentage trigger
		FailureRateThreshold: 0.5, // 50%
		MinRequests:          10,
		ResetTimeout:         10 * time.Second,
	}
	fsm := NewBreakerFSM("test-percentage", cfg)
	window := NewSlidingWindow()
	tripper := NewTripper(fsm, window, cfg)

	// Record 4 failures and 0 successes: total=4, fail=4.
	// Rate is 100%, but total (4) < MinRequests (10), so it must NOT trip.
	for i := 0; i < 4; i++ {
		if tripper.RecordFailure() {
			t.Fatalf("circuit tripped before MinRequests threshold was satisfied")
		}
	}
	if fsm.State() != StateClosed {
		t.Fatalf("circuit tripped before reaching MinRequests")
	}

	// Record 6 successes: total=10, fail=4 (rate = 40% < 50%).
	for i := 0; i < 6; i++ {
		tripper.RecordSuccess()
	}
	if fsm.State() != StateClosed {
		t.Fatalf("circuit tripped with failure rate 40%% <= 50%%")
	}

	// Record 2 failures: total=12, fail=6 (rate = 6/12 = 50% >= 50%).
	// The 6th failure reaches 50% threshold and must trip the circuit.
	tripper.RecordFailure() // fail=5 (5/11 = 45.4%)
	tripped := tripper.RecordFailure() // fail=6 (6/12 = 50.0%)
	if !tripped {
		t.Fatalf("expected circuit to trip when reaching 50%% error rate at 12 requests")
	}
	if fsm.State() != StateOpen {
		t.Fatalf("expected FSM to be in StateOpen, got %v", fsm.State())
	}
}

func TestTripper_BurstFailures(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures:  5,
		FailureRateThreshold: 0.5,
		MinRequests:          10,
		ResetTimeout:         10 * time.Second,
	}
	fsm := NewBreakerFSM("test-burst", cfg)
	window := NewSlidingWindow()
	tripper := NewTripper(fsm, window, cfg)

	// Simulate burst of 500 failures
	tripCount := 0
	for i := 0; i < 500; i++ {
		if tripper.RecordFailure() {
			tripCount++
		}
	}

	// Should trip exactly once on the threshold transition
	if tripCount != 1 {
		t.Fatalf("expected exactly 1 trip event during burst, got %d", tripCount)
	}
	if fsm.State() != StateOpen {
		t.Fatalf("expected StateOpen after burst, got %v", fsm.State())
	}
}

func TestTripper_ConcurrentFailuresRace(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures:  20,
		FailureRateThreshold: 0.6,
		MinRequests:          30,
		ResetTimeout:         10 * time.Second,
	}
	fsm := NewBreakerFSM("test-concurrency", cfg)
	window := NewSlidingWindow()
	tripper := NewTripper(fsm, window, cfg)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if id%2 == 0 {
					tripper.RecordFailure()
				} else {
					tripper.RecordSuccess()
				}
			}
		}(i)
	}

	wg.Wait()

	// Circuit should have transitioned cleanly without panic or deadlock
	st := fsm.State()
	if st != StateClosed && st != StateOpen {
		t.Fatalf("unexpected state after concurrent load: %v", st)
	}
}
