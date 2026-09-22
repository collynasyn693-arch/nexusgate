package balancer

import (
	"context"
	"net/http"
	"sync/atomic"
)

// roundRobinBalancer implements standard lock-free round-robin target selection.
type roundRobinBalancer struct {
	targets atomic.Pointer[[]*Backend]
	counter atomic.Uint64
}

// NewRoundRobin constructs a standard lock-free round-robin load balancer.
func NewRoundRobin(targets []*Backend) Balancer {
	b := &roundRobinBalancer{}
	b.UpdateTargets(targets)
	return b
}

// Name returns the balancing algorithm identifier.
func (b *roundRobinBalancer) Name() string {
	return string(AlgorithmRoundRobin)
}

// UpdateTargets updates the active target set.
func (b *roundRobinBalancer) UpdateTargets(targets []*Backend) {
	filtered := make([]*Backend, 0, len(targets))
	for _, t := range targets {
		if t != nil && t.IsHealthy() && !t.IsDraining() {
			filtered = append(filtered, t)
		}
	}
	b.targets.Store(&filtered)
}

// Select picks the next backend sequentially using an atomic sequence counter.
func (b *roundRobinBalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
	ptr := b.targets.Load()
	if ptr == nil {
		return nil, ErrNoHealthyBackends
	}
	targets := *ptr
	n := len(targets)
	if n == 0 {
		return nil, ErrNoHealthyBackends
	}
	if n == 1 {
		return targets[0], nil
	}

	idx := b.counter.Add(1) - 1
	return targets[idx%uint64(n)], nil
}

// NewBalancer instantiates a load balancing strategy matching the specified algorithm.
func NewBalancer(algo BalancingAlgorithm, targets []*Backend) (Balancer, error) {
	switch algo {
	case AlgorithmSWWR, AlgorithmSWRR:
		return NewSWWR(targets), nil
	case AlgorithmRoundRobin:
		return NewRoundRobin(targets), nil
	case AlgorithmP2C:
		return NewP2C(targets), nil
	case AlgorithmPeakEWMA:
		return NewPeakEWMA(targets), nil
	case AlgorithmIPHash:
		return NewIPHash(targets), nil
	default:
		return nil, ErrInvalidAlgorithm
	}
}

// ReloadPoolAlgorithm dynamically instantiates and atomically hot-reloads the load balancing
// strategy of an active TargetPool without dropping in-flight traffic.
func ReloadPoolAlgorithm(pool TargetPool, algo BalancingAlgorithm) error {
	if pool == nil {
		return ErrNoHealthyBackends
	}

	// Instantiate new algorithm pre-primed with current healthy pool targets
	newBalancer, err := NewBalancer(algo, pool.HealthyTargets())
	if err != nil {
		return err
	}

	// Atomically swap the balancer on the live pool
	pool.SetBalancer(newBalancer)
	return nil
}
