package telemetry

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// RouteSnapshot represents point-in-time statistics for a specific route.
type RouteSnapshot struct {
	RouteID       uint32 `json:"route_id"`
	RouteName     string `json:"route_name"`
	TotalRequests uint64 `json:"total_requests"`
	TotalBytesIn  uint64 `json:"total_bytes_in"`
	TotalBytesOut uint64 `json:"total_bytes_out"`
	Status2xx     uint32 `json:"status_2xx"`
	Status3xx     uint32 `json:"status_3xx"`
	Status4xx     uint32 `json:"status_4xx"`
	Status5xx     uint32 `json:"status_5xx"`
}

// routeStatsTracker maintains atomic counters for a single route.
type routeStatsTracker struct {
	name          string
	totalRequests atomic.Uint64
	totalBytesIn  atomic.Uint64
	totalBytesOut atomic.Uint64
	status2xx     atomic.Uint32
	status3xx     atomic.Uint32
	status4xx     atomic.Uint32
	status5xx     atomic.Uint32
}

func (t *routeStatsTracker) Record(event MetricEvent) {
	t.totalRequests.Add(1)
	t.totalBytesIn.Add(uint64(event.BytesIn))
	t.totalBytesOut.Add(uint64(event.BytesOut))

	code := event.StatusCode
	switch {
	case code >= 200 && code < 300:
		t.status2xx.Add(1)
	case code >= 300 && code < 400:
		t.status3xx.Add(1)
	case code >= 400 && code < 500:
		t.status4xx.Add(1)
	case code >= 500 && code < 600:
		t.status5xx.Add(1)
	}
}

func (t *routeStatsTracker) Snapshot(routeID uint32) RouteSnapshot {
	return RouteSnapshot{
		RouteID:       routeID,
		RouteName:     t.name,
		TotalRequests: t.totalRequests.Load(),
		TotalBytesIn:  t.totalBytesIn.Load(),
		TotalBytesOut: t.totalBytesOut.Load(),
		Status2xx:     t.status2xx.Load(),
		Status3xx:     t.status3xx.Load(),
		Status4xx:     t.status4xx.Load(),
		Status5xx:     t.status5xx.Load(),
	}
}

