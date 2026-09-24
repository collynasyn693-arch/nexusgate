package telemetry

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// DefaultRingBufferSize is the standard power-of-two capacity (65,536 slots).
const DefaultRingBufferSize = 65536

// ringSlot represents a single slot within the circular buffer.
// Explicitly padded to strictly 64 bytes (1 cache line on ARM64):
// - seq: atomic.Uint64 (8 bytes, offset 0)
// - event: MetricEvent (32 bytes, offset 8)
// - _pad: [24]byte (24 bytes, offset 40)
// Total: strictly 64 bytes.
// This eliminates cache-line straddling and false sharing between adjacent slots.
type ringSlot struct {
	seq   atomic.Uint64 // 8 bytes (offset 0..7)
	event MetricEvent   // 32 bytes (offset 8..39)
	_pad  [24]byte      // 24 bytes (offset 40..63)
}

func init() {
	if sz := unsafe.Sizeof(ringSlot{}); sz != 64 {
		panic(fmt.Sprintf("ringSlot size mismatch: expected 64 bytes, got %d", sz))
	}
}

// RingBuffer is a lock-free, cache-line padded Multi-Producer Single-Consumer (MPSC)
// circular buffer optimized for ARM64 multi-core and big.LITTLE architectures.
type RingBuffer struct {
	head    atomic.Uint64
	_pad0   [56]byte // Cache line padding isolating head cursor

	tail    atomic.Uint64
	_pad1   [56]byte // Cache line padding isolating tail cursor

	dropped atomic.Uint64
	_pad2   [56]byte // Cache line padding isolating dropped counter

	capacity uint64
	mask     uint64
	slots    []ringSlot
}

// NewRingBuffer allocates and initializes a lock-free circular ring buffer.
// The capacity must be a positive power of two (e.g. 65536).
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 || (capacity&(capacity-1)) != 0 {
		capacity = DefaultRingBufferSize
	}
	capU64 := uint64(capacity)
	rb := &RingBuffer{
		capacity: capU64,
		mask:     capU64 - 1,
		slots:    make([]ringSlot, capU64),
	}
	// Initialize slot sequence numbers according to Vyukov's algorithm
	for i := uint64(0); i < capU64; i++ {
		rb.slots[i].seq.Store(i)
	}
	return rb
}

// Push enqueues a MetricEvent into the ring buffer without blocking.
// Multiple producer goroutines may call Push concurrently.
// Returns true if the event was enqueued, or false if the ring buffer is full.
func (rb *RingBuffer) Push(event MetricEvent) bool {
	pos := rb.head.Load()
	for {
		slot := &rb.slots[pos&rb.mask]
		seq := slot.seq.Load()
		diff := int64(seq) - int64(pos)

		if diff == 0 {
			// Slot is ready for writing at sequence pos
			if rb.head.CompareAndSwap(pos, pos+1) {
				slot.event = event
				// Store-Release: ensures slot.event payload is globally visible
				// before the sequence advancement is observed by consumer.
				slot.seq.Store(pos + 1)
				return true
			}
			// Another producer won the CAS; reload pos and retry
			pos = rb.head.Load()
		} else if diff < 0 {
			// Buffer is full (wrap-around caught up to tail)
			rb.dropped.Add(1)
			return false
		} else {
			// Pos is lagging behind; refresh head
			pos = rb.head.Load()
		}
	}
}

// PushOverwrite attempts to push an event into the ring buffer.
// If the buffer is full, it records the drop on rb.dropped and returns false,
// ensuring that high-throughput producers never block the gateway hot path.
func (rb *RingBuffer) PushOverwrite(event MetricEvent) bool {
	return rb.Push(event)
}

// Pop dequeues a MetricEvent from the ring buffer.
// Must be called by a single consumer goroutine.
// Returns the event and true, or zero-value event and false if the buffer is empty.
func (rb *RingBuffer) Pop() (MetricEvent, bool) {
	pos := rb.tail.Load()
	slot := &rb.slots[pos&rb.mask]
	seq := slot.seq.Load()
	diff := int64(seq) - int64(pos+1)

	if diff == 0 {
		// Load-Acquire: payload is guaranteed valid
		event := slot.event
		// Mark slot available for next cycle (pos + capacity)
		slot.seq.Store(pos + rb.mask + 1)
		rb.tail.Store(pos + 1)
		return event, true
	}
	return MetricEvent{}, false
}

// BatchPop dequeues up to len(dest) MetricEvents in a single contiguous batch,
// amortizing the tail store memory barrier across multiple items.
// Returns the number of events popped. Must be called by a single consumer.
func (rb *RingBuffer) BatchPop(dest []MetricEvent) int {
	pos := rb.tail.Load()
	count := 0
	limit := len(dest)

	for count < limit {
		slot := &rb.slots[pos&rb.mask]
		seq := slot.seq.Load()
		diff := int64(seq) - int64(pos+1)

		if diff != 0 {
			// No more ready events
			break
		}
		dest[count] = slot.event
		slot.seq.Store(pos + rb.mask + 1)
		pos++
		count++
	}

	if count > 0 {
		// Single store barrier for the entire popped batch
		rb.tail.Store(pos)
	}
	return count
}

// Len returns the approximate number of unconsumed elements currently in the buffer.
func (rb *RingBuffer) Len() int {
	head := rb.head.Load()
	tail := rb.tail.Load()
	if head > tail {
		diff := head - tail
		if diff > rb.capacity {
			return int(rb.capacity)
		}
		return int(diff)
	}
	return 0
}

// Cap returns the total capacity of the ring buffer.
func (rb *RingBuffer) Cap() int {
	return int(rb.capacity)
}

// Dropped returns the cumulative number of dropped events due to buffer saturation.
func (rb *RingBuffer) Dropped() uint64 {
	return rb.dropped.Load()
}

// IsEmpty returns true if the ring buffer contains no unconsumed events.
func (rb *RingBuffer) IsEmpty() bool {
	pos := rb.tail.Load()
	slot := &rb.slots[pos&rb.mask]
	seq := slot.seq.Load()
	return int64(seq)-int64(pos+1) != 0
}

// Reset clears the ring buffer, resetting all cursors and slot sequence numbers.
func (rb *RingBuffer) Reset() {
	rb.head.Store(0)
	rb.tail.Store(0)
	rb.dropped.Store(0)
	for i := uint64(0); i < rb.capacity; i++ {
		rb.slots[i].seq.Store(i)
		rb.slots[i].event = MetricEvent{}
	}
}
