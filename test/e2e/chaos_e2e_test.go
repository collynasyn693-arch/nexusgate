package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

func TestGateway_E2E_ChaosAndMocking(t *testing.T) {
	backendReceived := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendReceived = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend ok"))
	}))
	defer backend.Close()

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "test_chaos_e2e.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8092,
		},
		Chaos: config.ChaosConfig{
			Enabled:        true,
			HeaderKey:      "X-NexusGate-Chaos",
			AllowedSubnets: []string{"127.0.0.0/8", "::1/128"},
			MaxDelay:       2 * time.Second,
			StrictMode:     false,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "chaos-route",
				Path:       "/api/chaos",
				Methods:    []string{"GET"},
				UpstreamID: "mock-upstream",
			},
			{
				ID:      "static-mock-route",
				Path:    "/api/mock",
				Methods: []string{"GET"},
				Mock: &config.MockResponse{
					Enabled:    true,
					StatusCode: 200,
					Headers:    map[string]string{"X-NexusGate-Mock": "active"},
					Body:       `{"mocked":true,"message":"static response"}`,
				},
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "mock-upstream",
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

	pipeline := sup.BuildPipeline()
	gwServer := httptest.NewServer(pipeline)
	defer gwServer.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// Test 1: Static mock route bypasses upstream completely
	resp, err := client.Get(gwServer.URL + "/api/mock")
	if err != nil {
		t.Fatalf("mock request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for mock, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-NexusGate-Mock") != "active" {
		t.Fatalf("expected X-NexusGate-Mock header on mock response")
	}
	if string(body) != `{"mocked":true,"message":"static response"}` {
		t.Fatalf("unexpected mock body: %s", string(body))
	}
	if backendReceived {
		t.Fatalf("static mock route must not forward to backend")
	}

	// Test 2: Injected Status Override (?__status=504)
	resp, err = client.Get(gwServer.URL + "/api/chaos?__status=504")
	if err != nil {
		t.Fatalf("chaos status request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504 from chaos injection, got %d", resp.StatusCode)
	}

	// Test 3: Simulated Latency Injection (?__delay=100ms)
	start := time.Now()
	resp, err = client.Get(gwServer.URL + "/api/chaos?__delay=100ms")
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("delay request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 after delay, got %d", resp.StatusCode)
	}
	if duration < 90*time.Millisecond {
		t.Fatalf("expected delay of ~100ms, elapsed: %v", duration)
	}

	_ = sup.Shutdown(context.Background())
}
