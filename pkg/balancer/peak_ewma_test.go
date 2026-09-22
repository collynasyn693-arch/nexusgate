package balancer

import (
	"context"
	"testing"
	"time"
)

func TestPeakEWMA_LatencySpikeEvasion(t *testing.T) {
	bFast1 := NewBackend(mustURL("http://fast-1"), 10, 0)
	bFast2 := NewBackend(mustURL("http://fast-2"), 10, 0)
	bSpike := NewBackend(mustURL("http://spike"), 10, 0)

	// Set baseline latencies: fast nodes 5ms, spiked node 500ms
	bFast1.RecordLatency(5 * time.Millisecond)
	bFast2.RecordLatency(5 * time.Millisecond)
	bSpike.RecordLatency(500 * time.Millisecond)

	balancer := NewPeakEWMA([]*Backend{bFast1, bFast2, bSpike})
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

	t.Logf("Peak-EWMA spike distribution: fast1=%d, fast2=%d, spike=%d",
		counts[bFast1.RawURL], counts[bFast2.RawURL], counts[bSpike.RawURL])

	// Spiked node (500ms) should receive almost zero requests compared to 5ms nodes
	if counts[bSpike.RawURL] > 50 {
		t.Errorf("spiked backend received %d requests; expected <= 50", counts[bSpike.RawURL])
	}
	if counts[bFast1.RawURL]+counts[bFast2.RawURL] < 9950 {
		t.Errorf("expected fast nodes to handle almost all traffic: fast1=%d, fast2=%d",
			counts[bFast1.RawURL], counts[bFast2.RawURL])
	}
}

func TestPeakEWMA_StarvationRecovery(t *testing.T) {
	bSpike := NewBackend(mustURL("http://recovering"), 10, 0)
	// Spiked to 500ms
	bSpike.RecordLatency(500 * time.Millisecond)

	now := time.Now().UnixNano()
	// Simulate 60 seconds (6 tau half-lives) having elapsed with zero requests
	pastTime := now - int64(60*time.Second)
	bSpike.lastUpdateNanos.Store(pastTime)

	decayed := bSpike.EffectiveLatency(now)
	decayedDur := time.Duration(decayed)

	t.Logf("Effective latency after 60s idle: %v (initial 500ms, floor 1ms)", decayedDur)

	// After 60 seconds (6 half-lives of tau=10s, factor = e^-6 ≈ 0.00247),
	// 500ms should have decayed close to the 1ms floor (e.g. < 5ms).
	if decayedDur > 10*time.Millisecond {
		t.Errorf("expected latency to decay below 10ms after 60s; got %v", decayedDur)
	}
}

func TestPeakEWMA_DynamicConvergence(t *testing.T) {
	b1 := NewBackend(mustURL("http://node-1"), 10, 0)
	b2 := NewBackend(mustURL("http://node-2"), 10, 0)

	b1.RecordLatency(10 * time.Millisecond)
	b2.RecordLatency(10 * time.Millisecond)

	balancer := NewPeakEWMA([]*Backend{b1, b2})
	ctx := context.Background()

	// 1. Initially equal: 500 requests should split roughly evenly
	countsPhase1 := make(map[string]int)
	for i := 0; i < 500; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		countsPhase1[sel.RawURL]++
	}
	t.Logf("Phase 1 (equal): node1=%d, node2=%d", countsPhase1[b1.RawURL], countsPhase1[b2.RawURL])

	// 2. Sudden spike on node 1 to 300ms
	b1.RecordLatency(300 * time.Millisecond)

	countsPhase2 := make(map[string]int)
	for i := 0; i < 500; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		countsPhase2[sel.RawURL]++
	}
	t.Logf("Phase 2 (node1 spiked): node1=%d, node2=%d", countsPhase2[b1.RawURL], countsPhase2[b2.RawURL])
	if countsPhase2[b1.RawURL] != 0 {
		t.Errorf("expected 0 requests for spiked node1, got %d", countsPhase2[b1.RawURL])
	}

	// 3. Node 1 recovers to 5ms
	b1.latencyEWMA.Store(5 * int64(time.Millisecond))
	b1.lastUpdateNanos.Store(time.Now().UnixNano())

	countsPhase3 := make(map[string]int)
	for i := 0; i < 500; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		countsPhase3[sel.RawURL]++
	}
	t.Logf("Phase 3 (node1 recovered to 5ms vs node2 10ms): node1=%d, node2=%d",
		countsPhase3[b1.RawURL], countsPhase3[b2.RawURL])
	// Node 1 (5ms) is faster than Node 2 (10ms) -> Node 1 must receive 100% of 2-node selections
	if countsPhase3[b1.RawURL] != 500 {
		t.Errorf("expected node 1 (5ms) to win all selections against node 2 (10ms), got %d",
			countsPhase3[b1.RawURL])
	}
}

func TestPeakEWMA_EmptyOrSingle(t *testing.T) {
	bEmpty := NewPeakEWMA(nil)
	if _, err := bEmpty.Select(context.Background(), nil); err != ErrNoHealthyBackends {
		t.Errorf("expected ErrNoHealthyBackends, got %v", err)
	}

	bSingle := NewBackend(mustURL("http://single"), 10, 0)
	balancer := NewPeakEWMA([]*Backend{bSingle})
	for i := 0; i < 10; i++ {
		sel, err := balancer.Select(context.Background(), nil)
		if err != nil || sel != bSingle {
			t.Fatalf("expected bSingle, got %v, %v", sel, err)
		}
	}
}
