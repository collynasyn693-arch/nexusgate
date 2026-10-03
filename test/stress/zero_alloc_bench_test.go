package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"nexusgate/pkg/balancer"
	"nexusgate/pkg/proxy"
	"nexusgate/pkg/router"
	"nexusgate/pkg/telemetry"
)

// TestZeroAlloc_RoutingLookup asserts that router trie lookups have strictly 0 allocs/op.
func TestZeroAlloc_RoutingLookup(t *testing.T) {
	r := router.New()
	_ = r.Handle("GET", "/api/v1/users/:id/profile", router.HandlerFunc(func(w http.ResponseWriter, req *http.Request, p router.Params) {}))

	params := make(router.Params, 0, 8)

	allocs := testing.AllocsPerRun(1000, func() {
		params = params[:0]
		_, _ = r.Lookup("GET", "/api/v1/users/12345/profile", &params)
	})

	if allocs > 0 {
		t.Errorf("VIOLATION: router lookup allocated %f allocs/op (must be 0)", allocs)
	}
}

// TestZeroAlloc_BalancerSelection asserts that balancer decision paths have strictly 0 allocs/op.
func TestZeroAlloc_BalancerSelection(t *testing.T) {
	u1, _ := url.Parse("http://10.0.0.1:8080")
	u2, _ := url.Parse("http://10.0.0.2:8080")
	b1 := balancer.NewBackend(u1, 1, 0)
	b2 := balancer.NewBackend(u2, 1, 0)

	rr := balancer.NewRoundRobin([]*balancer.Backend{b1, b2})
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = rr.Select(ctx, req)
	})

	if allocs > 0 {
		t.Errorf("VIOLATION: balancer selection allocated %f allocs/op (must be 0)", allocs)
	}
}

// TestZeroAlloc_HeaderSanitization asserts standard hop-by-hop header removal has 0 allocs/op.
func TestZeroAlloc_HeaderSanitization(t *testing.T) {
	h := make(http.Header)
	closeVal := []string{"close"}
	keepAliveVal := []string{"timeout=5"}

	allocs := testing.AllocsPerRun(1000, func() {
		h["Connection"] = closeVal
		h["Keep-Alive"] = keepAliveVal
		proxy.RemoveHopByHopHeaders(h)
	})

	if allocs > 0 {
		t.Errorf("VIOLATION: hop-by-hop removal allocated %f allocs/op (must be 0)", allocs)
	}
}

// TestZeroAlloc_TelemetryRingPush asserts telemetry ring buffer push has 0 allocs/op.
func TestZeroAlloc_TelemetryRingPush(t *testing.T) {
	ring := telemetry.NewRingBuffer(1024)
	event := telemetry.MetricEvent{
		Timestamp:  time.Now().UnixNano(),
		LatencyNs:  123456,
		BytesIn:    100,
		BytesOut:   200,
		RouteID:    42,
		StatusCode: 200,
	}

	allocs := testing.AllocsPerRun(1000, func() {
		ring.Push(event)
	})

	if allocs > 0 {
		t.Errorf("VIOLATION: telemetry push allocated %f allocs/op (must be 0)", allocs)
	}
}

// BenchmarkCore_ZeroAllocPaths executes micro-benchmarks for all critical zero-alloc paths.
func BenchmarkCore_ZeroAllocPaths(b *testing.B) {
	r := router.New()
	_ = r.Handle("GET", "/api/v1/users/:id/profile", router.HandlerFunc(func(w http.ResponseWriter, req *http.Request, p router.Params) {}))

	params := make(router.Params, 0, 8)
	b.Run("RouterLookup", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			params = params[:0]
			_, _ = r.Lookup("GET", "/api/v1/users/12345/profile", &params)
		}
	})

	u1, _ := url.Parse("http://10.0.0.1:8080")
	u2, _ := url.Parse("http://10.0.0.2:8080")
	b1 := balancer.NewBackend(u1, 1, 0)
	b2 := balancer.NewBackend(u2, 1, 0)
	rr := balancer.NewRoundRobin([]*balancer.Backend{b1, b2})
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	b.Run("BalancerSelect", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = rr.Select(ctx, req)
		}
	})

	ring := telemetry.NewRingBuffer(1024)
	event := telemetry.MetricEvent{
		Timestamp:  time.Now().UnixNano(),
		LatencyNs:  123456,
		StatusCode: 200,
	}
	b.Run("TelemetryPush", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ring.Push(event)
		}
	})
}
