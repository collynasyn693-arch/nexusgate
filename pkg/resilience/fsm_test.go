package resilience

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestState_StringAndJSON(t *testing.T) {
	tests := []struct {
		state       State
		expectedStr string
		expectedJ   string
	}{
		{StateClosed, "Closed", `"Closed"`},
		{StateHalfOpen, "Half-Open", `"Half-Open"`},
		{StateOpen, "Open", `"Open"`},
		{State(99), "Unknown(99)", `"Unknown(99)"`},
	}

	for _, tc := range tests {
		if tc.state.String() != tc.expectedStr {
			t.Fatalf("expected string %q, got %q", tc.expectedStr, tc.state.String())
		}

		b, err := json.Marshal(tc.state)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		if string(b) != tc.expectedJ {
			t.Fatalf("expected json %s, got %s", tc.expectedJ, string(b))
		}
	}
}

func TestFSM_InitialState(t *testing.T) {
	cfg := DefaultConfig()
	fsm := NewBreakerFSM("test-fsm", cfg)

	if fsm.Name() != "test-fsm" {
		t.Fatalf("expected name 'test-fsm', got %q", fsm.Name())
	}
	if fsm.State() != StateClosed {
		t.Fatalf("expected initial state Closed, got %v", fsm.State())
	}
	if fsm.Generation() != 1 {
		t.Fatalf("expected generation 1, got %d", fsm.Generation())
	}
	if !fsm.Allow() {
		t.Fatalf("expected Allow() to return true in Closed state")
	}
}

func TestFSM_ValidStateTransitions(t *testing.T) {
	cfg := DefaultConfig()
	fsm := NewBreakerFSM("test-transitions", cfg)

	// 1. Closed -> Open via Trip()
	genBefore := fsm.Generation()
	if !fsm.Trip() {
		t.Fatalf("expected Trip() to return true when transitioning Closed -> Open")
	}
	if fsm.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", fsm.State())
	}
	if fsm.Generation() <= genBefore {
		t.Fatalf("expected generation increment")
	}
	// Redundant Trip should be idempotent
	if fsm.Trip() {
		t.Fatalf("expected redundant Trip() to return false")
	}

	// 2. Open -> HalfOpen via TransitionTo
	genBefore = fsm.Generation()
	if err := fsm.TransitionTo(StateHalfOpen); err != nil {
		t.Fatalf("unexpected error on Open -> HalfOpen: %v", err)
	}
	if fsm.State() != StateHalfOpen {
		t.Fatalf("expected state HalfOpen, got %v", fsm.State())
	}
	if fsm.Generation() <= genBefore {
		t.Fatalf("expected generation increment")
	}

	// 3. HalfOpen -> Closed via TransitionTo
	genBefore = fsm.Generation()
	if err := fsm.TransitionTo(StateClosed); err != nil {
		t.Fatalf("unexpected error on HalfOpen -> Closed: %v", err)
	}
	if fsm.State() != StateClosed {
		t.Fatalf("expected state Closed, got %v", fsm.State())
	}

	// 4. Closed -> Open -> HalfOpen -> Open (re-trip)
	if err := fsm.TransitionTo(StateOpen); err != nil {
		t.Fatalf("unexpected error on Closed -> Open: %v", err)
	}
	if err := fsm.TransitionTo(StateHalfOpen); err != nil {
		t.Fatalf("unexpected error on Open -> HalfOpen: %v", err)
	}
	if err := fsm.TransitionTo(StateOpen); err != nil {
		t.Fatalf("unexpected error on HalfOpen -> Open: %v", err)
	}
	if fsm.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", fsm.State())
	}
}

func TestFSM_InvalidStateTransitions(t *testing.T) {
	cfg := DefaultConfig()
	fsm := NewBreakerFSM("test-invalid", cfg)

	// 1. Closed -> HalfOpen directly is prohibited
	err := fsm.TransitionTo(StateHalfOpen)
	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for Closed -> HalfOpen, got %v", err)
	}

	// 2. Open -> Closed directly without canary probe is prohibited
	fsm.Trip()
	err = fsm.TransitionTo(StateClosed)
	if !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition for Open -> Closed, got %v", err)
	}

	// 3. Self transitions are idempotent no-ops
	if err := fsm.TransitionTo(StateOpen); err != nil {
		t.Fatalf("expected nil error for idempotent Open -> Open, got %v", err)
	}
}

func TestFSM_Reset(t *testing.T) {
	cfg := DefaultConfig()
	fsm := NewBreakerFSM("test-reset", cfg)

	fsm.Trip()
	if fsm.State() != StateOpen {
		t.Fatalf("expected Open state before reset")
	}

	fsm.Reset()
	if fsm.State() != StateClosed {
		t.Fatalf("expected Closed state after reset, got %v", fsm.State())
	}
	if fsm.OpenUntil() != 0 {
		t.Fatalf("expected openUntil to be 0 after reset")
	}
	if !fsm.Allow() {
		t.Fatalf("expected Allow() to be true after reset")
	}
}

func TestFSM_CooldownAndLazyHalfOpen(t *testing.T) {
	cfg := Config{
		ResetTimeout:        40 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	}
	fsm := NewBreakerFSM("test-cooldown", cfg)

	fsm.Trip()
	if fsm.State() != StateOpen {
		t.Fatalf("expected Open state")
	}

	// Immediate Allow() while Open should reject
	if fsm.Allow() {
		t.Fatalf("expected Allow() to return false while Open cooldown is active")
	}

	// Wait for cooldown to expire
	time.Sleep(50 * time.Millisecond)

	// First request after cooldown triggers transition to Half-Open and admits canary 1
	if !fsm.Allow() {
		t.Fatalf("expected Allow() to return true for 1st canary after cooldown")
	}
	if fsm.State() != StateHalfOpen {
		t.Fatalf("expected state to be HalfOpen after cooldown, got %v", fsm.State())
	}

	// Second canary allowed (HalfOpenMaxRequests = 2)
	if !fsm.Allow() {
		t.Fatalf("expected Allow() to return true for 2nd canary")
	}

	// Third canary should be rejected (limit reached)
	if fsm.Allow() {
		t.Fatalf("expected Allow() to return false when canary limit reached")
	}
}

func TestFSM_ConcurrentAllowRace(t *testing.T) {
	cfg := Config{
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 10,
	}
	fsm := NewBreakerFSM("test-race", cfg)
	fsm.Trip()

	time.Sleep(15 * time.Millisecond)

	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if fsm.Allow() {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if fsm.State() != StateHalfOpen {
		t.Fatalf("expected state HalfOpen, got %v", fsm.State())
	}
	if allowedCount > cfg.HalfOpenMaxRequests {
		t.Fatalf("expected at most %d canaries admitted, got %d", cfg.HalfOpenMaxRequests, allowedCount)
	}
	if allowedCount == 0 {
		t.Fatalf("expected at least 1 canary admitted")
	}
}
