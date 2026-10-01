package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

func TestGateway_E2E_RoutingAndLoadBalancing(t *testing.T) {
	var count1, count2 atomic.Int64

	// Mock Upstream 1
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count1.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend-1 response"))
	}))
	defer backend1.Close()

	// Mock Upstream 2
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count2.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend-2 response"))
	}))
	defer backend2.Close()

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "test_gateway_e2e.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8089,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "service-route",
				Path:       "/api/service",
				Methods:    []string{"GET"},
				UpstreamID: "pool-1",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "pool-1",
				Algorithm: "round_robin",
				Targets: []config.TargetConfig{
					{URL: backend1.URL, Weight: 1},
					{URL: backend2.URL, Weight: 1},
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	pipeline := sup.BuildPipeline()
	gwServer := httptest.NewServer(pipeline)
	defer gwServer.Close()

	// Send 20 requests
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 20; i++ {
		resp, err := client.Get(gwServer.URL + "/api/service")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) == 0 {
			t.Fatalf("empty response body on request %d", i)
		}
	}

	// Verify both backends received requests
	c1 := count1.Load()
	c2 := count2.Load()
	if c1 == 0 || c2 == 0 {
		t.Fatalf("expected load distributed between backends, got count1=%d, count2=%d", c1, c2)
	}
	if c1+c2 != 20 {
		t.Fatalf("expected total 20 requests, got %d", c1+c2)
	}

	// Verify telemetry
	snap := sup.Snapshot()
	if snap.TotalRequests != 20 {
		t.Fatalf("expected snapshot TotalRequests=20, got %d", snap.TotalRequests)
	}

	// Cleanup
	_ = sup.Shutdown(context.Background())
}
