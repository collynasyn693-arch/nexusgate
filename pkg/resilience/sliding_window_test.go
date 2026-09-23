package resilience

import (
	"sync"
	"testing"
)

func TestSlidingWindow_BasicCounts(t *testing.T) {
	w := NewSlidingWindow()

	if w.Total() != 0 {
		t.Fatalf("expected 0 total requests, got %d", w.Total())
	}
	if w.FailureRate() != 0.0 {
		t.Fatalf("expected 0.0 failure rate on empty window, got %f", w.FailureRate())
	}

	w.RecordSuccess()
	w.RecordSuccess()
	w.RecordFailure()

	if w.Total() != 3 {
		t.Fatalf("expected 3 total requests, got %d", w.Total())
	}
	if w.Successes() != 2 {
		t.Fatalf("expected 2 successes, got %d", w.Successes())
	}
	if w.Failures() != 1 {
		t.Fatalf("expected 1 failure, got %d", w.Failures())
	}

	expectedRate := 1.0 / 3.0
	if diff := w.FailureRate() - expectedRate; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("expected failure rate ~%f, got %f", expectedRate, w.FailureRate())
	}

	snap := w.Snapshot()
	if snap.TotalRequests != 3 || snap.Successes != 2 || snap.Failures != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestSlidingWindow_RolloverAndExpiration(t *testing.T) {
	currentSec := int64(100)
	w := NewSlidingWindow()
	w.timeFn = func() int64 {
		return currentSec
	}

	// Record 10 successes and 5 failures at second 100
	for i := 0; i < 10; i++ {
		w.RecordSuccess()
	}
	for i := 0; i < 5; i++ {
		w.RecordFailure()
	}

	if w.Total() != 15 || w.Failures() != 5 {
		t.Fatalf("expected 15 total, 5 failures at second 100")
	}

	// Advance time by 30 seconds (second 130) -> within 60s window
	currentSec = 130
	// Record 5 successes at second 130
	for i := 0; i < 5; i++ {
		w.RecordSuccess()
	}

	// Total should now be 15 (from sec 100) + 5 (from sec 130) = 20
	if w.Total() != 20 || w.Failures() != 5 {
		t.Fatalf("expected 20 total, 5 failures at second 130, got total=%d fail=%d", w.Total(), w.Failures())
	}

	// Advance time to second 165 (65 seconds after second 100)
	// Second 100's bucket has expired (>60s diff). Second 130 is 35s ago (still active).
	currentSec = 165
	if w.Total() != 5 || w.Failures() != 0 {
		t.Fatalf("expected 5 total, 0 failures at second 165, got total=%d fail=%d", w.Total(), w.Failures())
	}

	// Advance time to second 200 (>60s after second 130)
	currentSec = 200
	if w.Total() != 0 || w.Failures() != 0 {
		t.Fatalf("expected 0 total after complete window expiration, got total=%d fail=%d", w.Total(), w.Failures())
	}
}

func TestSlidingWindow_CircularIndexWrapAndOverwrite(t *testing.T) {
	currentSec := int64(10)
	w := NewSlidingWindow()
	w.timeFn = func() int64 {
		return currentSec
	}

	// Bucket at index 10 % 60 = 10
	w.RecordFailure()
	w.RecordFailure()

	if w.Failures() != 2 {
		t.Fatalf("expected 2 failures at sec 10")
	}

	// Jump forward by 120 seconds to sec 130 (same index 130 % 60 = 10)
	currentSec = 130
	w.RecordSuccess()

	// Old failures from sec 10 must be cleared by lazy eviction
	if w.Failures() != 0 {
		t.Fatalf("expected 0 failures after circular wrap, got %d", w.Failures())
	}
	if w.Successes() != 1 {
		t.Fatalf("expected 1 success after circular wrap, got %d", w.Successes())
	}
}

func TestSlidingWindow_BackwardTimeJumpSafety(t *testing.T) {
	currentSec := int64(200)
	w := NewSlidingWindow()
	w.timeFn = func() int64 {
		return currentSec
	}

	w.RecordSuccess()
	w.RecordFailure()

	// Simulate backward clock jump: time steps back to 190
	currentSec = 190

	// Counts must ignore future bucket (sec 200) because nowSec - b.timestamp < 0
	total, failures := w.Counts()
	if total != 0 || failures != 0 {
		t.Fatalf("expected 0 total and 0 failures when clock steps backward, got total=%d fail=%d", total, failures)
	}
}

func TestSlidingWindow_Reset(t *testing.T) {
	w := NewSlidingWindow()
	w.RecordSuccess()
	w.RecordFailure()

	if w.Total() != 2 {
		t.Fatalf("expected total 2 before reset")
	}

	w.Reset()
	if w.Total() != 0 || w.Failures() != 0 {
		t.Fatalf("expected 0 total after reset")
	}
}

func TestSlidingWindow_ConcurrentRecordAndQuery(t *testing.T) {
	w := NewSlidingWindow()
	var wg sync.WaitGroup

	// 50 writers recording concurrent successes and failures
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if (id+j)%2 == 0 {
					w.RecordSuccess()
				} else {
					w.RecordFailure()
				}
			}
		}(i)
	}

	// 10 readers checking counts concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = w.Total()
				_ = w.FailureRate()
				_ = w.Snapshot()
			}
		}()
	}

	wg.Wait()

	if w.Total() != 5000 {
		t.Fatalf("expected 5000 total requests after concurrent writers, got %d", w.Total())
	}
}
