package balancer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
)

// mockBalancer is a simple test implementation of Balancer.
type mockBalancer struct {
	mu      sync.Mutex
	name    string
	targets []*Backend
}

func newMockBalancer(name string) *mockBalancer {
	return &mockBalancer{name: name}
}

func (m *mockBalancer) Name() string { return m.name }

func (m *mockBalancer) UpdateTargets(targets []*Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets = make([]*Backend, len(targets))
	copy(m.targets, targets)
}

func (m *mockBalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.targets) == 0 {
		return nil, ErrNoHealthyBackends
	}
	return m.targets[0], nil
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestTargetPool_BasicCRUD(t *testing.T) {
	b1 := NewBackend(mustURL("http://10.0.0.1:8080"), 10, 0)
	b2 := NewBackend(mustURL("http://10.0.0.2:8080"), 20, 0)

	bal := newMockBalancer("mock")
	pool := NewPool("pool-1", []*Backend{b1, b2}, bal)

	if pool.ID() != "pool-1" {
		t.Fatalf("expected pool ID 'pool-1', got %q", pool.ID())
	}

	// Lookup b1 and b2
	got1, ok1 := pool.Get("http://10.0.0.1:8080")
	if !ok1 || got1 != b1 {
		t.Fatalf("expected to find b1 in pool")
	}

	got2, ok2 := pool.Get("http://10.0.0.2:8080")
	if !ok2 || got2 != b2 {
		t.Fatalf("expected to find b2 in pool")
	}

	// Check Targets & HealthyTargets
	if len(pool.Targets()) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(pool.Targets()))
	}
	if len(pool.HealthyTargets()) != 2 {
		t.Fatalf("expected 2 healthy targets, got %d", len(pool.HealthyTargets()))
	}

	// Add b3
	b3 := NewBackend(mustURL("http://10.0.0.3:8080"), 30, 0)
	if err := pool.Add(b3); err != nil {
		t.Fatalf("unexpected error adding b3: %v", err)
	}
	if len(pool.Targets()) != 3 {
		t.Fatalf("expected 3 targets after add, got %d", len(pool.Targets()))
	}

	// Duplicate add must error
	if err := pool.Add(b3); err == nil {
		t.Fatalf("expected error on duplicate add")
	}

	// Nil backend add must error
	if err := pool.Add(nil); err == nil {
		t.Fatalf("expected error on nil backend add")
	}

	// Remove b2
	if !pool.Remove("http://10.0.0.2:8080") {
		t.Fatalf("expected Remove(b2) to succeed")
	}
	if len(pool.Targets()) != 2 {
		t.Fatalf("expected 2 targets after remove, got %d", len(pool.Targets()))
	}
	if _, found := pool.Get("http://10.0.0.2:8080"); found {
		t.Fatalf("removed target still found in pool")
	}

	// Remove non-existent target returns false
	if pool.Remove("http://non-existent:8080") {
		t.Fatalf("expected Remove of non-existent target to return false")
	}
}

func TestTargetPool_SetHealth(t *testing.T) {
	b1 := NewBackend(mustURL("http://10.0.0.1:8080"), 10, 0)
	b2 := NewBackend(mustURL("http://10.0.0.2:8080"), 20, 0)

	bal := newMockBalancer("mock")
	pool := NewPool("pool-health", []*Backend{b1, b2}, bal)

	if len(pool.HealthyTargets()) != 2 {
		t.Fatalf("expected 2 healthy targets initially")
	}

	// Mark b1 unhealthy
	if !pool.SetHealth("http://10.0.0.1:8080", false) {
		t.Fatalf("SetHealth(b1, false) failed")
	}
	if b1.IsHealthy() {
		t.Fatalf("expected b1.IsHealthy() to be false")
	}

	healthy := pool.HealthyTargets()
	if len(healthy) != 1 || healthy[0].RawURL != "http://10.0.0.2:8080" {
		t.Fatalf("expected only b2 in healthy targets, got %+v", healthy)
	}

	// Non-existent target health update returns false
	if pool.SetHealth("http://non-existent", true) {
		t.Fatalf("expected SetHealth on unknown target to return false")
	}

	// Mark b1 healthy again
	pool.SetHealth("http://10.0.0.1:8080", true)
	if len(pool.HealthyTargets()) != 2 {
		t.Fatalf("expected 2 healthy targets after re-enabling b1")
	}
}

func TestTargetPool_SetBalancer(t *testing.T) {
	b1 := NewBackend(mustURL("http://10.0.0.1:8080"), 10, 0)
	bal1 := newMockBalancer("bal1")
	bal2 := newMockBalancer("bal2")

	pool := NewPool("pool-swap", []*Backend{b1}, bal1)
	if pool.Balancer().Name() != "bal1" {
		t.Fatalf("expected bal1 active")
	}

	pool.SetBalancer(bal2)
	if pool.Balancer().Name() != "bal2" {
		t.Fatalf("expected bal2 active after SetBalancer")
	}

	// Ensure bal2 was primed with healthy targets
	sel, err := pool.Select(context.Background(), nil)
	if err != nil || sel != b1 {
		t.Fatalf("Select returned unexpected target: %v, err: %v", sel, err)
	}
}

func TestTargetPool_ConcurrentAccess(t *testing.T) {
	const numGoroutines = 50
	const numOps = 200

	backends := make([]*Backend, 5)
	for i := range backends {
		backends[i] = NewBackend(mustURL(fmt.Sprintf("http://10.0.0.%d:8080", i+1)), int64(10*(i+1)), 0)
	}

	bal := newMockBalancer("concurrent")
	pool := NewPool("concurrent-pool", backends, bal)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for op := 0; op < numOps; op++ {
				switch (id + op) % 5 {
				case 0:
					_ = pool.Targets()
				case 1:
					_ = pool.HealthyTargets()
				case 2:
					idx := op % len(backends)
					_, _ = pool.Get(backends[idx].RawURL)
				case 3:
					idx := op % len(backends)
					pool.SetHealth(backends[idx].RawURL, op%2 == 0)
				case 4:
					_, _ = pool.Select(context.Background(), nil)
				}
			}
		}(g)
	}

	wg.Wait()
}
