package telemetry

import (
	"sync"
	"sync/atomic"
	"time"
)

const RollingWindowSeconds = 60

// secondBucket tracks metrics accumulated within a single 1-second epoch slice.
type secondBucket struct {
	epochSec  int64
	requests  uint64
	bytesIn   uint64
	bytesOut  uint64
	status2xx uint32
	status3xx uint32
	status4xx uint32
	status5xx uint32
}

// Reset clears all counters in the bucket and updates its epoch second.
func (b *secondBucket) Reset(epochSec int64) {
	b.epochSec = epochSec
	b.requests = 0
	b.bytesIn = 0
	b.bytesOut = 0
	b.status2xx = 0
	b.status3xx = 0
	b.status4xx = 0
	b.status5xx = 0
}

// ThroughputAggregator tracks rolling throughput and HTTP status code counters
// over 1s, 10s, and 60s moving windows with passive mathematical decay to 0.0 RPS.
type ThroughputAggregator struct {
	mu            sync.RWMutex
	buckets       [RollingWindowSeconds]secondBucket
	activeConns   atomic.Int64
	totalRequests atomic.Uint64
	totalBytesIn  atomic.Uint64
	totalBytesOut atomic.Uint64

	// Cumulative status counters
	total2xx atomic.Uint64
	total3xx atomic.Uint64
	total4xx atomic.Uint64
	total5xx atomic.Uint64
}

// NewThroughputAggregator creates and initializes a ThroughputAggregator.
func NewThroughputAggregator() *ThroughputAggregator {
	nowSec := time.Now().Unix()
	agg := &ThroughputAggregator{}
	for i := 0; i < RollingWindowSeconds; i++ {
		agg.buckets[i].epochSec = nowSec - int64(RollingWindowSeconds-i)
	}
	return agg
}

// IncActiveConns atomically increments the active connections counter.
func (a *ThroughputAggregator) IncActiveConns() int64 {
	return a.activeConns.Add(1)
}

// DecActiveConns atomically decrements the active connections counter.
func (a *ThroughputAggregator) DecActiveConns() int64 {
	return a.activeConns.Add(-1)
}

// ActiveConns returns the current number of active connections.
func (a *ThroughputAggregator) ActiveConns() int64 {
	val := a.activeConns.Load()
	if val < 0 {
		return 0
	}
	return val
}

// Record records a request completion event into the rolling window and cumulative counters.
func (a *ThroughputAggregator) Record(timestampNs int64, statusCode int, bytesIn, bytesOut uint32) {
	var sec int64
	if timestampNs > 0 {
		sec = timestampNs / 1e9
	} else {
		sec = time.Now().Unix()
	}

	a.totalRequests.Add(1)
	a.totalBytesIn.Add(uint64(bytesIn))
	a.totalBytesOut.Add(uint64(bytesOut))

	// Update cumulative status counters
	switch {
	case statusCode >= 200 && statusCode < 300:
		a.total2xx.Add(1)
	case statusCode >= 300 && statusCode < 400:
		a.total3xx.Add(1)
	case statusCode >= 400 && statusCode < 500:
		a.total4xx.Add(1)
	case statusCode >= 500 && statusCode < 600:
		a.total5xx.Add(1)
	}

	idx := sec % RollingWindowSeconds
	if idx < 0 {
		idx = 0
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	b := &a.buckets[idx]
	if b.epochSec != sec {
		b.Reset(sec)
	}

	b.requests++
	b.bytesIn += uint64(bytesIn)
	b.bytesOut += uint64(bytesOut)

	switch {
	case statusCode >= 200 && statusCode < 300:
		b.status2xx++
	case statusCode >= 300 && statusCode < 400:
		b.status3xx++
	case statusCode >= 400 && statusCode < 500:
		b.status4xx++
	case statusCode >= 500 && statusCode < 600:
		b.status5xx++
	}
}

// Rates calculates the 1s, 10s, and 60s moving window RPS.
// Stale buckets past their window are naturally decayed to zero without requiring background tickers.
func (a *ThroughputAggregator) Rates() (rps1s, rps10s, rps60s float64) {
	return a.RatesAt(time.Now().Unix())
}

// RatesAt computes rolling RPS relative to a specific epoch second (useful for deterministic testing).
func (a *ThroughputAggregator) RatesAt(nowSec int64) (rps1s, rps10s, rps60s float64) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var (
		reqs1s  uint64
		reqs10s uint64
		reqs60s uint64
	)

	for i := 0; i < RollingWindowSeconds; i++ {
		b := &a.buckets[i]
		diff := nowSec - b.epochSec
		if diff < 0 || diff >= RollingWindowSeconds {
			continue // Stale bucket outside the 60s window
		}

		reqs60s += b.requests
		if diff < 10 {
			reqs10s += b.requests
		}
		if diff < 1 {
			reqs1s += b.requests
		}
	}

	rps1s = float64(reqs1s)
	rps10s = float64(reqs10s) / 10.0
	rps60s = float64(reqs60s) / 60.0
	return rps1s, rps10s, rps60s
}

// RollingStatusCounts returns the sum of 2xx, 3xx, 4xx, 5xx within the active 60-second rolling window.
func (a *ThroughputAggregator) RollingStatusCounts(nowSec int64) (c2xx, c3xx, c4xx, c5xx uint32) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for i := 0; i < RollingWindowSeconds; i++ {
		b := &a.buckets[i]
		diff := nowSec - b.epochSec
		if diff >= 0 && diff < RollingWindowSeconds {
			c2xx += b.status2xx
			c3xx += b.status3xx
			c4xx += b.status4xx
			c5xx += b.status5xx
		}
	}
	return c2xx, c3xx, c4xx, c5xx
}

// CumulativeTotals returns total requests, bytes in, bytes out, and cumulative status counts.
func (a *ThroughputAggregator) CumulativeTotals() (requests, bytesIn, bytesOut, c2xx, c3xx, c4xx, c5xx uint64) {
	return a.totalRequests.Load(),
		a.totalBytesIn.Load(),
		a.totalBytesOut.Load(),
		a.total2xx.Load(),
		a.total3xx.Load(),
		a.total4xx.Load(),
		a.total5xx.Load()
}

// Reset clears all rolling buckets and cumulative counters.
func (a *ThroughputAggregator) Reset() {
	nowSec := time.Now().Unix()
	a.mu.Lock()
	defer a.mu.Unlock()

	for i := 0; i < RollingWindowSeconds; i++ {
		a.buckets[i].Reset(nowSec - int64(RollingWindowSeconds-i))
	}
	a.activeConns.Store(0)
	a.totalRequests.Store(0)
	a.totalBytesIn.Store(0)
	a.totalBytesOut.Store(0)
	a.total2xx.Store(0)
	a.total3xx.Store(0)
	a.total4xx.Store(0)
	a.total5xx.Store(0)
}