// GatewaySnapshot represents a unified, point-in-time snapshot of the gateway's telemetry.
type GatewaySnapshot struct {
	TimestampUnixNano int64   `json:"timestamp_unix_nano"`
	UptimeSeconds     float64 `json:"uptime_seconds"`
	TotalRequests     uint64  `json:"total_requests"`
	ActiveConnections int64   `json:"active_connections"`
	TotalBytesIn      uint64  `json:"total_bytes_in"`
	TotalBytesOut     uint64  `json:"total_bytes_out"`

	// Moving window rates
	RPS1s  float64 `json:"rps_1s"`
	RPS10s float64 `json:"rps_10s"`
	RPS60s float64 `json:"rps_60s"`

	// Status counts in rolling 60s window
	Status2xx uint32 `json:"status_2xx"`
	Status3xx uint32 `json:"status_3xx"`
	Status4xx uint32 `json:"status_4xx"`
	Status5xx uint32 `json:"status_5xx"`

	// Cumulative status counts
	Cumulative2xx uint64 `json:"cumulative_2xx"`
	Cumulative3xx uint64 `json:"cumulative_3xx"`
	Cumulative4xx uint64 `json:"cumulative_4xx"`
	Cumulative5xx uint64 `json:"cumulative_5xx"`

	// Latency percentiles in microseconds
	LatencyP50Us   int64  `json:"latency_p50_us"`
	LatencyP90Us   int64  `json:"latency_p90_us"`
	LatencyP99Us   int64  `json:"latency_p99_us"`
	LatencyP999Us  int64  `json:"latency_p999_us"`
	LatencyMinUs   int64  `json:"latency_min_us"`
	LatencyMaxUs   int64  `json:"latency_max_us"`
	LatencyMeanUs  int64  `json:"latency_mean_us"`
	LatencySamples uint64 `json:"latency_samples"`

	// Go runtime memory metrics
	AllocBytes      uint64 `json:"alloc_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	NumGC           uint32 `json:"num_gc"`
	Goroutines      int    `json:"goroutines"`

	// Ring buffer stats
	RingCapacity uint64 `json:"ring_capacity"`
	RingInflight uint64 `json:"ring_inflight"`
	RingDropped  uint64 `json:"ring_dropped"`

	// Per-route statistics
	Routes map[uint32]RouteSnapshot `json:"routes,omitempty"`
}

// ToBinaryFrame converts the GatewaySnapshot into a compact 64-byte BinaryFrame for IPC broadcast.
func (s *GatewaySnapshot) ToBinaryFrame() BinaryFrame {
	rps1sMilli := uint32(0)
	if s.RPS1s > 0 {
		milli := s.RPS1s * 1000.0
		if milli > float64(^uint32(0)) {
			rps1sMilli = ^uint32(0)
		} else {
			rps1sMilli = uint32(milli)
		}
	}

	active := uint32(0)
	if s.ActiveConnections > 0 {
		if s.ActiveConnections > int64(^uint32(0)) {
			active = ^uint32(0)
		} else {
			active = uint32(s.ActiveConnections)
		}
	}

	clampUint32 := func(v int64) uint32 {
		if v <= 0 {
			return 0
		}
		if v > int64(^uint32(0)) {
			return ^uint32(0)
		}
		return uint32(v)
	}

	p50 := clampUint32(s.LatencyP50Us)
	p90 := clampUint32(s.LatencyP90Us)
	p99 := clampUint32(s.LatencyP99Us)

	return BinaryFrame{
		Version:           CurrentVersion,
		FrameType:         FrameTypeSnapshot,
		TimestampUnixNano: s.TimestampUnixNano,
		TotalRequests:     s.TotalRequests,
		ActiveConns:       active,
		RPS1s:             rps1sMilli,
		P50LatencyUs:      p50,
		P90LatencyUs:      p90,
		P99LatencyUs:      p99,
		Status2xxCount:    s.Status2xx,
		Status3xxCount:    s.Status3xx,
		Status4xxCount:    s.Status4xx,
		Status5xxCount:    s.Status5xx,
	}
}

// SnapshotProvider aggregates data from ThroughputAggregator, LatencyHistogram,
// RingBuffer, and Go runtime stats into point-in-time GatewaySnapshots.
type SnapshotProvider struct {
	mu         sync.RWMutex
	startTime  time.Time
	aggregator *ThroughputAggregator
	histogram  *LatencyHistogram
	ringBuffer *RingBuffer
	routes     map[uint32]*routeStatsTracker
}

// NewSnapshotProvider creates an initialized SnapshotProvider.
func NewSnapshotProvider(agg *ThroughputAggregator, hist *LatencyHistogram, rb *RingBuffer) *SnapshotProvider {
	if agg == nil {
		agg = NewThroughputAggregator()
	}
	if hist == nil {
		hist = NewLatencyHistogram()
	}
	return &SnapshotProvider{
		startTime:  time.Now(),
		aggregator: agg,
		histogram:  hist,
		ringBuffer: rb,
		routes:     make(map[uint32]*routeStatsTracker),
	}
}

// SetStartTime overrides the start time (useful for testing uptime calculations).
func (p *SnapshotProvider) SetStartTime(t time.Time) {
	p.mu.Lock()
	p.startTime = t
	p.mu.Unlock()
}

// RegisterRoute associates a numeric RouteID with a human-readable name.
func (p *SnapshotProvider) RegisterRoute(routeID uint32, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.routes[routeID]; !exists {
		p.routes[routeID] = &routeStatsTracker{name: name}
	}
}

// RecordEvent records a complete MetricEvent into all telemetry subsystems:
// aggregator (rates and status), histogram (latency), ring buffer (event streaming), and route stats.
func (p *SnapshotProvider) RecordEvent(event MetricEvent, routeName string) {
	p.aggregator.Record(event.Timestamp, int(event.StatusCode), event.BytesIn, event.BytesOut)
	p.histogram.Record(event.LatencyNs)

	if p.ringBuffer != nil {
		p.ringBuffer.Push(event)
	}

	if event.RouteID != 0 {
		p.mu.RLock()
		tracker, exists := p.routes[event.RouteID]
		p.mu.RUnlock()

		if !exists && routeName != "" {
			p.mu.Lock()
			tracker, exists = p.routes[event.RouteID]
			if !exists {
				tracker = &routeStatsTracker{name: routeName}
				p.routes[event.RouteID] = tracker
			}
			p.mu.Unlock()
		}

		if tracker != nil {
			tracker.Record(event)
		}
	}
}

// TakeSnapshot captures an atomic point-in-time GatewaySnapshot.
func (p *SnapshotProvider) TakeSnapshot() GatewaySnapshot {
	now := time.Now()
	nowUnixNano := now.UnixNano()
	nowSec := now.Unix()

	p.mu.RLock()
	uptime := now.Sub(p.startTime).Seconds()
	if uptime < 0 {
		uptime = 0
	}

	// Copy route stats
	routeMap := make(map[uint32]RouteSnapshot, len(p.routes))
	for id, tracker := range p.routes {
		routeMap[id] = tracker.Snapshot(id)
	}
	p.mu.RUnlock()

	rps1s, rps10s, rps60s := p.aggregator.RatesAt(nowSec)
	c2xx, c3xx, c4xx, c5xx := p.aggregator.RollingStatusCounts(nowSec)
	totReqs, bytesIn, bytesOut, tot2xx, tot3xx, tot4xx, tot5xx := p.aggregator.CumulativeTotals()
	activeConns := p.aggregator.ActiveConns()
	percentiles := p.histogram.Percentiles()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	var ringCap, ringInflight, ringDropped uint64
	if p.ringBuffer != nil {
		ringCap = uint64(p.ringBuffer.Cap())
		ringInflight = uint64(p.ringBuffer.Len())
		ringDropped = p.ringBuffer.Dropped()
	}

	return GatewaySnapshot{
		TimestampUnixNano: nowUnixNano,
		UptimeSeconds:     uptime,
		TotalRequests:     totReqs,
		ActiveConnections: activeConns,
		TotalBytesIn:      bytesIn,
		TotalBytesOut:     bytesOut,

		RPS1s:  rps1s,
		RPS10s: rps10s,
		RPS60s: rps60s,

		Status2xx: c2xx,
		Status3xx: c3xx,
		Status4xx: c4xx,
		Status5xx: c5xx,

		Cumulative2xx: tot2xx,
		Cumulative3xx: tot3xx,
		Cumulative4xx: tot4xx,
		Cumulative5xx: tot5xx,

		LatencyP50Us:   percentiles.P50,
		LatencyP90Us:   percentiles.P90,
		LatencyP99Us:   percentiles.P99,
		LatencyP999Us:  percentiles.P999,
		LatencyMinUs:   percentiles.Min,
		LatencyMaxUs:   percentiles.Max,
		LatencyMeanUs:  percentiles.Mean,
		LatencySamples: percentiles.Count,

		AllocBytes:      memStats.Alloc,
		TotalAllocBytes: memStats.TotalAlloc,
		SysBytes:        memStats.Sys,
		NumGC:           memStats.NumGC,
		Goroutines:      runtime.NumGoroutine(),

		RingCapacity: ringCap,
		RingInflight: ringInflight,
		RingDropped:  ringDropped,

		Routes: routeMap,
	}
}
