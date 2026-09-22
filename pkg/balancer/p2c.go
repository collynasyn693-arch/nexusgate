package balancer

import (
	"context"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
)

// p2cBalancer implements the Power-of-Two-Choices (P2C) Least-Loaded algorithm.
// It selects two distinct candidates uniformly at random and routes to the node with
// the lower weighted inflight load, drastically mitigating queue herd effects.
type p2cBalancer struct {
	targets atomic.Pointer[[]*Backend]
}

// NewP2C constructs a new Power-of-Two-Choices load balancer.
func NewP2C(targets []*Backend) Balancer {
	b := &p2cBalancer{}
	b.UpdateTargets(targets)
	return b
}

// Name returns the balancing algorithm identifier.
func (b *p2cBalancer) Name() string {
	return string(AlgorithmP2C)
}

// UpdateTargets updates the internal candidate snapshot with healthy, non-draining backends.
func (b *p2cBalancer) UpdateTargets(targets []*Backend) {
	filtered := make([]*Backend, 0, len(targets))
	for _, t := range targets {
		if t != nil && t.IsHealthy() && !t.IsDraining() {
			filtered = append(filtered, t)
		}
	}
	b.targets.Store(&filtered)
}

// Select picks the least-loaded candidate between two randomly selected backends.
// Load is evaluated using integer cross-multiplication with virtual inflight addition:
// load1 = (conns1 + 1) * weight2 vs load2 = (conns2 + 1) * weight1.
func (b *p2cBalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
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

	var i, j int
	if n == 2 {
		i, j = 0, 1
	} else {
		// Uniform random pair selection in O(1) without rejection loops
		i = rand.N(n)
		j = rand.N(n - 1)
		if j >= i {
			j++
		}
	}

	c1 := targets[i]
	c2 := targets[j]

	conns1 := c1.Inflight()
	conns2 := c2.Inflight()
	w1 := c1.EffectiveWeight()
	w2 := c2.EffectiveWeight()
	if w1 <= 0 {
		w1 = 1
	}
	if w2 <= 0 {
		w2 = 1
	}

	// Cross-multiplication of normalized loads with virtual inflight: (conns + 1) / weight
	load1 := (conns1 + 1) * w2
	load2 := (conns2 + 1) * w1

	if load1 < load2 {
		return c1, nil
	} else if load2 < load1 {
		return c2, nil
	}

	// Tie-breaking: when normalized loads are equal, prefer higher configured weight
	if w1 > w2 {
		return c1, nil
	} else if w2 > w1 {
		return c2, nil
	}
	return c1, nil
}
