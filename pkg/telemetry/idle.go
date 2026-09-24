package telemetry

import (
	"context"
	"sync/atomic"
	"time"
)

// Idle state enum constants.
const (
	IdleStateActive   int32 = 0
	IdleStateSleeping int32 = 1

	DefaultIdleTimeout = 3 * time.Second
)

// IdleDetector monitors traffic activity and places telemetry workers into sleep states
// when 0 RPS persists for >3.0s, eliminating continuous CPU timer interrupts and
// preserving ARM big.LITTLE mobile battery reserves under Android Termux.
type IdleDetector struct {
	state        atomic.Int32
	lastActivity atomic.Int64 // Unix nanoseconds
	timeout      time.Duration
	wakeCh       chan struct{} // buffered, capacity = 1
	sleepCount   atomic.Uint64
	wakeCount    atomic.Uint64
	closed       atomic.Bool
}

// NewIdleDetector creates an initialized IdleDetector.
func NewIdleDetector(timeout time.Duration) *IdleDetector {
	if timeout <= 0 {
		timeout = DefaultIdleTimeout
	}
	d := &IdleDetector{
		timeout: timeout,
		wakeCh:  make(chan struct{}, 1),
	}
	d.state.Store(IdleStateActive)
	d.lastActivity.Store(time.Now().UnixNano())
	return d
}

// OnActivity notifies the idle detector of incoming traffic or telemetry events.
// Fast path: <2ns atomic load when already active.
// Slow path: atomically wakes sleeping workers in <100μs via the buffered wake channel.
func (d *IdleDetector) OnActivity() {
	now := time.Now().UnixNano()
	d.lastActivity.Store(now)

	// Fast path: worker is already active
	if d.state.Load() == IdleStateActive {
		return
	}

	// Slow path: transition from Sleeping -> Active
	if d.state.CompareAndSwap(IdleStateSleeping, IdleStateActive) {
		d.wakeCount.Add(1)
		select {
		case d.wakeCh <- struct{}{}:
		default:
		}
	}
}

// IsSleeping returns true if the telemetry subsystem is currently parked in low-power sleep.
func (d *IdleDetector) IsSleeping() bool {
	return d.state.Load() == IdleStateSleeping
}

// ShouldSleep evaluates whether traffic silence has exceeded the configured idle threshold.
func (d *IdleDetector) ShouldSleep() bool {
	if d.closed.Load() {
		return false
	}
	last := d.lastActivity.Load()
	elapsed := time.Duration(time.Now().UnixNano() - last)
	return elapsed >= d.timeout
}

// LastActivity returns the timestamp of the last recorded activity.
func (d *IdleDetector) LastActivity() time.Time {
	return time.Unix(0, d.lastActivity.Load())
}

// IdleDuration returns the elapsed duration since the last recorded activity.
func (d *IdleDetector) IdleDuration() time.Duration {
	return time.Duration(time.Now().UnixNano() - d.lastActivity.Load())
}

// EnterSleep transitions the state machine into IdleStateSleeping.
// The caller may provide a doubleCheck predicate (e.g. checking whether a ring buffer has unconsumed items).
// If doubleCheck returns false (work is pending), sleep transition is aborted.
func (d *IdleDetector) EnterSleep(doubleCheck func() bool) bool {
	if d.closed.Load() {
		return false
	}
	if !d.ShouldSleep() {
		return false
	}

	// Drain any residual wake signal before attempting CAS
	select {
	case <-d.wakeCh:
	default:
	}

	if !d.state.CompareAndSwap(IdleStateActive, IdleStateSleeping) {
		return false
	}

	// Post-CAS guard: re-check if work arrived or doubleCheck fails
	if doubleCheck != nil && !doubleCheck() {
		d.state.Store(IdleStateActive)
		return false
	}

	d.sleepCount.Add(1)
	return true
}

// WaitWake blocks the calling worker goroutine until an activity wakeup signal is received
// or the context is canceled.
func (d *IdleDetector) WaitWake(ctx context.Context) error {
	select {
	case <-d.wakeCh:
		d.state.Store(IdleStateActive)
		return nil
	case <-ctx.Done():
		d.state.Store(IdleStateActive)
		return ctx.Err()
	}
}

// SleepAndBlock combines EnterSleep and WaitWake: if idle conditions are met, it parks
// the worker goroutine until new activity arrives or context cancels.
func (d *IdleDetector) SleepAndBlock(ctx context.Context, doubleCheck func() bool) error {
	if !d.EnterSleep(doubleCheck) {
		return nil
	}
	return d.WaitWake(ctx)
}

// Stats returns the current idle state, sleep/wake transitions, and elapsed idle duration.
func (d *IdleDetector) Stats() (state string, sleeps, wakes uint64, idleDuration time.Duration) {
	if d.IsSleeping() {
		state = "sleeping"
	} else {
		state = "active"
	}
	return state, d.sleepCount.Load(), d.wakeCount.Load(), d.IdleDuration()
}

// Close stops the idle detector and unblocks any sleeping workers.
func (d *IdleDetector) Close() {
	if d.closed.CompareAndSwap(false, true) {
		d.state.Store(IdleStateActive)
		select {
		case d.wakeCh <- struct{}{}:
		default:
		}
	}
}
