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
	"nexusgate/pkg/resilience"
)

func TestGateway_E2E_CircuitBreakerTripAndRecovery(t *testing.T) {
	var failBackend atomic.Bool
	failBackend.Store(true)

	// Mock server that fails when failBackend == true
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failBackend.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("500 internal error"))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("200 recovered"))
		}
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "test_circuit_e2e.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8091,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "circuit-route",
				Path:       "/api/circuit",
				Methods:    []string{"GET"},
				UpstreamID: "failing-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "failing-pool",
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

	// Pre-configure circuit breaker with short reset timeout for fast testing
	cbConfig := resilience.Config{
		Enabled:              true,
		ConsecutiveFailures:  3,
		FailureRateThreshold: 0.8,
		MinRequests:          10,
		ResetTimeout:         100 * time.Millisecond,
		HalfOpenMaxRequests:  2,
		ConsecutiveSuccesses: 2,
	}
	_ = sup.Breakers.GetOrCreate(backend.URL, cbConfig)

	pipeline := sup.BuildPipeline()
	gwServer := httptest.NewServer(pipeline)
	defer gwServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// Step 1: Send 3 requests that return 500 to trip the circuit
	for i := 0; i < 3; i++ {
		resp, err := client.Get(gwServer.URL + "/api/circuit")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected status 500 from failing backend on iteration %d, got %d (body: %s)", i, resp.StatusCode, string(body))
		}
	}

	// Step 2: 4th request must be short-circuited with 503
	resp, err := client.Get(gwServer.URL + "/api/circuit")
	if err != nil {
		t.Fatalf("trip check request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected short-circuit status 503, got %d", resp.StatusCode)
	}

	// Step 3: Wait for reset timeout to elapse (150ms > 100ms)
	time.Sleep(150 * time.Millisecond)

	// Step 4: Backend recovers
	failBackend.Store(false)

	// Step 5: Canaries succeed and restore circuit to Closed
	for i := 0; i < 2; i++ {
		resp, err := client.Get(gwServer.URL + "/api/circuit")
		if err != nil {
			t.Fatalf("canary %d failed: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected canary status 200, got %d", resp.StatusCode)
		}
	}

	_ = sup.Shutdown(context.Background())
}
