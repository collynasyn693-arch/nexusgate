package chaos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDelayInjector_ZeroOrNegative(t *testing.T) {
	di := NewDelayInjector(10, 5*time.Second)

	start := time.Now()
	err := di.InjectDelay(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error for 0 delay: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Errorf("zero delay took too long: %v", elapsed)
	}

	start = time.Now()
	err = di.InjectDelay(context.Background(), -100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error for negative delay: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Errorf("negative delay took too long: %v", elapsed)
	}
}

func TestDelayInjector_Precision(t *testing.T) {
	di := NewDelayInjector(10, 5*time.Second)

	targetDelay := 30 * time.Millisecond
	start := time.Now()
	err := di.InjectDelay(context.Background(), targetDelay)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("InjectDelay failed: %v", err)
	}
	if elapsed < 25*time.Millisecond || elapsed > 80*time.Millisecond {
		t.Errorf("expected elapsed duration ~30ms, got %v", elapsed)
	}
}

func TestDelayInjector_ContextCancellation(t *testing.T) {
	di := NewDelayInjector(10, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 15ms
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	// Attempt 500ms delay
	err := di.InjectDelay(ctx, 500*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("context cancellation abort took too long: %v", elapsed)
	}
}

func TestDelayInjector_PreCanceledContext(t *testing.T) {
	di := NewDelayInjector(10, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	start := time.Now()
	err := di.InjectDelay(ctx, 1*time.Second)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("pre-canceled context check took too long: %v", elapsed)
	}
}

func TestDelayInjector_MaxConcurrentCeiling(t *testing.T) {
	maxConcurrent := 2
	di := NewDelayInjector(maxConcurrent, 5*time.Second)

	var wg sync.WaitGroup
	started := make(chan struct{}, maxConcurrent)

	for i := 0; i < maxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started <- struct{}{}
			_ = di.InjectDelay(ctx, 80*time.Millisecond)
		}()
	}

	// Wait until both in-flight delays have started
	<-started
	<-started
	time.Sleep(10 * time.Millisecond)

	// Attempt 3rd concurrent delay - should fail immediately with ErrTooManyConcurrentDelays
	err := di.InjectDelay(context.Background(), 50*time.Millisecond)
	if !errors.Is(err, ErrTooManyConcurrentDelays) {
		t.Fatalf("expected ErrTooManyConcurrentDelays, got: %v", err)
	}

	wg.Wait()

	// After goroutines finish, active count must return to 0
	if active := di.ActiveCount(); active != 0 {
		t.Errorf("expected active count 0, got %d", active)
	}
}

func TestSleepCtx(t *testing.T) {
	// Normal sleep
	start := time.Now()
	err := SleepCtx(context.Background(), 20*time.Millisecond)
	if err != nil {
		t.Fatalf("SleepCtx failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("SleepCtx returned prematurely: %v", elapsed)
	}

	// Cancelled sleep
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = SleepCtx(ctx, 500*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
