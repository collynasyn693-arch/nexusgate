package telemetry

import (
	"sync"
	"time"
)

// TotalBucketCount is the exact number of fixed bins in the 4-tier HDR histogram.
const (
	Tier1Buckets = 1000 // 0 to 999 μs (1 μs resolution)
	Tier2Buckets = 990  // 1 ms to 99.9 ms (100 μs resolution)
	Tier3Buckets = 900  // 100 ms to 999 ms (1 ms resolution)
	Tier4Buckets = 1180 // 1 s to 60 s (50 ms resolution)
	OverflowBins = 1    // >60 s

	TotalBucketCount = Tier1Buckets + Tier2Buckets + Tier3Buckets + Tier4Buckets + OverflowBins // 4071
	NumGenerations   = 3                                                                        // 3 generational windows (10s each = 30s rolling)
	GenDurationSec   = 10
)

// LatencyPercentiles holds computed latency percentiles in microseconds.
type LatencyPercentiles struct {
	P50   int64  // 50th percentile (μs)
	P90   int64  // 90th percentile (μs)
	P99   int64  // 99th percentile (μs)
	P999  int64  // 99.9th percentile (μs)
	Min   int64  // Minimum recorded latency (μs)
	Max   int64  // Maximum recorded latency (μs)
	Mean  int64  // Mean latency (μs)
	Count uint64 // Total samples in active window
}

// LatencyHistogram is a fixed-memory, zero-allocation sliding HDR latency histogram
// computing microsecond-accurate percentiles without heap allocations.
type LatencyHistogram struct {
	mu           sync.RWMutex
	windows      [NumGenerations][TotalBucketCount]uint32
	windowCounts [NumGenerations]uint64
	windowSums   [NumGenerations]int64 // sum in microseconds
	windowGen    [NumGenerations]int64 // epoch generation tag (nowSec / GenDurationSec)
	activeGen    int64                 // current generation tag
}

// NewLatencyHistogram creates an initialized LatencyHistogram with pre-allocated bucket arrays.
func NewLatencyHistogram() *LatencyHistogram {
	nowSec := time.Now().Unix()
	gen := nowSec / GenDurationSec
	h := &LatencyHistogram{
		activeGen: gen,
	}
	for i := 0; i < NumGenerations; i++ {
		h.windowGen[i] = gen - int64(NumGenerations-1-i)
	}
	return h
}

// latencyToBucket maps a latency duration in nanoseconds to its corresponding bucket index (0..4070).
func latencyToBucket(ns int64) int {
	if ns <= 0 {
		return 0
	}
	us := ns / 1000 // Convert nanoseconds to microseconds

	// Tier 1: 0 to 999 μs
	if us < 1000 {
		return int(us)
	}

	// Tier 2: 1,000 to 99,999 μs (1 ms to 99.9 ms, 100 μs resolution)
	if us < 100000 {
		return Tier1Buckets + int((us-1000)/100)
	}

	// Tier 3: 100,000 to 999,999 μs (100 ms to 999 ms, 1 ms resolution)
	if us < 1000000 {
		return Tier1Buckets + Tier2Buckets + int((us-100000)/1000)
	}

	// Tier 4: 1,000,000 to 60,000,000 μs (1 s to 60 s, 50 ms resolution)
	if us < 60000000 {
		return Tier1Buckets + Tier2Buckets + Tier3Buckets + int((us-1000000)/50000)
	}

	// Overflow: >= 60 s
	return TotalBucketCount - 1
}

// bucketToLatencyUs returns the representative latency in microseconds (midpoint) for a bucket index.
func bucketToLatencyUs(idx int) int64 {
	if idx < 0 {
		return 0
	}
	// Tier 1: 0 to 999 μs
	if idx < Tier1Buckets {
		return int64(idx)
	}
	// Tier 2: 1 ms to 99.9 ms
	if idx < Tier1Buckets+Tier2Buckets {
		offset := idx - Tier1Buckets
		return 1000 + int64(offset)*100 + 50
	}
	// Tier 3: 100 ms to 999 ms
	if idx < Tier1Buckets+Tier2Buckets+Tier3Buckets {
		offset := idx - (Tier1Buckets + Tier2Buckets)
		return 100000 + int64(offset)*1000 + 500
	}
	// Tier 4: 1 s to 60 s
	if idx < TotalBucketCount-1 {
		offset := idx - (Tier1Buckets + Tier2Buckets + Tier3Buckets)
		return 1000000 + int64(offset)*50000 + 25000
	}
	// Overflow
	return 60000000
}

