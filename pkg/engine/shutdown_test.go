package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexusgate/pkg/config"
)

func createTestConfig(port int) *config.GatewayConfig {
	return &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: port,
		},
		Routes: []RouteConfig{
			{
				ID:      "test-route",
				Path:    "/api/test",
				Methods: []string{"GET"},
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID: "test-upstream",
				Targets: []config.TargetConfig{
					{URL: "http://127.0.0.1:9099", Weight: 1},
				},
			},
		},
	}
}

type RouteConfig = config.RouteConfig

func TestSupervisor_ShutdownGraceful(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "test_shutdown.sock")

	cfg := createTestConfig(8088)
	sup, err := NewSupervisor(cfg, sockPath)
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	// Mock HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	sup.running.Store(true)
	sup.ActiveConns.Store(2) // Simulate 2 active in-flight requests

	// Simulate inflight connection draining after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		sup.ActiveConns.Store(0)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = sup.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error on graceful shutdown: %v", err)
	}

	if sup.ActiveConns.Load() != 0 {
		t.Fatalf("expected 0 active connections after shutdown")
	}

	// Verify socket removed
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("expected socket file to be unlinked after shutdown")
	}

	// Test idempotent shutdown
	err2 := sup.Shutdown(ctx)
	if err2 != nil {
		t.Fatalf("subsequent shutdown call should be idempotent, got: %v", err2)
	}
}
