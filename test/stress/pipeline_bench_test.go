package stress

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

// BenchmarkPipeline_MockRoute benchmarks the complete supervisor pipeline for a mock route,
// isolating the gateway routing, chaos check, telemetry ring push, and response writing overhead.
func BenchmarkPipeline_MockRoute(b *testing.B) {
	tempDir := b.TempDir()
	sockPath := filepath.Join(tempDir, "bench_mock.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8096,
		},
		Routes: []config.RouteConfig{
			{
				ID:      "mock-fast",
				Path:    "/api/bench-fast",
				Methods: []string{"GET"},
				Mock: &config.MockResponse{
					Enabled:    true,
					StatusCode: http.StatusOK,
					Body:       `{"status":"ok","source":"nexusgate-mock"}`,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		b.Fatalf("failed to create supervisor: %v", err)
	}

	pipeline := sup.BuildPipeline()
	req := httptest.NewRequest(http.MethodGet, "/api/bench-fast", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		pipeline.ServeHTTP(w, req)
	}
}

// BenchmarkPipeline_ProxiedRoute benchmarks the complete supervisor pipeline with a real
// backend HTTP round-trip, measuring reverse proxy buffering, routing, and balancer overhead.
func BenchmarkPipeline_ProxiedRoute(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"upstream":"backend-node"}`))
	}))
	defer backend.Close()

	tempDir := b.TempDir()
	sockPath := filepath.Join(tempDir, "bench_proxy.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8097,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "proxy-route",
				Path:       "/api/proxy",
				Methods:    []string{"GET"},
				UpstreamID: "bench-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "bench-pool",
				Algorithm: "round_robin",
				Targets: []config.TargetConfig{
					{URL: backend.URL, Weight: 1},
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		b.Fatalf("failed to create supervisor: %v", err)
	}

	pipeline := sup.BuildPipeline()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		pipeline.ServeHTTP(w, req)
	}
}

// BenchmarkPipeline_Parallel measures pipeline throughput under high multi-core concurrency.
func BenchmarkPipeline_Parallel(b *testing.B) {
	tempDir := b.TempDir()
	sockPath := filepath.Join(tempDir, "bench_parallel.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8098,
		},
		Routes: []config.RouteConfig{
			{
				ID:      "mock-parallel",
				Path:    "/api/parallel",
				Methods: []string{"GET"},
				Mock: &config.MockResponse{
					Enabled:    true,
					StatusCode: http.StatusOK,
					Body:       "pong",
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		b.Fatalf("failed to create supervisor: %v", err)
	}

	pipeline := sup.BuildPipeline()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodGet, "/api/parallel", nil)
		for pb.Next() {
			w := httptest.NewRecorder()
			pipeline.ServeHTTP(w, req)
		}
	})
}
