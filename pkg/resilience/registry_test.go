package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistry_BasicOperations(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()

	cfg := DefaultConfig()

	// 1. GetOrCreate
	cb1 := reg.GetOrCreate("upstream-auth", cfg)
	if cb1 == nil || cb1.Name() != "upstream-auth" {
		t.Fatalf("unexpected breaker from GetOrCreate: %v", cb1)
	}

	// 2. Get existing
	cbFound, ok := reg.Get("upstream-auth")
	if !ok || cbFound != cb1 {
		t.Fatalf("expected to find upstream-auth breaker")
	}

	// 3. All snapshot
	all := reg.All()
	if len(all) != 1 || all["upstream-auth"] != cb1 {
		t.Fatalf("unexpected snapshot from All(): %v", all)
	}

	// 4. Remove
	if !reg.Remove("upstream-auth") {
		t.Fatalf("expected Remove to return true")
	}
	if _, ok := reg.Get("upstream-auth"); ok {
		t.Fatalf("upstream-auth still found after Remove")
	}
	if reg.Remove("non-existent") {
		t.Fatalf("expected Remove of non-existent to return false")
	}
}

// mockProber implements HealthChecker for testing registry prober lifecycle.
type mockProber struct {
	target  string
	stopped atomic.Bool
}

func (m *mockProber) Target() string                   { return m.target }
func (m *mockProber) IsHealthy() bool                  { return !m.stopped.Load() }
func (m *mockProber) Start(ctx context.Context) error  { return nil }
func (m *mockProber) Stop()                            { m.stopped.Store(true) }
func (m *mockProber) CheckOnce(ctx context.Context) error { return nil }

func TestRegistry_SweepOrphanedBreakers(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()

	cfg := DefaultConfig()
	_ = reg.GetOrCreate("srv-1", cfg)
	_ = reg.GetOrCreate("srv-2", cfg)
	_ = reg.GetOrCreate("srv-3", cfg)

	p2 := &mockProber{target: "srv-2"}
	reg.RegisterProber("srv-2", p2)

	p3 := &mockProber{target: "srv-3"}
	reg.RegisterProber("srv-3", p3)

	// Active configuration only has srv-1 and srv-3 (srv-2 was removed during reload)
	activeList := []string{"srv-1", "srv-3"}
	removed := reg.Sweep(activeList)

	if removed != 1 {
		t.Fatalf("expected 1 orphaned breaker removed, got %d", removed)
	}

	// srv-2 must be gone, and its prober stopped
	if _, ok := reg.Get("srv-2"); ok {
		t.Fatalf("srv-2 still found after Sweep")
	}
	if !p2.stopped.Load() {
		t.Fatalf("expected srv-2 prober to be stopped upon sweep")
	}

	// srv-1 and srv-3 must remain
	if _, ok := reg.Get("srv-1"); !ok {
		t.Fatalf("srv-1 missing after Sweep")
	}
	if _, ok := reg.Get("srv-3"); !ok {
		t.Fatalf("srv-3 missing after Sweep")
	}
	if p3.stopped.Load() {
		t.Fatalf("srv-3 prober should not have been stopped")
	}
}

func TestRegistry_ConcurrentLoadRace(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()

	cfg := Config{
		ConsecutiveFailures: 3,
		ResetTimeout:        50 * time.Millisecond,
	}

	numGoroutines := 100
	numUpstreams := 10
	iterations := 50

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				upstreamName := fmt.Sprintf("upstream-%d", (workerID+j)%numUpstreams)

				// 1. GetOrCreate
				cb := reg.GetOrCreate(upstreamName, cfg)
				if cb == nil {
					t.Errorf("nil breaker returned")
					return
				}

				// 2. Query State and Allow
				_ = cb.Allow()
				_ = cb.State()

				// 3. Mutate (record result)
				if (workerID+j)%3 == 0 {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}

				// 4. Occasional All() snapshot query
				if j%20 == 0 {
					_ = reg.All()
				}

				// 5. Occasional Sweep
				if workerID == 0 && j%10 == 0 {
					reg.Sweep([]string{"upstream-0", "upstream-1", "upstream-2", "upstream-3", "upstream-4", "upstream-5", "upstream-6", "upstream-7", "upstream-8", "upstream-9"})
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all upstreams remain valid
	all := reg.All()
	if len(all) == 0 {
		t.Fatalf("expected active breakers remaining in registry")
	}
}
