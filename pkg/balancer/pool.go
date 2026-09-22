package balancer

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// poolState holds an immutable snapshot of targets, healthy candidates, and the active balancer.
// State reads are 100% lock-free and wait-free via atomic.Pointer.
type poolState struct {
	targets   []*Backend
	healthy   []*Backend
	targetMap map[string]*Backend
	balancer  Balancer
}

// Pool implements the TargetPool interface managing backend nodes with Copy-On-Write snapshots.
type Pool struct {
	id    string
	state atomic.Pointer[poolState]
	mu    sync.Mutex // serializes state mutations (Add, Remove, SetHealth, SetBalancer)
}

// NewPool constructs a new upstream target pool with an initial set of targets and a balancer strategy.
func NewPool(id string, initialTargets []*Backend, b Balancer) *Pool {
	p := &Pool{id: id}

	targets := make([]*Backend, 0, len(initialTargets))
	healthy := make([]*Backend, 0, len(initialTargets))
	targetMap := make(map[string]*Backend, len(initialTargets))

	for _, t := range initialTargets {
		if t == nil {
			continue
		}
		targets = append(targets, t)
		targetMap[t.RawURL] = t
		if t.IsHealthy() && !t.IsDraining() {
			healthy = append(healthy, t)
		}
	}

	if b != nil {
		b.UpdateTargets(healthy)
	}

	s := &poolState{
		targets:   targets,
		healthy:   healthy,
		targetMap: targetMap,
		balancer:  b,
	}
	p.state.Store(s)
	return p
}

// ID returns the target pool's identifier.
func (p *Pool) ID() string {
	return p.id
}

// Get retrieves a backend target by its raw URL string in O(1) lock-free time.
func (p *Pool) Get(rawURL string) (*Backend, bool) {
	s := p.state.Load()
	if s == nil {
		return nil, false
	}
	b, ok := s.targetMap[rawURL]
	return b, ok
}

// Targets returns an immutable snapshot of all registered targets.
func (p *Pool) Targets() []*Backend {
	s := p.state.Load()
	if s == nil {
		return nil
	}
	return s.targets
}

// HealthyTargets returns an immutable snapshot of currently healthy, non-draining targets.
func (p *Pool) HealthyTargets() []*Backend {
	s := p.state.Load()
	if s == nil {
		return nil
	}
	return s.healthy
}

// Balancer returns the currently active load balancer strategy.
func (p *Pool) Balancer() Balancer {
	s := p.state.Load()
	if s == nil {
		return nil
	}
	return s.balancer
}

// SetBalancer atomically swaps the active balancing algorithm, pre-priming it with healthy targets.
func (p *Pool) SetBalancer(b Balancer) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cur := p.state.Load()
	if b != nil && cur != nil {
		b.UpdateTargets(cur.healthy)
	}

	newState := &poolState{
		targets:   cur.targets,
		healthy:   cur.healthy,
		targetMap: cur.targetMap,
		balancer:  b,
	}
	p.state.Store(newState)
}

