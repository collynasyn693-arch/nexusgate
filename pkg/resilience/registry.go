package resilience

import (
	"sync"
)

// BreakerRegistry is a thread-safe registry managing circuit breakers and health probers
// across multiple independent upstream targets. Supports dynamic upstream addition,
// removal, and sweeping of orphaned breakers.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]CircuitBreaker
	probers  map[string]HealthChecker
	closed   bool
}

// NewRegistry constructs an initialized BreakerRegistry.
func NewRegistry() *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]CircuitBreaker),
		probers:  make(map[string]HealthChecker),
	}
}

// Get retrieves a circuit breaker by upstream name. Returns false if not found.
func (r *BreakerRegistry) Get(name string) (CircuitBreaker, bool) {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()
	return cb, ok
}

// GetOrCreate returns an existing circuit breaker or creates, registers, and returns a new one.
func (r *BreakerRegistry) GetOrCreate(name string, cfg Config) CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check under write lock
	if cb, ok := r.breakers[name]; ok {
		return cb
	}

	newBreaker := NewBreaker(name, cfg)
	r.breakers[name] = newBreaker
	return newBreaker
}

// Register explicitly adds a circuit breaker to the registry.
func (r *BreakerRegistry) Register(breaker CircuitBreaker) {
	if breaker == nil {
		return
	}
	r.mu.Lock()
	r.breakers[breaker.Name()] = breaker
	r.mu.Unlock()
}

// RegisterProber associates an active background health checker with an upstream target.
func (r *BreakerRegistry) RegisterProber(name string, prober HealthChecker) {
	if prober == nil {
		return
	}
	r.mu.Lock()
	// Stop existing prober if replacing
	if old, exists := r.probers[name]; exists && old != nil {
		old.Stop()
	}
	r.probers[name] = prober
	r.mu.Unlock()
}

// GetProber retrieves an active background health checker by upstream name.
func (r *BreakerRegistry) GetProber(name string) (HealthChecker, bool) {
	r.mu.RLock()
	p, ok := r.probers[name]
	r.mu.RUnlock()
	return p, ok
}

// Remove deletes a circuit breaker and stops its associated health prober.
func (r *BreakerRegistry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	cb, exists := r.breakers[name]
	if exists {
		delete(r.breakers, name)
	}

	if prober, ok := r.probers[name]; ok {
		prober.Stop()
		delete(r.probers, name)
	}

	return exists || cb != nil
}

// All returns a shallow-copy map snapshot of all registered circuit breakers.
func (r *BreakerRegistry) All() map[string]CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]CircuitBreaker, len(r.breakers))
	for k, v := range r.breakers {
		snapshot[k] = v
	}
	return snapshot
}

// ResetAll resets all circuit breakers in the registry back to StateClosed.
func (r *BreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// Sweep removes any circuit breakers and health probers that are not present
// in the provided slice of active upstream identifiers.
// This prevents memory leaks and orphaned background probe timers during hot-reloads.
func (r *BreakerRegistry) Sweep(activeUpstreams []string) int {
	activeSet := make(map[string]struct{}, len(activeUpstreams))
	for _, id := range activeUpstreams {
		activeSet[id] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	removedCount := 0
	for id, cb := range r.breakers {
		if _, active := activeSet[id]; !active {
			cb.Reset()
			delete(r.breakers, id)
			removedCount++
		}
	}

	for id, prober := range r.probers {
		if _, active := activeSet[id]; !active {
			prober.Stop()
			delete(r.probers, id)
		}
	}

	return removedCount
}

// Close gracefully stops all health probers and clears all registered circuit breakers.
func (r *BreakerRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.closed = true

	for _, p := range r.probers {
		if p != nil {
			p.Stop()
		}
	}

	for _, cb := range r.breakers {
		if cb != nil {
			cb.Reset()
		}
	}

	r.breakers = make(map[string]CircuitBreaker)
	r.probers = make(map[string]HealthChecker)
}
