package balancer

import (
	"context"
	"net/http"
	"sync"
)

// swwrPeer tracks the runtime currentWeight for an upstream backend target in SWWR.
type swwrPeer struct {
	backend       *Backend
	currentWeight int64
}

// swwrBalancer implements the deterministic Nginx Smooth Weighted Round-Robin algorithm.
// It interleaves target selection proportionally according to configured effective weights,
// ensuring smooth traffic distribution without burstiness (e.g. 5:1:1 -> AABACAA).
type swwrBalancer struct {
	mu          sync.Mutex
	peers       []swwrPeer
	totalWeight int64
}

// NewSWWR creates a new Smooth Weighted Round-Robin load balancer.
func NewSWWR(targets []*Backend) Balancer {
	b := &swwrBalancer{}
	b.UpdateTargets(targets)
	return b
}

// NewSWRR creates a new Smooth Weighted Round-Robin load balancer (canonical alias).
func NewSWRR(targets []*Backend) Balancer {
	return NewSWWR(targets)
}

// Name returns the balancing algorithm identifier.
func (b *swwrBalancer) Name() string {
	return string(AlgorithmSWWR)
}

// UpdateTargets updates the active target set and re-centers currentWeight to preserve
// the zero-sum invariant across dynamic membership and health changes.
func (b *swwrBalancer) UpdateTargets(targets []*Backend) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.peers = make([]swwrPeer, 0, len(targets))
	b.totalWeight = 0

	for _, target := range targets {
		if target == nil || !target.IsHealthy() || target.IsDraining() {
			continue
		}
		w := target.EffectiveWeight()
		if w <= 0 {
			w = 1
		}
		b.peers = append(b.peers, swwrPeer{
			backend:       target,
			currentWeight: 0,
		})
		b.totalWeight += w
	}
}

// Select picks the next backend target according to the Smooth Weighted Round-Robin algorithm:
// 1. Increment currentWeight of each target by its effectiveWeight.
// 2. Select the target with the maximum currentWeight.
// 3. Subtract totalWeight from the selected target's currentWeight.
// Mathematical Invariant: At all times, sum(currentWeight) == 0.
func (b *swwrBalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n := len(b.peers)
	if n == 0 || b.totalWeight <= 0 {
		return nil, ErrNoHealthyBackends
	}
	if n == 1 {
		return b.peers[0].backend, nil
	}

	var bestIdx int = -1
	var maxWeight int64 = -1 << 63

	for i := 0; i < n; i++ {
		w := b.peers[i].backend.EffectiveWeight()
		if w <= 0 {
			w = 1
		}
		b.peers[i].currentWeight += w
		if b.peers[i].currentWeight > maxWeight {
			maxWeight = b.peers[i].currentWeight
			bestIdx = i
		}
	}

	b.peers[bestIdx].currentWeight -= b.totalWeight
	return b.peers[bestIdx].backend, nil
}
