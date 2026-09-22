package balancer

import (
	"context"
	"testing"
)

func TestP2C_HerdMitigation_SkewedLoad(t *testing.T) {
	b0 := NewBackend(mustURL("http://b0"), 10, 0) // Heavily loaded: 100 conns
	b1 := NewBackend(mustURL("http://b1"), 10, 0) // Heavily loaded: 80 conns
	b2 := NewBackend(mustURL("http://b2"), 10, 0) // Lightly loaded: 5 conns
	b3 := NewBackend(mustURL("http://b3"), 10, 0) // Idle: 0 conns

	b0.activeConns.Store(100)
	b1.activeConns.Store(80)
	b2.activeConns.Store(5)
	b3.activeConns.Store(0)

	balancer := NewP2C([]*Backend{b0, b1, b2, b3})
	ctx := context.Background()

	const iterations = 10000
	counts := make(map[string]int)

	for i := 0; i < iterations; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		counts[sel.RawURL]++
	}

	// b3 (0 conns) and b2 (5 conns) must absorb almost all traffic.
	// b0 (100 conns) should receive virtually zero traffic (< 100 requests out of 10,000, i.e. < 1%).
	t.Logf("P2C distribution under skewed load: b0=%d, b1=%d, b2=%d, b3=%d",
		counts[b0.RawURL], counts[b1.RawURL], counts[b2.RawURL], counts[b3.RawURL])

	if counts[b0.RawURL] > 100 {
		t.Errorf("b0 received %d requests; expected <= 100 under heavy load", counts[b0.RawURL])
	}
	if counts[b3.RawURL] < 4500 {
		t.Errorf("b3 received %d requests; expected >= 4500 (idle backend priority)", counts[b3.RawURL])
	}
}

func TestP2C_WeightedDistribution_VirtualInflight(t *testing.T) {
	// Two nodes with equal (zero) active connections but unequal weights (100 vs 1).
	bHeavy := NewBackend(mustURL("http://heavy"), 100, 0)
	bLight := NewBackend(mustURL("http://light"), 1, 0)

	balancer := NewP2C([]*Backend{bHeavy, bLight})
	ctx := context.Background()

	// With (conns + 1) * otherWeight:
	// Heavy load = (0 + 1) * 1 = 1
	// Light load = (0 + 1) * 100 = 100
	// Heavy load (1) < Light load (100) -> Heavy MUST be selected 100% of the time when both idle.
	for i := 0; i < 100; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		if sel.RawURL != "http://heavy" {
			t.Fatalf("step %d: expected http://heavy, got %s", i, sel.RawURL)
		}
	}
}

func TestP2C_DynamicLoadBalancing(t *testing.T) {
	// Simulate active requests: when a node is selected, its inflight count increases.
	// After selection, verify load balances smoothly across identical nodes.
	bA := NewBackend(mustURL("http://node-a"), 10, 0)
	bB := NewBackend(mustURL("http://node-b"), 10, 0)
	bC := NewBackend(mustURL("http://node-c"), 10, 0)

	balancer := NewP2C([]*Backend{bA, bB, bC})
	ctx := context.Background()

	const totalRequests = 3000
	counts := make(map[string]int)

	for i := 0; i < totalRequests; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		counts[sel.RawURL]++
		// Simulate holding a connection briefly
		sel.activeConns.Add(1)
		// Periodically release some to simulate steady-state processing
		if i%3 == 0 {
			bA.activeConns.Store(bA.activeConns.Load() / 2)
			bB.activeConns.Store(bB.activeConns.Load() / 2)
			bC.activeConns.Store(bC.activeConns.Load() / 2)
		}
	}

	// Each node should receive a fair share (~1000 requests, allowing reasonable variance)
	for _, raw := range []string{"http://node-a", "http://node-b", "http://node-c"} {
		c := counts[raw]
		if c < 700 || c > 1300 {
			t.Errorf("node %s received %d requests; expected between 700 and 1300", raw, c)
		}
	}
}

func TestP2C_EmptyOrSingle(t *testing.T) {
	bEmpty := NewP2C(nil)
	if _, err := bEmpty.Select(context.Background(), nil); err != ErrNoHealthyBackends {
		t.Errorf("expected ErrNoHealthyBackends, got %v", err)
	}

	bSingle := NewBackend(mustURL("http://only-one"), 10, 0)
	balancer := NewP2C([]*Backend{bSingle})
	for i := 0; i < 10; i++ {
		sel, err := balancer.Select(context.Background(), nil)
		if err != nil || sel != bSingle {
			t.Fatalf("expected bSingle, got %v, %v", sel, err)
		}
	}
}
