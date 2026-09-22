package balancer

import (
	"context"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
	"time"
)

// peakEWMABalancer implements the Peak-EWMA (Exponentially Weighted Moving Average) algorithm.
// It samples candidate backends via Power-of-Two-Choices and scores them using the product
// of active inflight connections and time-decayed latency trends:
// Cost = (inflight + 1) * EffectiveLatency(t).
// Traffic automatically evades suddenly spiked backends while dynamically probing recovering nodes.
type peakEWMABalancer struct {
	targets atomic.Pointer[[]*Backend]
}

// NewPeakEWMA constructs a new Peak-EWMA latency-aware load balancer.
func NewPeakEWMA(targets []*Backend) Balancer {
	b := &peakEWMABalancer{}
	b.UpdateTargets(targets)
	return b
}

// Name returns the balancing algorithm identifier.
func (b *peakEWMABalancer) Name() string {
	return string(AlgorithmPeakEWMA)
}

// UpdateTargets updates the active candidate set of healthy backends.
func (b *peakEWMABalancer) UpdateTargets(targets []*Backend) {
	filtered := make([]*Backend, 0, len(targets))
	for _, t := range targets {
		if t != nil && t.IsHealthy() && !t.IsDraining() {
			filtered = append(filtered, t)
		}
	}
	b.targets.Store(&filtered)
}

// Select picks the upstream target with the lower weighted latency-inflight cost:
// Cost1 = (conns1 + 1) * EffectiveLatency(now) * weight2
// vs
// Cost2 = (conns2 + 1) * EffectiveLatency(now) * weight1.
func (b *peakEWMABalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
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

	now := time.Now().UnixNano()
	ewma1 := c1.EffectiveLatency(now)
	ewma2 := c2.EffectiveLatency(now)

	cost1 := (c1.Inflight() + 1) * ewma1
	cost2 := (c2.Inflight() + 1) * ewma2

	w1 := c1.EffectiveWeight()
	w2 := c2.EffectiveWeight()
	if w1 <= 0 {
		w1 = 1
	}
	if w2 <= 0 {
		w2 = 1
	}

	// Weighted cost comparison via cross-multiplication: Cost / Weight
	weightedCost1 := cost1 * w2
	weightedCost2 := cost2 * w1

	if weightedCost1 < weightedCost2 {
		return c1, nil
	} else if weightedCost2 < weightedCost1 {
		return c2, nil
	}

	// Tie-breaker: prefer candidate with higher weight
	if w1 >= w2 {
		return c1, nil
	}
	return c2, nil
}
