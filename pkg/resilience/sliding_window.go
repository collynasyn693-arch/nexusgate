package resilience

import (
	"sync"
	"time"
)

// DefaultWindowBuckets is the fixed number of 1-second buckets in the sliding window.
const DefaultWindowBuckets = 60

// bucket represents a 1-second metric slot storing success and failure counters.
type bucket struct {
	timestamp int64 // Monotonic second epoch
	successes int64 // Successful requests in this second
	failures  int64 // Failed requests in this second
}

// SlidingWindow maintains a circular ring buffer of 60 1-second buckets for tracking
// rolling error rates. Uses monotonic epoch seconds to eliminate Android NTP / cellular
// clock shift hazards, and sync.Mutex for zero-allocation, battery-friendly synchronization.
type SlidingWindow struct {
	mu      sync.Mutex
	epoch   time.Time
	buckets [DefaultWindowBuckets]bucket // 1,440 bytes inline contiguous memory
}

// NewSlidingWindow constructs and initializes a new 60-second sliding window ring buffer.
func NewSlidingWindow() *SlidingWindow {
	return &SlidingWindow{
		epoch: monotonicEpoch,
	}
}

// monotonicSecond returns the current monotonic second since the window epoch.
func (w *SlidingWindow) monotonicSecond() int64 {
	return int64(time.Since(w.epoch) / time.Second)
}

// RecordSuccess records a successful request in the current 1-second bucket.
func (w *SlidingWindow) RecordSuccess() {
	w.RecordResult(true)
}

// RecordFailure records a failed request in the current 1-second bucket.
func (w *SlidingWindow) RecordFailure() {
	w.RecordResult(false)
}

// RecordResult updates the current bucket based on the success boolean flag.
// If the bucket at the circular index is from a prior epoch (>60s ago), it is lazily cleared.
func (w *SlidingWindow) RecordResult(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	nowSec := w.monotonicSecond()
	idx := uint64(nowSec) % DefaultWindowBuckets
	b := &w.buckets[idx]

	if b.timestamp != nowSec {
		// Lazy eviction: stale bucket from >= 60 seconds ago
		b.timestamp = nowSec
		b.successes = 0
		b.failures = 0
	}

	if success {
		b.successes++
	} else {
		b.failures++
	}
}

// Counts computes and returns the sum of total and failed requests within the active 60-second window.
// Buckets older than 60 seconds or with future timestamps are strictly ignored.
func (w *SlidingWindow) Counts() (total int64, failures int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	nowSec := w.monotonicSecond()
	for i := 0; i < DefaultWindowBuckets; i++ {
		b := &w.buckets[i]
		diff := nowSec - b.timestamp
		if diff >= 0 && diff < DefaultWindowBuckets {
			total += (b.successes + b.failures)
			failures += b.failures
		}
	}
	return total, failures
}

// Successes returns the total count of successful requests within the active 60-second window.
func (w *SlidingWindow) Successes() int64 {
	total, failures := w.Counts()
	return total - failures
}

// Failures returns the total count of failed requests within the active 60-second window.
func (w *SlidingWindow) Failures() int64 {
	_, failures := w.Counts()
	return failures
}

// Total returns the total request volume within the active 60-second window.
func (w *SlidingWindow) Total() int64 {
	total, _ := w.Counts()
	return total
}

// FailureRate calculates the rolling error rate (0.0 to 1.0) within the active 60-second window.
// Returns 0.0 if total requests is 0, completely avoiding IEEE 754 division-by-zero NaN values.
func (w *SlidingWindow) FailureRate() float64 {
	total, failures := w.Counts()
	if total == 0 {
		return 0.0
	}
	return float64(failures) / float64(total)
}

// Snapshot returns a structured Counts snapshot of the current sliding-window metrics.
func (w *SlidingWindow) Snapshot() Counts {
	total, failures := w.Counts()
	rate := 0.0
	if total > 0 {
		rate = float64(failures) / float64(total)
	}

	return Counts{
		TotalRequests: total,
		Successes:     total - failures,
		Failures:      failures,
		FailureRate:   rate,
	}
}

// Reset clears all buckets in the sliding window ring buffer.
func (w *SlidingWindow) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := 0; i < DefaultWindowBuckets; i++ {
		w.buckets[i] = bucket{}
	}
}
