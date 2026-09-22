package balancer

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTracker_NormalAcquireRelease(t *testing.T) {
	b := NewBackend(mustURL("http://tracker-backend"), 10, 0)

	guard, err := b.AcquireGuard()
	if err != nil {
		t.Fatalf("failed acquiring guard: %v", err)
	}

	if b.Inflight() != 1 {
		t.Fatalf("expected 1 inflight, got %d", b.Inflight())
	}
	if b.TotalRequests() != 1 {
		t.Fatalf("expected 1 total requests, got %d", b.TotalRequests())
	}

	// First release
	guard.Release()
	if b.Inflight() != 0 {
		t.Fatalf("expected 0 inflight after release, got %d", b.Inflight())
	}

	// Second release must be a no-op (idempotent)
	guard.Release()
	if b.Inflight() != 0 {
		t.Fatalf("expected 0 inflight after redundant release, got %d", b.Inflight())
	}
}

func TestTracker_DoneWithLatencyAndError(t *testing.T) {
	b := NewBackend(mustURL("http://done-backend"), 10, 0)

	guard, err := b.AcquireGuard()
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}

	customErr := errors.New("upstream gateway timeout")
	guard.Done(15*time.Millisecond, customErr)

	if b.Inflight() != 0 {
		t.Errorf("expected 0 inflight after Done, got %d", b.Inflight())
	}
	if b.TotalErrors() != 1 {
		t.Errorf("expected 1 error recorded, got %d", b.TotalErrors())
	}
	if b.Latency() < 10*time.Millisecond {
		t.Errorf("expected latency >= 10ms, got %v", b.Latency())
	}
}

func TestTracker_PanicLeakStressTest(t *testing.T) {
	b := NewBackend(mustURL("http://panic-backend"), 10, 0)

	const numGoroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(workerID int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				func() {
					defer func() {
						_ = recover()
					}()

					guard, err := b.AcquireGuard()
					if err != nil {
						return
					}
					defer guard.Release()

					// Simulate intermittent catastrophic panic inside handler
					if (workerID+op)%3 == 0 {
						panic(fmt.Sprintf("panic at worker %d op %d", workerID, op))
					}
				}()
			}
		}(g)
	}

	wg.Wait()

	// Assert that despite thousands of panics, inflight connections strictly hit 0 with zero leaks!
	if b.Inflight() != 0 {
		t.Fatalf("CRITICAL CONCURRENCY LEAK: inflight connections = %d after panics (expected 0)", b.Inflight())
	}
}

func TestTracker_MaxConnsEnforcement(t *testing.T) {
	// Backend bounded by max_conns = 2
	b := NewBackend(mustURL("http://bounded-backend"), 10, 2)

	g1, err1 := b.AcquireGuard()
	if err1 != nil {
		t.Fatalf("g1 error: %v", err1)
	}

	g2, err2 := b.AcquireGuard()
	if err2 != nil {
		t.Fatalf("g2 error: %v", err2)
	}

	// 3rd concurrent acquire must be rejected due to saturation
	g3, err3 := b.AcquireGuard()
	if err3 != ErrBackendOverloaded || g3 != nil {
		t.Fatalf("expected ErrBackendOverloaded for g3, got: %v, %v", g3, err3)
	}

	// Release g1
	g1.Release()

	// Now acquisition should succeed
	g4, err4 := b.AcquireGuard()
	if err4 != nil {
		t.Fatalf("g4 acquire failed after g1 released: %v", err4)
	}

	g2.Release()
	g4.Release()

	if b.Inflight() != 0 {
		t.Fatalf("expected 0 inflight, got %d", b.Inflight())
	}
}

func TestTracker_ZeroAllocations(t *testing.T) {
	b := NewBackend(mustURL("http://bench-allocs"), 10, 0)

	// Warm up pool
	g, _ := b.AcquireGuard()
	g.Release()

	allocs := testing.AllocsPerRun(1000, func() {
		guard, err := b.AcquireGuard()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		guard.Release()
	})

	if allocs != 0 {
		t.Errorf("expected 0 allocs/op for AcquireGuard/Release, got %f", allocs)
	}
}
