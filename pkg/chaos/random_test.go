package chaos

import (
	"math"
	"sync"
	"testing"
)

func TestFaultInjector_Boundaries(t *testing.T) {
	fi := NewFaultInjector(0.0)

	// Rate 0 must never inject
	for i := 0; i < 1000; i++ {
		if fi.ShouldInject(0.0) {
			t.Fatalf("rate 0 injected fault at iteration %d", i)
		}
	}

	// Rate 1.0 must always inject
	for i := 0; i < 1000; i++ {
		if !fi.ShouldInject(1.0) {
			t.Fatalf("rate 1.0 failed to inject fault at iteration %d", i)
		}
	}

	// NaN and Inf must not inject
	if fi.ShouldInject(math.NaN()) {
		t.Errorf("NaN should not inject")
	}
	if fi.ShouldInject(math.Inf(1)) {
		t.Errorf("Inf should not inject")
	}
}

func TestFaultInjector_StatisticalDistribution(t *testing.T) {
	fi := NewFaultInjector(0.0)

	// Test 10% failure rate over 10,000 trials
	// Expected: 1,000 faults. Stddev: sqrt(10000 * 0.10 * 0.90) = 30.
	// 4-sigma range: [880, 1120]
	const trials = 10000
	rate10 := 0.10
	faults10 := 0

	for i := 0; i < trials; i++ {
		if fi.ShouldInject(rate10) {
			faults10++
		}
	}

	if faults10 < 880 || faults10 > 1120 {
		t.Errorf("10%% fault rate generated %d faults over %d trials (expected 880..1120)", faults10, trials)
	}

	// Test 50% failure rate over 10,000 trials
	// Expected: 5,000 faults. Stddev: sqrt(10000 * 0.50 * 0.50) = 50.
	// 4-sigma range: [4800, 5200]
	rate50 := 0.50
	faults50 := 0

	for i := 0; i < trials; i++ {
		if fi.ShouldInject(rate50) {
			faults50++
		}
	}

	if faults50 < 4800 || faults50 > 5200 {
		t.Errorf("50%% fault rate generated %d faults over %d trials (expected 4800..5200)", faults50, trials)
	}
}

func TestFaultInjector_DeterministicCustomRand(t *testing.T) {
	fi := NewFaultInjector(0.20)

	seq := []float64{0.10, 0.25, 0.05, 0.30}
	idx := 0
	fi.SetRandFunc(func() float64 {
		val := seq[idx%len(seq)]
		idx++
		return val
	})

	// 0.10 < 0.20 -> true
	if !fi.ShouldInject(0) {
		t.Errorf("expected true for 0.10 < 0.20")
	}
	// 0.25 < 0.20 -> false
	if fi.ShouldInject(0) {
		t.Errorf("expected false for 0.25 < 0.20")
	}
	// 0.05 < 0.20 -> true
	if !fi.ShouldInject(0) {
		t.Errorf("expected true for 0.05 < 0.20")
	}
	// 0.30 < 0.20 -> false
	if fi.ShouldInject(0) {
		t.Errorf("expected false for 0.30 < 0.20")
	}
}

func TestFaultInjector_ConcurrentAccess(t *testing.T) {
	fi := NewFaultInjector(0.25)
	var wg sync.WaitGroup
	workers := 16
	iters := 1000

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = fi.ShouldInject(0.25)
			}
		}()
	}

	wg.Wait()
}
