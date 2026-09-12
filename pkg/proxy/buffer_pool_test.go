package proxy

import (
	"sync"
	"testing"
)

func TestBufferPool_SizesAndInvariants(t *testing.T) {
	pool := NewDualTierBufferPool()

	// Small buffer invariant: len == cap == SmallBufferSize
	sBuf := pool.GetSmall()
	if len(sBuf) != SmallBufferSize || cap(sBuf) != SmallBufferSize {
		t.Fatalf("expected GetSmall len=%d cap=%d, got len=%d cap=%d",
			SmallBufferSize, SmallBufferSize, len(sBuf), cap(sBuf))
	}
	pool.PutSmall(sBuf)

	// Large buffer invariant: len == cap == LargeBufferSize
	lBuf := pool.GetLarge()
	if len(lBuf) != LargeBufferSize || cap(lBuf) != LargeBufferSize {
		t.Fatalf("expected GetLarge len=%d cap=%d, got len=%d cap=%d",
			LargeBufferSize, LargeBufferSize, len(lBuf), cap(lBuf))
	}
	pool.PutLarge(lBuf)

	// Dynamic Get tests
	buf1 := pool.Get(512)
	if cap(buf1) != SmallBufferSize {
		t.Fatalf("expected 512-byte request to return SmallBufferSize, got cap=%d", cap(buf1))
	}
	pool.Put(buf1)

	buf2 := pool.Get(SmallBufferSize)
	if cap(buf2) != SmallBufferSize {
		t.Fatalf("expected SmallBufferSize request to return SmallBufferSize, got cap=%d", cap(buf2))
	}
	pool.Put(buf2)

	buf3 := pool.Get(SmallBufferSize + 1)
	if cap(buf3) != LargeBufferSize {
		t.Fatalf("expected 4097-byte request to return LargeBufferSize, got cap=%d", cap(buf3))
	}
	pool.Put(buf3)
}

func TestBufferPool_ReuseAndRetention(t *testing.T) {
	pool := NewDualTierBufferPool()

	buf := pool.GetSmall()
	buf[0] = 0xAA
	buf[1] = 0xBB
	pool.PutSmall(buf)

	reused := pool.GetSmall()
	if len(reused) != SmallBufferSize || cap(reused) != SmallBufferSize {
		t.Fatalf("reused buffer must preserve full capacity, got len=%d cap=%d", len(reused), cap(reused))
	}
	// Verify buffer is readable and reusable
	if reused[0] != 0xAA || reused[1] != 0xBB {
		t.Logf("buffer was allocated freshly or cleared (acceptable in sync.Pool)")
	}
	pool.PutSmall(reused)
}

func TestBufferPool_BoundsAndCapacityRejection(t *testing.T) {
	pool := NewDualTierBufferPool()

	// 1. Slice capacity shrunk below SmallBufferSize
	shrunkSlice := make([]byte, 100)
	pool.PutSmall(shrunkSlice) // Must be discarded without panic

	// 2. Slice capacity larger than SmallBufferSize but passed to PutSmall
	oversizedSlice := make([]byte, 8192)
	pool.PutSmall(oversizedSlice) // Must be discarded

	// 3. Shrunk slice passed to PutLarge
	shrunkLarge := make([]byte, SmallBufferSize)
	pool.PutLarge(shrunkLarge) // Must be discarded

	// 4. Arbitrary capacity passed to Put
	arbitrary := make([]byte, 12345)
	pool.Put(arbitrary) // Must be discarded

	// Verify normal Get calls still work reliably
	s := pool.GetSmall()
	if cap(s) != SmallBufferSize {
		t.Fatalf("pool poisoned with corrupted buffer, cap=%d", cap(s))
	}
	pool.PutSmall(s)
}

func TestBufferPool_ConcurrentStress(t *testing.T) {
	pool := NewDualTierBufferPool()
	const goroutines = 50
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if (id+j)%2 == 0 {
					buf := pool.GetSmall()
					buf[0] = byte(id)
					buf[SmallBufferSize-1] = byte(j)
					pool.PutSmall(buf)
				} else {
					buf := pool.GetLarge()
					buf[0] = byte(id)
					buf[LargeBufferSize-1] = byte(j)
					pool.PutLarge(buf)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestBufferPool_ZeroAllocations(t *testing.T) {
	pool := NewDualTierBufferPool()

	// Prime the pools
	b1 := pool.GetSmall()
	pool.PutSmall(b1)
	b2 := pool.GetLarge()
	pool.PutLarge(b2)

	smallAllocs := testing.AllocsPerRun(1000, func() {
		buf := pool.GetSmall()
		buf[0] = 1
		pool.PutSmall(buf)
	})

	if smallAllocs > 0 {
		t.Fatalf("expected 0 allocs/op for GetSmall/PutSmall, got %v", smallAllocs)
	}

	largeAllocs := testing.AllocsPerRun(1000, func() {
		buf := pool.GetLarge()
		buf[0] = 1
		pool.PutLarge(buf)
	})

	if largeAllocs > 0 {
		t.Fatalf("expected 0 allocs/op for GetLarge/PutLarge, got %v", largeAllocs)
	}
}
