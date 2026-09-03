package config

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ReloadListener represents a callback triggered whenever an atomic configuration swap occurs.
// Note: oldCfg and newCfg are strictly immutable.
type ReloadListener func(oldCfg, newCfg *GatewayConfig)

// ConfigHolder maintains the active gateway configuration with zero-lock atomic reads
// and thread-safe dynamic hot-reloading using sync/atomic.Pointer.
type ConfigHolder struct {
	ptr       atomic.Pointer[GatewayConfig]
	mu        sync.Mutex
	nextSubID uint64
	listeners map[uint64]ReloadListener
}

// NewConfigHolder initializes a new ConfigHolder with an initial configuration.
func NewConfigHolder(initialCfg *GatewayConfig) *ConfigHolder {
	if initialCfg == nil {
		panic("initial configuration cannot be nil")
	}
	h := &ConfigHolder{
		listeners: make(map[uint64]ReloadListener),
	}
	h.ptr.Store(initialCfg)
	return h
}

// Get returns the currently active GatewayConfig.
// This is the hot-path method: lock-free, zero allocations (0 B/op), executes in sub-nanosecond time.
// The returned pointer is strictly immutable.
func (h *ConfigHolder) Get() *GatewayConfig {
	return h.ptr.Load()
}

// Swap atomically replaces the active configuration with newCfg.
// Listeners are executed outside the internal mutex to prevent deadlocks.
func (h *ConfigHolder) Swap(newCfg *GatewayConfig) (*GatewayConfig, error) {
	if newCfg == nil {
		return nil, fmt.Errorf("new configuration cannot be nil")
	}

	h.mu.Lock()
	oldCfg := h.ptr.Swap(newCfg)

	// Copy listeners under lock to prevent concurrent map modification
	listenersCopy := make([]ReloadListener, 0, len(h.listeners))
	for _, fn := range h.listeners {
		if fn != nil {
			listenersCopy = append(listenersCopy, fn)
		}
	}
	h.mu.Unlock()

	// Execute callbacks outside lock
	for _, fn := range listenersCopy {
		fn(oldCfg, newCfg)
	}

	return oldCfg, nil
}

// Subscribe registers a listener callback to be invoked on configuration reloads.
// Returns an unsubscribe function to unregister the listener.
func (h *ConfigHolder) Subscribe(fn ReloadListener) func() {
	if fn == nil {
		return func() {}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.listeners == nil {
		h.listeners = make(map[uint64]ReloadListener)
	}
	h.nextSubID++
	id := h.nextSubID
	h.listeners[id] = fn

	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.listeners, id)
	}
}

// Update performs an atomic Read-Copy-Update transformation on the configuration.
// It deep-clones the active configuration, passes it to the mutator function,
// validates the candidate, and atomically swaps it in.
func (h *ConfigHolder) Update(mutator func(candidate *GatewayConfig) error) (*GatewayConfig, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	current := h.ptr.Load()
	candidate := current.Clone()

	if err := mutator(candidate); err != nil {
		return nil, fmt.Errorf("config mutation failed: %w", err)
	}

	if err := Validate(candidate); err != nil {
		return nil, fmt.Errorf("mutated configuration failed validation: %w", err)
	}

	oldCfg := h.ptr.Swap(candidate)

	// Copy and dispatch listeners
	listenersCopy := make([]ReloadListener, 0, len(h.listeners))
	for _, fn := range h.listeners {
		if fn != nil {
			listenersCopy = append(listenersCopy, fn)
		}
	}

	// Dispatch in background or after releasing lock if called via Update
	go func() {
		for _, fn := range listenersCopy {
			fn(oldCfg, candidate)
		}
	}()

	return candidate, nil
}
