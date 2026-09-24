package telemetry

import (
	"math"
	"testing"
)

func TestHistogram_BucketMappingTiers(t *testing.T) {
	tests := []struct {
		name       string
		latencyNs  int64
		expectedUs int64
		maxDiffUs  int64
	}{
		{"SubMicrosecond", 500, 0, 1},
		{"Tier1_Low", 25000, 25, 1},        // 25 μs
		{"Tier1_High", 950000, 950, 1},     // 950 μs
		{"Tier2_Low", 1500000, 1500, 100},  // 1.5 ms
		{"Tier2_High", 50000000, 50000, 100}, // 50 ms
		{"Tier3_Low", 150000000, 150000, 1000}, // 150 ms
		{"Tier3_High", 800000000, 800000, 1000}, // 800 ms
		{"Tier4_Low", 2000000000, 2000000, 50000}, // 2 s
		{"Tier4_High", 45000000000, 45000000, 50000}, // 45 s
		{"Overflow", 75000000000, 60000000, 50000}, // 75 s -> 60s bucket
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := latencyToBucket(tc.latencyNs)
			if b < 0 || b >= TotalBucketCount {
				t.Fatalf("bucket out of bounds: %d", b)
			}
			mappedUs := bucketToLatencyUs(b)
			diff := int64(math.Abs(float64(mappedUs - tc.expectedUs)))
			if diff > tc.maxDiffUs {
				t.Errorf("bucket mapping error too high for %s: got %d μs, want ~%d μs (diff %d > max %d)",
					tc.name, mappedUs, tc.expectedUs, diff, tc.maxDiffUs)
			}
		})
	}
}

func TestHistogram_PercentileAccuracyKnownDistribution(t *testing.T) {
	h := NewLatencyHistogram()

	// Inject 10,000 samples linearly distributed from 1 μs to 1,000 μs
	// (1,000 ns to 1,000,000 ns) where resolution is 1 μs.
	const samples = 10000
	for i := 1; i <= samples; i++ {
		// latency in nanoseconds: i * 100 ns
		latencyNs := int64(i) * 100 // 100ns to 1,000,000ns (1ms)
		h.Record(latencyNs)
	}

	piles := h.Percentiles()
	if piles.Count != samples {
		t.Fatalf("expected count %d, got %d", samples, piles.Count)
	}

	// Theoretical values for uniform distribution [0.1 μs .. 1000 μs]:
	// P50: ~500 μs
	// P90: ~900 μs
	// P99: ~990 μs
	// P99.9: ~999 μs
	checkPercentile := func(name string, actual, expected int64, tolerancePercent float64) {
		diff := math.Abs(float64(actual - expected))
		maxDiff := float64(expected) * (tolerancePercent / 100.0)
		if diff > maxDiff {
			t.Errorf("%s out of tolerance: got %d μs, expected %d μs (diff %.2f > max %.2f)",
				name, actual, expected, diff, maxDiff)
		}
	}

	checkPercentile("P50", piles.P50, 500, 2.0)
	checkPercentile("P90", piles.P90, 900, 2.0)
	checkPercentile("P99", piles.P99, 990, 2.0)
	checkPercentile("P999", piles.P999, 999, 2.0)

	// Test single percentile accessor
	if p50 := h.Percentile(50.0); p50 != piles.P50 {
		t.Errorf("Percentile(50.0) mismatch: %d != %d", p50, piles.P50)
	}
}

func TestHistogram_ZeroAllocations(t *testing.T) {
	h := NewLatencyHistogram()
	// Warm up
	h.Record(500000)

	allocs := testing.AllocsPerRun(1000, func() {
		h.Record(1250000) // 1.25 ms
	})

	if allocs != 0.0 {
		t.Fatalf("expected 0 allocs per Record, got %.2f", allocs)
	}
}

func TestHistogram_EmptyAndReset(t *testing.T) {
	h := NewLatencyHistogram()
	piles := h.Percentiles()
	if piles.Count != 0 || piles.P50 != 0 {
		t.Fatalf("expected zeroed percentiles on empty histogram, got %+v", piles)
	}

	h.Record(10000000) // 10ms
	if h.Percentiles().Count != 1 {
		t.Fatalf("expected count 1")
	}

	h.Reset()
	if h.Percentiles().Count != 0 {
		t.Fatalf("expected count 0 after reset")
	}
}
