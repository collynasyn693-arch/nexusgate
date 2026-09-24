package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestIdleDetector_BasicTransitions(t *testing.T) {
	// Set small idle timeout for rapid test
	timeout := 50 * time.Millisecond
	d := NewIdleDetector(timeout)

	if d.IsSleeping() {
		t.Fatalf("expected initial state to be active")
	}
	if d.ShouldSleep() {
		t.Fatalf("expected ShouldSleep to be false immediately after creation")
	}

	// Wait for idle timeout
	time.Sleep(60 * time.Millisecond)

	if !d.ShouldSleep() {
		t.Fatalf("expected ShouldSleep to be true after timeout")
	}

	// Enter sleep
	if ok := d.EnterSleep(nil); !ok {
		t.Fatalf("expected EnterSleep to succeed")
	}
	if !d.IsSleeping() {
		t.Fatalf("expected IsSleeping to be true")
	}

	// Trigger activity
	d.OnActivity()

	if d.IsSleeping() {
		t.Fatalf("expected state to return to active after OnActivity")
	}

	state, sleeps, wakes, _ := d.Stats()
	if state != "active" || sleeps != 1 || wakes != 1 {
		t.Fatalf("unexpected stats: state=%s, sleeps=%d, wakes=%d", state, sleeps, wakes)
	}
}

func TestIdleDetector_DoubleCheckAbort(t *testing.T) {
	timeout := 20 * time.Millisecond
	d := NewIdleDetector(timeout)
	time.Sleep(30 * time.Millisecond)

	// doubleCheck returns false (simulating ring buffer not empty)
	ok := d.EnterSleep(func() bool {
		return false
	})

	if ok {
		t.Fatalf("expected EnterSleep to be aborted by doubleCheck")
	}
	if d.IsSleeping() {
		t.Fatalf("expected to remain active when doubleCheck aborts")
	}
}

func TestIdleDetector_InstantWakeupLatency(t *testing.T) {
	timeout := 10 * time.Millisecond
	d := NewIdleDetector(timeout)
	time.Sleep(15 * time.Millisecond)

	var (
		wakeLatency time.Duration
		woken       = make(chan struct{})
	)

	// Enter sleep
	if ok := d.EnterSleep(nil); !ok {
		t.Fatalf("failed to enter sleep")
	}

	go func() {
		ctx := context.Background()
		t0 := time.Now()
		_ = d.WaitWake(ctx)
		wakeLatency = time.Since(t0)
		close(woken)
	}()

	// Brief yield to ensure goroutine is waiting on wakeCh
	time.Sleep(2 * time.Millisecond)

	// Trigger instant wake
	d.OnActivity()

	select {
	case <-woken:
		// Go channel wakeups on ARM64 typically occur in 5-30μs
		t.Logf("wake-up latency: %v", wakeLatency)
		if wakeLatency > 50*time.Millisecond {
			t.Errorf("wake-up latency took too long: %v", wakeLatency)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("worker failed to wake up within 1s")
	}
}

func TestIdleDetector_ContextCancellation(t *testing.T) {
	d := NewIdleDetector(10 * time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	d.EnterSleep(nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- d.WaitWake(ctx)
	}()

	time.Sleep(2 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for canceled WaitWake")
	}
}

func TestIdleDetector_ConcurrentStress(t *testing.T) {
	d := NewIdleDetector(1 * time.Millisecond)
	const cycles = 500
	const numProducers = 8

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Consumer / worker repeatedly sleeping and waking
	var workerWg sync.WaitGroup
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		for i := 0; i < cycles; i++ {
			time.Sleep(2 * time.Millisecond)
			_ = d.SleepAndBlock(ctx, nil)
		}
	}()

	// Producers concurrently triggering OnActivity
	wg.Add(numProducers)
	for p := 0; p < numProducers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < cycles; i++ {
				d.OnActivity()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	d.Close()
	workerWg.Wait()

	if d.IsSleeping() {
		t.Fatalf("expected active state after close")
	}
}
