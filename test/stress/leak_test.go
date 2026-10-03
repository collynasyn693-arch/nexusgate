package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

func TestGateway_MemoryLeakRegression(t *testing.T) {
	// 1. Setup mock upstream backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("leak-check-payload-data"))
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "leak_test.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8099,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "leak-route",
				Path:       "/leak",
				Methods:    []string{"GET"},
				UpstreamID: "leak-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "leak-pool",
				Algorithm: "round_robin",
				Targets: []config.TargetConfig{
					{URL: backend.URL, Weight: 1},
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	gwServer := httptest.NewServer(sup.BuildPipeline())
	defer gwServer.Close()

	// 2. Warm up phase (500 requests) to populate pools and runtime caches
	ctx := context.Background()
	_, err = RunLoad(ctx, LoadConfig{
		TargetURL:     gwServer.URL + "/leak",
		Concurrency:   8,
		TotalRequests: 500,
	})
	if err != nil {
		t.Fatalf("warmup failed: %v", err)
	}

	// 3. Baseline memory measurement after GC
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 4. Sustained high-throughput workload (5000 requests)
	sustainedRequests := int64(5000)
	res, err := RunLoad(ctx, LoadConfig{
		TargetURL:     gwServer.URL + "/leak",
		Concurrency:   16,
		TotalRequests: sustainedRequests,
	})
	if err != nil {
		t.Fatalf("sustained load run failed: %v", err)
	}

	if res.SuccessCount < sustainedRequests {
		t.Fatalf("expected at least %d successes, got %d", sustainedRequests, res.SuccessCount)
	}

	// 5. Post-run memory measurement after GC
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	heapGrowth := int64(memAfter.HeapAlloc) - int64(memBefore.HeapAlloc)
	t.Logf("HeapAlloc Before: %d KB, HeapAlloc After: %d KB, Delta: %d KB",
		memBefore.HeapAlloc/1024, memAfter.HeapAlloc/1024, heapGrowth/1024)

	// Max permissible growth after 5000 requests: 2 MB
	// (Proves no per-request heap accumulation or uncollected leak)
	maxAllowedGrowthBytes := int64(2 * 1024 * 1024)
	if heapGrowth > maxAllowedGrowthBytes {
		t.Errorf("POTENTIAL MEMORY LEAK: heap grew by %d KB (> %d KB limit) over %d requests",
			heapGrowth/1024, maxAllowedGrowthBytes/1024, sustainedRequests)
	}
}
