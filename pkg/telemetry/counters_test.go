package telemetry

import (
	"sync"
	"testing"
)

func TestThroughputAggregator_ActiveConns(t *testing.T) {
	agg := NewThroughputAggregator()
	if c := agg.ActiveConns(); c != 0 {
		t.Fatalf("expected 0 active conns, got %d", c)
	}

	agg.IncActiveConns()
	agg.IncActiveConns()
	if c := agg.ActiveConns(); c != 2 {
		t.Fatalf("expected 2 active conns, got %d", c)
	}

	agg.DecActiveConns()
	if c := agg.ActiveConns(); c != 1 {
		t.Fatalf("expected 1 active conn, got %d", c)
	}

	agg.DecActiveConns()
	agg.DecActiveConns() // Underflow guard test
	if c := agg.ActiveConns(); c != 0 {
		t.Fatalf("expected 0 active conns after underflow, got %d", c)
	}
}

func TestThroughputAggregator_CumulativeTotals(t *testing.T) {
	agg := NewThroughputAggregator()
	baseSec := int64(1700000000)

	// Record 200 OK
	agg.Record(baseSec*1e9, 200, 100, 500)
	// Record 204 No Content
	agg.Record(baseSec*1e9, 204, 50, 0)
	// Record 301 Redirect
	agg.Record(baseSec*1e9, 301, 80, 120)
	// Record 404 Not Found
	agg.Record(baseSec*1e9, 404, 60, 200)
	// Record 503 Service Unavailable
	agg.Record(baseSec*1e9, 503, 10, 50)

	reqs, bIn, bOut, c2, c3, c4, c5 := agg.CumulativeTotals()
	if reqs != 5 {
		t.Errorf("expected 5 requests, got %d", reqs)
	}
	if bIn != 300 {
		t.Errorf("expected 300 bytesIn, got %d", bIn)
	}
	if bOut != 870 {
		t.Errorf("expected 870 bytesOut, got %d", bOut)
	}
	if c2 != 2 || c3 != 1 || c4 != 1 || c5 != 1 {
		t.Errorf("unexpected status counts: 2xx=%d, 3xx=%d, 4xx=%d, 5xx=%d", c2, c3, c4, c5)
	}
}

func TestThroughputAggregator_PassiveDecay(t *testing.T) {
	agg := NewThroughputAggregator()
	baseSec := int64(1700000000)

	// Record 60 requests in baseSec
	for i := 0; i < 60; i++ {
		agg.Record(baseSec*1e9, 200, 10, 10)
	}

	// At baseSec:
	// 1s window: 60 reqs -> 60.0 RPS
	// 10s window: 60 reqs / 10s -> 6.0 RPS
	// 60s window: 60 reqs / 60s -> 1.0 RPS
	r1, r10, r60 := agg.RatesAt(baseSec)
	if r1 != 60.0 {
		t.Errorf("expected rps1s=60.0, got %.2f", r1)
	}
	if r10 != 6.0 {
		t.Errorf("expected rps10s=6.0, got %.2f", r10)
	}
	if r60 != 1.0 {
		t.Errorf("expected rps60s=1.0, got %.2f", r60)
	}

	// At baseSec + 2 (2 seconds later, no new requests):
	// 1s window decayed to 0.0 RPS
	// 10s window remains 6.0 RPS
	// 60s window remains 1.0 RPS
	r1, r10, r60 = agg.RatesAt(baseSec + 2)
	if r1 != 0.0 {
		t.Errorf("expected rps1s=0.0 at t+2, got %.2f", r1)
	}
	if r10 != 6.0 {
		t.Errorf("expected rps10s=6.0 at t+2, got %.2f", r10)
	}
	if r60 != 1.0 {
		t.Errorf("expected rps60s=1.0 at t+2, got %.2f", r60)
	}

	// At baseSec + 15 (15 seconds later):
	// 1s window: 0.0 RPS
	// 10s window: decayed to 0.0 RPS
	// 60s window: remains 1.0 RPS
	r1, r10, r60 = agg.RatesAt(baseSec + 15)
	if r1 != 0.0 {
		t.Errorf("expected rps1s=0.0 at t+15, got %.2f", r1)
	}
	if r10 != 0.0 {
		t.Errorf("expected rps10s=0.0 at t+15, got %.2f", r10)
	}
	if r60 != 1.0 {
		t.Errorf("expected rps60s=1.0 at t+15, got %.2f", r60)
	}

	// At baseSec + 65 (65 seconds later):
	// All windows naturally decayed to strictly 0.0 RPS!
	r1, r10, r60 = agg.RatesAt(baseSec + 65)
	if r1 != 0.0 || r10 != 0.0 || r60 != 0.0 {
		t.Errorf("expected all rates to decay to 0.0 at t+65, got r1=%.2f, r10=%.2f, r60=%.2f", r1, r10, r60)
	}

	// Rolling status count decay at t+65
	c2, _, _, _ := agg.RollingStatusCounts(baseSec + 65)
	if c2 != 0 {
		t.Errorf("expected rolling 2xx status count to decay to 0, got %d", c2)
	}
}

func TestThroughputAggregator_Reset(t *testing.T) {
	agg := NewThroughputAggregator()
	agg.IncActiveConns()
	agg.Record(0, 200, 100, 200)

	agg.Reset()
	if c := agg.ActiveConns(); c != 0 {
		t.Fatalf("expected 0 active conns after reset, got %d", c)
	}
	reqs, _, _, _, _, _, _ := agg.CumulativeTotals()
	if reqs != 0 {
		t.Fatalf("expected 0 cumulative requests after reset, got %d", reqs)
	}
	r1, r10, r60 := agg.Rates()
	if r1 != 0 || r10 != 0 || r60 != 0 {
		t.Fatalf("expected 0 rates after reset, got %.2f, %.2f, %.2f", r1, r10, r60)
	}
}

func TestThroughputAggregator_ConcurrentAccess(t *testing.T) {
	agg := NewThroughputAggregator()
	const numGoroutines = 16
	const recordsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < recordsPerGoroutine; i++ {
				agg.Record(0, 200+(i%400), 50, 100)
				if i%100 == 0 {
					_, _, _ = agg.Rates()
				}
			}
		}(g)
	}

	wg.Wait()

	reqs, _, _, _, _, _, _ := agg.CumulativeTotals()
	expectedTotal := uint64(numGoroutines * recordsPerGoroutine)
	if reqs != expectedTotal {
		t.Fatalf("expected %d total requests, got %d", expectedTotal, reqs)
	}
}
