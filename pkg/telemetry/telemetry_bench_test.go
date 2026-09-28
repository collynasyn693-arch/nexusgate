package telemetry

import (
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkRingBuffer_PushSequential(b *testing.B) {
	rb := NewRingBuffer(65536)
	event := MetricEvent{
		Timestamp:  time.Now().UnixNano(),
		LatencyNs:  500 * 1000,
		BytesIn:    128,
		BytesOut:   1024,
		RouteID:    1,
		StatusCode: 200,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !rb.Push(event) {
			_, _ = rb.Pop()
			rb.Push(event)
		}
	}
}

func BenchmarkRingBuffer_PushParallel(b *testing.B) {
	rb := NewRingBuffer(65536)
	event := MetricEvent{
		Timestamp:  time.Now().UnixNano(),
		LatencyNs:  250 * 1000,
		BytesIn:    64,
		BytesOut:   512,
		RouteID:    42,
		StatusCode: 200,
	}

	var stopDrain atomic.Bool
	go func() {
		batch := make([]MetricEvent, 128)
		for !stopDrain.Load() {
			n := rb.BatchPop(batch)
			if n == 0 {
				time.Sleep(10 * time.Microsecond)
			}
		}
	}()
	defer stopDrain.Store(true)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rb.Push(event)
		}
	})
}

func BenchmarkHistogram_RecordSequential(b *testing.B) {
	h := NewLatencyHistogram()
	latency := int64(1500 * 1000) // 1.5ms

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		h.Record(latency)
	}
}

func BenchmarkHistogram_RecordParallel(b *testing.B) {
	h := NewLatencyHistogram()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		lat := int64(500 * 1000)
		for pb.Next() {
			h.Record(lat)
		}
	})
}

func BenchmarkHistogram_Percentiles(b *testing.B) {
	h := NewLatencyHistogram()
	for i := 0; i < 10000; i++ {
		h.Record(int64((i % 50000) * 1000))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = h.Percentiles()
	}
}

func BenchmarkThroughputAggregator_RecordParallel(b *testing.B) {
	agg := NewThroughputAggregator()
	now := time.Now().UnixNano()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			agg.Record(now, 200, 128, 512)
		}
	})
}

func BenchmarkBinaryFrame_EncodeDecode(b *testing.B) {
	frame := BinaryFrame{
		Version:           CurrentVersion,
		FrameType:         FrameTypeSnapshot,
		TimestampUnixNano: time.Now().UnixNano(),
		TotalRequests:     100000,
		ActiveConns:       50,
		RPS1s:             5000000,
		P50LatencyUs:      850,
		P90LatencyUs:      2200,
		P99LatencyUs:      9500,
		Status2xxCount:    9500,
		Status3xxCount:    200,
		Status4xxCount:    250,
		Status5xxCount:    50,
	}

	var buf [BinaryFrameSize]byte
	var decoded BinaryFrame

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := frame.Encode(buf[:]); err != nil {
			b.Fatal(err)
		}
		if err := decoded.Decode(buf[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshotProvider_RecordEventParallel(b *testing.B) {
	agg := NewThroughputAggregator()
	hist := NewLatencyHistogram()
	rb := NewRingBuffer(65536)
	provider := NewSnapshotProvider(agg, hist, rb)
	provider.RegisterRoute(1, "bench-route")

	var stopDrain atomic.Bool
	go func() {
		batch := make([]MetricEvent, 128)
		for !stopDrain.Load() {
			n := rb.BatchPop(batch)
			if n == 0 {
				time.Sleep(10 * time.Microsecond)
			}
		}
	}()
	defer stopDrain.Store(true)

	event := MetricEvent{
		Timestamp:  time.Now().UnixNano(),
		LatencyNs:  500 * 1000,
		BytesIn:    128,
		BytesOut:   1024,
		RouteID:    1,
		StatusCode: 200,
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			provider.RecordEvent(event, "bench-route")
		}
	})
}
