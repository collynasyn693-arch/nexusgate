package chaos

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ErrTooManyConcurrentDelays is returned when the active concurrent delay ceiling is exceeded.
var ErrTooManyConcurrentDelays = errors.New("nexusgate: chaos concurrent delay limit exceeded")

// DelayInjector manages bounded, non-blocking latency injection with concurrency ceilings.
type DelayInjector struct {
	maxConcurrent int32
	active        atomic.Int32
	maxDelay      time.Duration
}

// NewDelayInjector constructs a DelayInjector with specified concurrency ceiling and delay ceiling.
func NewDelayInjector(maxConcurrent int, maxDelay time.Duration) *DelayInjector {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentDelays
	}
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	return &DelayInjector{
		maxConcurrent: int32(maxConcurrent),
		maxDelay:      maxDelay,
	}
}

// ActiveCount returns the number of currently active in-flight delay injections.
func (di *DelayInjector) ActiveCount() int32 {
	return di.active.Load()
}

// InjectDelay executes a non-blocking timer-based delay bound to the provided context.
// It guarantees zero leaked timers or memory allocations when client context is canceled.
func (di *DelayInjector) InjectDelay(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if di.maxDelay > 0 && d > di.maxDelay {
		d = di.maxDelay
	}

	current := di.active.Add(1)
	defer di.active.Add(-1)

	if di.maxConcurrent > 0 && current > di.maxConcurrent {
		return ErrTooManyConcurrentDelays
	}

	timer := time.NewTimer(d)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return ctx.Err()
	}
}

// SleepCtx is a standalone helper executing non-blocking, leak-free sleep bound to context.
func SleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	timer := time.NewTimer(d)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return ctx.Err()
	}
}
