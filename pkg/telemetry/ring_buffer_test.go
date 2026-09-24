package telemetry

import (
	"testing"
	"unsafe"
)

func TestRingSlot_SizeAndAlignment(t *testing.T) {
	var s ringSlot
	if sz := unsafe.Sizeof(s); sz != 64 {
		t.Fatalf("expected ringSlot size to be strictly 64 bytes (1 cache line), got %d", sz)
	}
	if al := unsafe.Alignof(s); al != 8 {
		t.Fatalf("expected ringSlot alignment to be 8 bytes, got %d", al)
	}
}

func TestRingBuffer_BasicPushPop(t *testing.T) {
	rb := NewRingBuffer(16)
	if rb.Cap() != 16 {
		t.Fatalf("expected capacity 16, got %d", rb.Cap())
	}
	if !rb.IsEmpty() {
		t.Fatalf("expected empty buffer initially")
	}

	event := MetricEvent{
		Timestamp:  1000,
		LatencyNs:  25000,
		BytesIn:    128,
		BytesOut:   512,
		RouteID:    42,
		StatusCode: 200,
		Flags:      FlagSuccess,
	}

	if ok := rb.Push(event); !ok {
		t.Fatalf("failed to push event")
	}

	if rb.IsEmpty() {
		t.Fatalf("expected buffer to not be empty")
	}
	if rb.Len() != 1 {
		t.Fatalf("expected Len 1, got %d", rb.Len())
	}

	popped, ok := rb.Pop()
	if !ok {
		t.Fatalf("failed to pop event")
	}
	if popped != event {
		t.Fatalf("popped event does not match pushed event: %+v != %+v", popped, event)
	}
	if !rb.IsEmpty() {
		t.Fatalf("expected empty buffer after popping only event")
	}

	// Pop from empty
	_, ok = rb.Pop()
	if ok {
		t.Fatalf("expected Pop on empty buffer to return false")
	}
}

func TestRingBuffer_FullAndDropBehavior(t *testing.T) {
	const capSize = 8
	rb := NewRingBuffer(capSize)

	for i := 0; i < capSize; i++ {
		ok := rb.Push(MetricEvent{
			Timestamp:  int64(i),
			StatusCode: 200,
		})
		if !ok {
			t.Fatalf("unexpected push failure at index %d", i)
		}
	}

	if rb.Len() != capSize {
		t.Fatalf("expected Len %d, got %d", capSize, rb.Len())
	}

	// Buffer is now full; subsequent push must fail and record drop
	extraEvent := MetricEvent{Timestamp: 999, StatusCode: 500}
	if ok := rb.Push(extraEvent); ok {
		t.Fatalf("expected push to full buffer to return false")
	}
	if dropped := rb.Dropped(); dropped != 1 {
		t.Fatalf("expected dropped count 1, got %d", dropped)
	}

	// PushOverwrite behavior
	if ok := rb.PushOverwrite(extraEvent); ok {
		t.Fatalf("expected push overwrite on full buffer to return false")
	}
	if dropped := rb.Dropped(); dropped != 2 {
		t.Fatalf("expected dropped count 2, got %d", dropped)
	}

	// Drain 1 item
	item, ok := rb.Pop()
	if !ok || item.Timestamp != 0 {
		t.Fatalf("unexpected pop result: ok=%v, item=%+v", ok, item)
	}

	// Now 1 slot is available; push should succeed
	if ok := rb.Push(extraEvent); !ok {
		t.Fatalf("expected push to succeed after pop")
	}
}

func TestRingBuffer_BatchPop(t *testing.T) {
	rb := NewRingBuffer(32)

	for i := 0; i < 10; i++ {
		rb.Push(MetricEvent{Timestamp: int64(i + 1), StatusCode: uint16(200 + i)})
	}

	dest := make([]MetricEvent, 4)
	n := rb.BatchPop(dest)
	if n != 4 {
		t.Fatalf("expected batch pop count 4, got %d", n)
	}
	for i := 0; i < 4; i++ {
		if dest[i].Timestamp != int64(i+1) {
			t.Errorf("batch item %d timestamp mismatch: got %d, want %d", i, dest[i].Timestamp, i+1)
		}
	}

	// Pop remaining
	destLong := make([]MetricEvent, 10)
	n = rb.BatchPop(destLong)
	if n != 6 {
		t.Fatalf("expected remaining batch pop count 6, got %d", n)
	}

	// Batch pop on empty
	n = rb.BatchPop(destLong)
	if n != 0 {
		t.Fatalf("expected 0 for batch pop on empty buffer, got %d", n)
	}
}

func TestRingBuffer_WrapAround1MillionCycles(t *testing.T) {
	// Use small capacity (128) to force rapid wrap-around cycles
	const capSize = 128
	const totalCycles = 1_000_000
	rb := NewRingBuffer(capSize)

	for i := 0; i < totalCycles; i++ {
		event := MetricEvent{
			Timestamp:  int64(i),
			LatencyNs:  int64(i * 10),
			RouteID:    uint32(i % 50),
			StatusCode: uint16(200 + (i % 5)),
		}
		if ok := rb.Push(event); !ok {
			t.Fatalf("unexpected push failure at cycle %d", i)
		}
		popped, ok := rb.Pop()
		if !ok {
			t.Fatalf("unexpected pop failure at cycle %d", i)
		}
		if popped.Timestamp != int64(i) || popped.LatencyNs != int64(i*10) {
			t.Fatalf("data integrity mismatch at cycle %d: got %+v", i, popped)
		}
	}

	if !rb.IsEmpty() {
		t.Fatalf("expected buffer to be empty after all cycles")
	}
	if rb.Dropped() != 0 {
		t.Fatalf("expected 0 drops during interleaved push/pop, got %d", rb.Dropped())
	}
}

func TestRingBuffer_Reset(t *testing.T) {
	rb := NewRingBuffer(16)
	for i := 0; i < 5; i++ {
		rb.Push(MetricEvent{Timestamp: int64(i)})
	}
	if rb.Len() != 5 {
		t.Fatalf("expected Len 5, got %d", rb.Len())
	}

	rb.Reset()
	if !rb.IsEmpty() {
		t.Fatalf("expected buffer to be empty after Reset")
	}
	if rb.Len() != 0 {
		t.Fatalf("expected Len 0 after Reset, got %d", rb.Len())
	}
	if rb.Dropped() != 0 {
		t.Fatalf("expected Dropped 0 after Reset, got %d", rb.Dropped())
	}

	// Verify buffer operates properly after Reset
	if ok := rb.Push(MetricEvent{Timestamp: 100}); !ok {
		t.Fatalf("push failed after reset")
	}
	item, ok := rb.Pop()
	if !ok || item.Timestamp != 100 {
		t.Fatalf("pop failed after reset: ok=%v, item=%+v", ok, item)
	}
}