// Add registers a new backend target into the pool.
func (p *Pool) Add(backend *Backend) error {
	if backend == nil {
		return fmt.Errorf("nexusgate: cannot add nil backend")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cur := p.state.Load()
	if _, exists := cur.targetMap[backend.RawURL]; exists {
		return fmt.Errorf("nexusgate: backend %q already exists in pool", backend.RawURL)
	}

	newTargets := make([]*Backend, len(cur.targets), len(cur.targets)+1)
	copy(newTargets, cur.targets)
	newTargets = append(newTargets, backend)

	newMap := make(map[string]*Backend, len(newTargets))
	newHealthy := make([]*Backend, 0, len(newTargets))

	for _, t := range newTargets {
		newMap[t.RawURL] = t
		if t.IsHealthy() && !t.IsDraining() {
			newHealthy = append(newHealthy, t)
		}
	}

	bal := cur.balancer
	if bal != nil {
		bal.UpdateTargets(newHealthy)
	}

	newState := &poolState{
		targets:   newTargets,
		healthy:   newHealthy,
		targetMap: newMap,
		balancer:  bal,
	}
	p.state.Store(newState)
	return nil
}

// Remove deregisters a backend target by its raw URL string.
func (p *Pool) Remove(rawURL string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	cur := p.state.Load()
	if _, exists := cur.targetMap[rawURL]; !exists {
		return false
	}

	newTargets := make([]*Backend, 0, len(cur.targets)-1)
	newHealthy := make([]*Backend, 0, len(cur.healthy))
	newMap := make(map[string]*Backend, len(cur.targets)-1)

	for _, t := range cur.targets {
		if t.RawURL == rawURL {
			continue
		}
		newTargets = append(newTargets, t)
		newMap[t.RawURL] = t
		if t.IsHealthy() && !t.IsDraining() {
			newHealthy = append(newHealthy, t)
		}
	}

	bal := cur.balancer
	if bal != nil {
		bal.UpdateTargets(newHealthy)
	}

	newState := &poolState{
		targets:   newTargets,
		healthy:   newHealthy,
		targetMap: newMap,
		balancer:  bal,
	}
	p.state.Store(newState)
	return true
}

// SetHealth updates the health status of a target and refreshes the pool snapshot.
func (p *Pool) SetHealth(rawURL string, healthy bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	cur := p.state.Load()
	target, exists := cur.targetMap[rawURL]
	if !exists {
		return false
	}

	target.SetHealthy(healthy)

	newHealthy := make([]*Backend, 0, len(cur.targets))
	for _, t := range cur.targets {
		if t.IsHealthy() && !t.IsDraining() {
			newHealthy = append(newHealthy, t)
		}
	}

	bal := cur.balancer
	if bal != nil {
		bal.UpdateTargets(newHealthy)
	}

	newState := &poolState{
		targets:   cur.targets,
		healthy:   newHealthy,
		targetMap: cur.targetMap,
		balancer:  bal,
	}
	p.state.Store(newState)
	return true
}

// Drain marks a backend target as draining, excludes it from healthy routing, and waits
// for active in-flight connections to bleed down to zero or until the timeout expires.
func (p *Pool) Drain(ctx context.Context, rawURL string, timeout time.Duration) error {
	target, exists := p.Get(rawURL)
	if !exists {
		return ErrBackendNotFound
	}

	// 1. Mark target as draining
	target.SetDraining(true)

	// 2. Refresh healthy snapshot to immediately exclude target from new selections
	p.mu.Lock()
	cur := p.state.Load()
	newHealthy := make([]*Backend, 0, len(cur.targets))
	for _, t := range cur.targets {
		if t.IsHealthy() && !t.IsDraining() {
			newHealthy = append(newHealthy, t)
		}
	}
	bal := cur.balancer
	if bal != nil {
		bal.UpdateTargets(newHealthy)
	}
	newState := &poolState{
		targets:   cur.targets,
		healthy:   newHealthy,
		targetMap: cur.targetMap,
		balancer:  bal,
	}
	p.state.Store(newState)
	p.mu.Unlock()

	// 3. Wait for in-flight connections to bleed down
	return p.drainBackend(ctx, target, timeout)
}

// drainBackend coordinates the graceful connection bleed for a specific target.
func (p *Pool) drainBackend(ctx context.Context, b *Backend, timeout time.Duration) error {
	// Immediate short-circuit if backend has zero in-flight connections
	if b.Inflight() <= 0 {
		b.signalDrainDone()
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-b.drainDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrDrainTimeout
	}
}

// Select routes an incoming request to an upstream target using the active balancer strategy.
func (p *Pool) Select(ctx context.Context, req *http.Request) (*Backend, error) {
	s := p.state.Load()
	if s == nil || s.balancer == nil {
		return nil, ErrNoHealthyBackends
	}
	return s.balancer.Select(ctx, req)
}
