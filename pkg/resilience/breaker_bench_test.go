package resilience

import (
	"testing"
	"time"
)

// BenchmarkAllow_Closed_Sequential measures the single-goroutine execution latency
// of Allow() on the critical hot path in StateClosed.
// Target constraint: <10ns/op and strictly 0 B/op, 0 allocs/op.
func BenchmarkAllow_Closed_Sequential(b *testing.B) {
	cb := NewBreaker("bench-seq", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !cb.Allow() {
			b.Fatal("expected Allow() to return true in Closed state")
		}
	}
}

// BenchmarkAllow_Closed_Parallel measures the concurrent execution latency
// of Allow() across multiple goroutines in StateClosed.
// Target constraint: strictly 0 B/op, 0 allocs/op without mutex contention.
func BenchmarkAllow_Closed_Parallel(b *testing.B) {
	cb := NewBreaker("bench-par", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !cb.Allow() {
				b.Fatal("expected Allow() to return true")
			}
		}
	})
}

// BenchmarkFSM_Allow_Closed_Sequential measures raw FSM Allow() execution.
func BenchmarkFSM_Allow_Closed_Sequential(b *testing.B) {
	fsm := NewBreakerFSM("bench-fsm-seq", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !fsm.Allow() {
			b.Fatal("expected Allow() to return true")
		}
	}
}

// BenchmarkFSM_Allow_Closed_Parallel measures concurrent raw FSM Allow() execution.
func BenchmarkFSM_Allow_Closed_Parallel(b *testing.B) {
	fsm := NewBreakerFSM("bench-fsm-par", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !fsm.Allow() {
				b.Fatal("expected Allow() to return true")
			}
		}
	})
}

// BenchmarkAllow_Open measures rejection latency when the circuit breaker is in StateOpen.
func BenchmarkAllow_Open(b *testing.B) {
	cfg := DefaultConfig()
	cfg.ResetTimeout = 1 * time.Hour // Ensure cooldown does not expire during benchmark
	cb := NewBreaker("bench-open", cfg)
	cb.Trip()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if cb.Allow() {
			b.Fatal("expected Allow() to return false in Open state")
		}
	}
}

// BenchmarkRecordSuccess_Sequential measures sliding window and tripper update latency.
func BenchmarkRecordSuccess_Sequential(b *testing.B) {
	cb := NewBreaker("bench-record-seq", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cb.RecordSuccess()
	}
}

// BenchmarkRecordSuccess_Parallel measures sliding window update throughput across goroutines.
func BenchmarkRecordSuccess_Parallel(b *testing.B) {
	cb := NewBreaker("bench-record-par", DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.RecordSuccess()
		}
	})
}

// BenchmarkSlidingWindow_Counts measures aggregation over the 60 1-second buckets.
func BenchmarkSlidingWindow_Counts(b *testing.B) {
	w := NewSlidingWindow()
	for i := 0; i < 60; i++ {
		w.RecordSuccess()
		w.RecordFailure()
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = w.Counts()
	}
}
