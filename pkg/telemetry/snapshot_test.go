package telemetry

import (
	"sync"
	"testing"
	"time"
)

func TestSnapshot_BasicAndUptime(t *testing.T) {
	agg := NewThroughputAggregator()
	hist := NewLatencyHistogram()
	rb := NewRingBuffer(1024)

	provider := NewSnapshotProvider(agg, hist, rb)
	provider.SetStartTime(time.Now().Add(-10 * time.Second))

	provider.RegisterRoute(101, "api-v1")

	// Emit events
	now := time.Now().UnixNano()
	provider.RecordEvent(MetricEvent{
		Timestamp:  now,
		LatencyNs:  500 * 1000, // 500us
		BytesIn:    128,
		BytesOut:   1024,
		RouteID:    101,
		StatusCode: 200,
	}, "api-v1")

	provider.RecordEvent(MetricEvent{
		Timestamp:  now,
		LatencyNs:  2 * 1000 * 1000, // 2ms
		BytesIn:    256,
		BytesOut:   512,
		RouteID:    101,
		StatusCode: 404,
	}, "api-v1")

	provider.RecordEvent(MetricEvent{
		Timestamp:  now,
		LatencyNs:  50 * 1000 * 1000, // 50ms
		BytesIn:    64,
		BytesOut:   128,
		RouteID:    202,
		StatusCode: 502,
	}, "api-v2")

	snap := provider.TakeSnapshot()

	if snap.UptimeSeconds < 9.5 || snap.UptimeSeconds > 15.0 {
		t.Fatalf("unexpected uptime: %f", snap.UptimeSeconds)
	}

	if snap.TotalRequests != 3 {
		t.Fatalf("expected 3 requests, got %d", snap.TotalRequests)
	}

	if snap.TotalBytesIn != 128+256+64 {
		t.Fatalf("expected %d bytes in, got %d", 128+256+64, snap.TotalBytesIn)
	}

	if snap.TotalBytesOut != 1024+512+128 {
		t.Fatalf("expected %d bytes out, got %d", 1024+512+128, snap.TotalBytesOut)
	}

	if snap.Status2xx != 1 || snap.Status4xx != 1 || snap.Status5xx != 1 {
		t.Fatalf("unexpected status counts: 2xx=%d, 4xx=%d, 5xx=%d", snap.Status2xx, snap.Status4xx, snap.Status5xx)
	}

	if snap.LatencySamples != 3 {
		t.Fatalf("expected 3 latency samples, got %d", snap.LatencySamples)
	}

	if snap.RingCapacity != 1024 {
		t.Fatalf("expected ring capacity 1024, got %d", snap.RingCapacity)
	}

	if snap.AllocBytes == 0 || snap.Goroutines == 0 {
		t.Fatalf("expected runtime stats to be populated: alloc=%d goroutines=%d", snap.AllocBytes, snap.Goroutines)
	}

	// Verify route stats
	r101, exists := snap.Routes[101]
	if !exists || r101.TotalRequests != 2 || r101.RouteName != "api-v1" {
		t.Fatalf("route 101 mismatch: %+v", r101)
	}

	r202, exists := snap.Routes[202]
	if !exists || r202.TotalRequests != 1 || r202.RouteName != "api-v2" {
		t.Fatalf("route 202 mismatch: %+v", r202)
	}
}

func TestSnapshot_ToBinaryFrame(t *testing.T) {
	agg := NewThroughputAggregator()
	hist := NewLatencyHistogram()
	provider := NewSnapshotProvider(agg, hist, nil)

	agg.IncActiveConns()
	agg.IncActiveConns()

	now := time.Now().UnixNano()
	provider.RecordEvent(MetricEvent{
		Timestamp:  now,
		LatencyNs:  1500 * 1000, // 1.5ms
		StatusCode: 200,
	}, "test-route")

	snap := provider.TakeSnapshot()
	frame := snap.ToBinaryFrame()

	if frame.Version != CurrentVersion {
		t.Fatalf("expected version %d, got %d", CurrentVersion, frame.Version)
	}
	if frame.FrameType != FrameTypeSnapshot {
		t.Fatalf("expected frame type %d, got %d", FrameTypeSnapshot, frame.FrameType)
	}
	if frame.TotalRequests != 1 {
		t.Fatalf("expected 1 total request, got %d", frame.TotalRequests)
	}
	if frame.ActiveConns != 2 {
		t.Fatalf("expected 2 active connections, got %d", frame.ActiveConns)
	}

	var buf [BinaryFrameSize]byte
	if err := frame.Encode(buf[:]); err != nil {
		t.Fatalf("encode error: %v", err)
	}

	var decoded BinaryFrame
	if err := decoded.Decode(buf[:]); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if decoded.TotalRequests != frame.TotalRequests || decoded.ActiveConns != frame.ActiveConns {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", decoded, frame)
	}
}

func TestSnapshot_ConcurrentMetricUpdatesConsistency(t *testing.T) {
	agg := NewThroughputAggregator()
	hist := NewLatencyHistogram()
	rb := NewRingBuffer(65536)
	provider := NewSnapshotProvider(agg, hist, rb)

	workers := 8
	requestsPerWorker := 1000

	var wg sync.WaitGroup
	stopSnapshotter := make(chan struct{})

	// Background snapshotter goroutine taking concurrent snapshots
	go func() {
		for {
			select {
			case <-stopSnapshotter:
				return
			default:
				snap := provider.TakeSnapshot()
				_ = snap.ToBinaryFrame()
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			baseRoute := uint32(workerID + 1)
			routeName := "worker-route"
			now := time.Now().UnixNano()

			for i := 0; i < requestsPerWorker; i++ {
				status := 200
				if i%10 == 0 {
					status = 500
				} else if i%5 == 0 {
					status = 404
				}

				provider.RecordEvent(MetricEvent{
					Timestamp:  now,
					LatencyNs:  int64(100+i) * 1000,
					BytesIn:    uint32(64 + i%100),
					BytesOut:   uint32(256 + i%200),
					RouteID:    baseRoute,
					StatusCode: uint16(status),
				}, routeName)
			}
		}(w)
	}

	wg.Wait()
	close(stopSnapshotter)

	finalSnap := provider.TakeSnapshot()
	expectedTotal := uint64(workers * requestsPerWorker)

	if finalSnap.TotalRequests != expectedTotal {
		t.Fatalf("expected total requests %d, got %d", expectedTotal, finalSnap.TotalRequests)
	}

	totalFromStatus := finalSnap.Cumulative2xx + finalSnap.Cumulative4xx + finalSnap.Cumulative5xx
	if totalFromStatus != expectedTotal {
		t.Fatalf("expected status sum %d, got %d", expectedTotal, totalFromStatus)
	}

	if finalSnap.LatencySamples != expectedTotal {
		t.Fatalf("expected latency samples %d, got %d", expectedTotal, finalSnap.LatencySamples)
	}

	var routeTotal uint64
	for _, r := range finalSnap.Routes {
		routeTotal += r.TotalRequests
	}
	if routeTotal != expectedTotal {
		t.Fatalf("expected route total %d, got %d", expectedTotal, routeTotal)
	}
}
