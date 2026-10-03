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
	"nexusgate/pkg/proxy"
)

func TestGateway_GoroutineLeakDetector(t *testing.T) {
	// 1. Mock upstream that echoes back
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("goroutine-check-response"))
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "goroutine_leak.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8100,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "goroutine-route",
				Path:       "/goroutine-check",
				Methods:    []string{"GET"},
				UpstreamID: "gr-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "gr-pool",
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

	// 2. Warm up with 100 requests to establish persistent connection pools
	ctx := context.Background()
	_, _ = RunLoad(ctx, LoadConfig{
		TargetURL:     gwServer.URL + "/goroutine-check",
		Concurrency:   5,
		TotalRequests: 100,
	})

	// 3. Capture baseline goroutines
	time.Sleep(100 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	// 4. Heavy concurrent load burst: 2000 requests across 16 worker goroutines
	res, err := RunLoad(ctx, LoadConfig{
		TargetURL:     gwServer.URL + "/goroutine-check",
		Concurrency:   16,
		TotalRequests: 2000,
	})
	if err != nil {
		t.Fatalf("load execution failed: %v", err)
	}
	if res.SuccessCount < 2000 {
		t.Fatalf("expected at least 2000 successes, got %d", res.SuccessCount)
	}

	// 5. Close idle client connections so keepalive reader routines terminate
	gwServer.CloseClientConnections()
	backend.CloseClientConnections()
	if tr, ok := proxy.DefaultTransport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}

	// Poll for goroutines to settle back to baseline
	deadline := time.Now().Add(2 * time.Second)
	finalGoroutines := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		finalGoroutines = runtime.NumGoroutine()
		if finalGoroutines <= baselineGoroutines+2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Logf("Baseline Goroutines: %d, Final Goroutines: %d", baselineGoroutines, finalGoroutines)

	// Invariant: No leaked goroutines (allowance for test runner background routines)
	if finalGoroutines > baselineGoroutines+3 {
		buf := make([]byte, 64*1024)
		n := runtime.Stack(buf, true)
		t.Logf("Stack traces:\n%s", string(buf[:n]))
		t.Errorf("GOROUTINE LEAK DETECTED: baseline=%d, post-load=%d (delta=+%d)",
			baselineGoroutines, finalGoroutines, finalGoroutines-baselineGoroutines)
	}
}
