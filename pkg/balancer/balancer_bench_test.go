package balancer

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func createBenchTargets(n int) []*Backend {
	targets := make([]*Backend, n)
	for i := 0; i < n; i++ {
		b := NewBackend(mustURL(fmt.Sprintf("http://10.0.0.%d:8080", i+1)), int64(10*(i+1)), 0)
		b.RecordLatency(time.Duration(1+i) * time.Millisecond)
		targets[i] = b
	}
	return targets
}

func BenchmarkSWWR_Sequential(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewSWWR(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = bal.Select(ctx, nil)
	}
}

func BenchmarkSWWR_Parallel(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewSWWR(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = bal.Select(ctx, nil)
		}
	})
}

func BenchmarkP2C_Sequential(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewP2C(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = bal.Select(ctx, nil)
	}
}

func BenchmarkP2C_Parallel(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewP2C(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = bal.Select(ctx, nil)
		}
	})
}

func BenchmarkPeakEWMA_Sequential(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewPeakEWMA(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = bal.Select(ctx, nil)
	}
}

func BenchmarkPeakEWMA_Parallel(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewPeakEWMA(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = bal.Select(ctx, nil)
		}
	})
}

func BenchmarkIPHash_Sequential(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewIPHash(targets)
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = bal.Select(ctx, req)
	}
}

func BenchmarkIPHash_Parallel(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewIPHash(targets)
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = bal.Select(ctx, req)
		}
	})
}

func BenchmarkRoundRobin_Sequential(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewRoundRobin(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = bal.Select(ctx, nil)
	}
}

func BenchmarkRoundRobin_Parallel(b *testing.B) {
	targets := createBenchTargets(5)
	bal := NewRoundRobin(targets)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = bal.Select(ctx, nil)
		}
	})
}

func BenchmarkConnGuard_AcquireRelease(b *testing.B) {
	target := NewBackend(mustURL("http://bench-target"), 10, 0)

	// Warm up pool
	g, _ := target.AcquireGuard()
	g.Release()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		guard, err := target.AcquireGuard()
		if err != nil {
			b.Fatal(err)
		}
		guard.Release()
	}
}
