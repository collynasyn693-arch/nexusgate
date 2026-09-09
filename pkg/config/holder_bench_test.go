package config

import (
	"sync"
	"testing"
)

func BenchmarkAtomicConfigHolder_Get(b *testing.B) {
	cfg := NewDefaultConfig()
	holder := NewConfigHolder(cfg)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c := holder.Get()
		if c == nil {
			b.Fatal("nil config returned")
		}
	}
}

func BenchmarkAtomicConfigHolder_ParallelGet(b *testing.B) {
	cfg := NewDefaultConfig()
	holder := NewConfigHolder(cfg)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c := holder.Get()
			if c == nil {
				b.Fatal("nil config returned")
			}
		}
	})
}

type rwMutexHolder struct {
	mu  sync.RWMutex
	cfg *GatewayConfig
}

func (m *rwMutexHolder) Get() *GatewayConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func BenchmarkRWMutex_Get(b *testing.B) {
	cfg := NewDefaultConfig()
	holder := &rwMutexHolder{cfg: cfg}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c := holder.Get()
		if c == nil {
			b.Fatal("nil config returned")
		}
	}
}

func BenchmarkRWMutex_ParallelGet(b *testing.B) {
	cfg := NewDefaultConfig()
	holder := &rwMutexHolder{cfg: cfg}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c := holder.Get()
			if c == nil {
				b.Fatal("nil config returned")
			}
		}
	})
}

func BenchmarkConfigHolder_Swap(b *testing.B) {
	cfg1 := NewDefaultConfig()
	cfg2 := NewDefaultConfig()
	holder := NewConfigHolder(cfg1)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			_, _ = holder.Swap(cfg2)
		} else {
			_, _ = holder.Swap(cfg1)
		}
	}
}