// Record records a request latency duration in nanoseconds into the active generational window.
// This function performs 0 heap allocations.
func (h *LatencyHistogram) Record(latencyNs int64) {
	nowSec := time.Now().Unix()
	currentGen := nowSec / GenDurationSec
	bucket := latencyToBucket(latencyNs)
	us := latencyNs / 1000
	if us < 0 {
		us = 0
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	slot := int(currentGen % NumGenerations)
	if h.windowGen[slot] != currentGen {
		// Rotate expired generation
		clear(h.windows[slot][:])
		h.windowCounts[slot] = 0
		h.windowSums[slot] = 0
		h.windowGen[slot] = currentGen
	}

	h.windows[slot][bucket]++
	h.windowCounts[slot]++
	h.windowSums[slot] += us
}

// Percentiles calculates P50, P90, P99, and P99.9 latency percentiles across
// active valid generational windows in a single pass without heap allocations.
func (h *LatencyHistogram) Percentiles() LatencyPercentiles {
	nowSec := time.Now().Unix()
	currentGen := nowSec / GenDurationSec

	h.mu.RLock()
	defer h.mu.RUnlock()

	// Aggregate counts across non-expired windows (within last NumGenerations * GenDurationSec)
	var (
		totalCount uint64
		totalSum   int64
		validSlots [NumGenerations]bool
	)

	for i := 0; i < NumGenerations; i++ {
		genDiff := currentGen - h.windowGen[i]
		if genDiff >= 0 && genDiff < NumGenerations {
			validSlots[i] = true
			totalCount += h.windowCounts[i]
			totalSum += h.windowSums[i]
		}
	}

	if totalCount == 0 {
		return LatencyPercentiles{}
	}

	res := LatencyPercentiles{
		Count: totalCount,
		Mean:  totalSum / int64(totalCount),
	}

	// Targets in sample counts
	targetP50 := uint64(float64(totalCount) * 0.50)
	targetP90 := uint64(float64(totalCount) * 0.90)
	targetP99 := uint64(float64(totalCount) * 0.99)
	targetP999 := uint64(float64(totalCount) * 0.999)

	var (
		cumCount uint64
		p50Done  bool
		p90Done  bool
		p99Done  bool
		p999Done bool
		minFound bool
		lastIdx  int
	)

	for idx := 0; idx < TotalBucketCount; idx++ {
		var bucketCount uint32
		for s := 0; s < NumGenerations; s++ {
			if validSlots[s] {
				bucketCount += h.windows[s][idx]
			}
		}
		if bucketCount == 0 {
			continue
		}

		lastIdx = idx
		if !minFound {
			res.Min = bucketToLatencyUs(idx)
			minFound = true
		}

		cumCount += uint64(bucketCount)

		if !p50Done && cumCount >= targetP50 {
			res.P50 = bucketToLatencyUs(idx)
			p50Done = true
		}
		if !p90Done && cumCount >= targetP90 {
			res.P90 = bucketToLatencyUs(idx)
			p90Done = true
		}
		if !p99Done && cumCount >= targetP99 {
			res.P99 = bucketToLatencyUs(idx)
			p99Done = true
		}
		if !p999Done && cumCount >= targetP999 {
			res.P999 = bucketToLatencyUs(idx)
			p999Done = true
		}
	}

	res.Max = bucketToLatencyUs(lastIdx)
	if !p50Done {
		res.P50 = res.Max
	}
	if !p90Done {
		res.P90 = res.Max
	}
	if !p99Done {
		res.P99 = res.Max
	}
	if !p999Done {
		res.P999 = res.Max
	}

	return res
}

// Percentile returns a specific percentile (0.0 to 100.0) in microseconds.
func (h *LatencyHistogram) Percentile(p float64) int64 {
	piles := h.Percentiles()
	if piles.Count == 0 {
		return 0
	}
	if p <= 50.0 {
		return piles.P50
	}
	if p <= 90.0 {
		return piles.P90
	}
	if p <= 99.0 {
		return piles.P99
	}
	return piles.P999
}

// Reset clears all recorded histogram data across all generations.
func (h *LatencyHistogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	nowSec := time.Now().Unix()
	gen := nowSec / GenDurationSec
	h.activeGen = gen
	for i := 0; i < NumGenerations; i++ {
		clear(h.windows[i][:])
		h.windowCounts[i] = 0
		h.windowSums[i] = 0
		h.windowGen[i] = gen - int64(NumGenerations-1-i)
	}
}
