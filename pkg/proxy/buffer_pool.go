package proxy

import (
	"sync"
)

// DefaultBufferPool is the global singleton instance used when no custom pool is specified.
var DefaultBufferPool BufferPool = NewDualTierBufferPool()

// DualTierBufferPool provides high-performance byte buffer recycling with 4KB and 32KB pools.
// To guarantee 0 allocs/op during steady-state pooling on ARM64, the underlying sync.Pools
// store pointers to fixed-size byte arrays (*[4096]byte and *[32768]byte).
// Conversion between slice and array pointer uses Go 1.20+ slice-to-array-pointer casting,
// eliminating interface boxing heap allocations (convTslice) entirely.
type DualTierBufferPool struct {
	smallPool sync.Pool
	largePool sync.Pool
}

// NewDualTierBufferPool constructs a new dual-tier buffer recycling manager.
func NewDualTierBufferPool() *DualTierBufferPool {
	return &DualTierBufferPool{
		smallPool: sync.Pool{
			New: func() any {
				return new([SmallBufferSize]byte)
			},
		},
		largePool: sync.Pool{
			New: func() any {
				return new([LargeBufferSize]byte)
			},
		},
	}
}

// GetSmall retrieves a 4KB buffer slice where len == cap == 4096.
// Invariant: len(buf) > 0 to prevent io.CopyBuffer runtime panics.
func (p *DualTierBufferPool) GetSmall() []byte {
	bufPtr := p.smallPool.Get().(*[SmallBufferSize]byte)
	return bufPtr[:]
}

// PutSmall returns a 4KB buffer slice to the pool.
// Invariants:
// 1. Slices whose capacity has been reduced or abnormally expanded (cap != 4096) are discarded to GC.
// 2. The slice is converted to a fixed-size array pointer without heap boxing allocations.
func (p *DualTierBufferPool) PutSmall(b []byte) {
	if cap(b) != SmallBufferSize {
		return
	}
	// Reslice to full capacity before converting
	b = b[:SmallBufferSize]
	arrPtr := (*[SmallBufferSize]byte)(b)
	p.smallPool.Put(arrPtr)
}

// GetLarge retrieves a 32KB buffer slice where len == cap == 32768.
// Invariant: len(buf) > 0 to prevent io.CopyBuffer runtime panics.
func (p *DualTierBufferPool) GetLarge() []byte {
	bufPtr := p.largePool.Get().(*[LargeBufferSize]byte)
	return bufPtr[:]
}

// PutLarge returns a 32KB buffer slice to the pool.
// Invariants:
// 1. Slices whose capacity is not exactly 32768 are discarded to GC.
// 2. The slice is converted to a fixed-size array pointer without heap boxing allocations.
func (p *DualTierBufferPool) PutLarge(b []byte) {
	if cap(b) != LargeBufferSize {
		return
	}
	// Reslice to full capacity before converting
	b = b[:LargeBufferSize]
	arrPtr := (*[LargeBufferSize]byte)(b)
	p.largePool.Put(arrPtr)
}

// Get retrieves a buffer slice of at least the requested size.
// If size <= SmallBufferSize (4096), a small buffer is returned.
// Otherwise, a large buffer (32768) is returned.
func (p *DualTierBufferPool) Get(size int) []byte {
	if size <= SmallBufferSize {
		return p.GetSmall()
	}
	return p.GetLarge()
}

// Put routes the slice back to either the small pool or large pool based on its capacity.
// Slices not matching either 4096 or 32768 bytes are discarded to avoid pool poisoning.
func (p *DualTierBufferPool) Put(b []byte) {
	switch cap(b) {
	case SmallBufferSize:
		p.PutSmall(b)
	case LargeBufferSize:
		p.PutLarge(b)
	default:
		// Capacity mismatch: discard slice to allow garbage collector to reclaim it.
		return
	}
}
