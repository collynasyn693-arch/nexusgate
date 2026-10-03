package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

func TestGateway_Stress_Zero502Errors(t *testing.T) {
	// 1. Setup mock cluster with 3 healthy backend nodes
	cluster := NewMockCluster([]ClusterNodeConfig{
		{ID: "worker-1", BaseDelay: 0},
		{ID: "worker-2", BaseDelay: 0},
		{ID: "worker-3", BaseDelay: 0},
	})
	defer cluster.Close()

	// 2. Setup supervisor
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "stress_gateway.sock")

	targets := make([]config.TargetConfig, len(cluster.Nodes))
	for i, n := range cluster.Nodes {
		targets[i] = config.TargetConfig{URL: n.Server.URL, Weight: 1}
	}

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8095,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "stress-route",
				Path:       "/stress",
				Methods:    []string{"GET"},
				UpstreamID: "stress-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "stress-pool",
				Algorithm: "round_robin",
				Targets:   targets,
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	gwServer := httptest.NewServer(sup.BuildPipeline())
	defer gwServer.Close()

	// Calibrate request count for high stress with zero dropped packets
	reqCount := int64(2000)
	if envVal := os.Getenv("STRESS_REQUESTS"); envVal != "" {
		if parsed, err := strconv.ParseInt(envVal, 10, 64); err == nil && parsed > 0 {
			reqCount = parsed
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := RunLoad(ctx, LoadConfig{
		TargetURL:     gwServer.URL + "/stress",
		Concurrency:   16,
		TotalRequests: reqCount,
	})
	if err != nil {
		t.Fatalf("stress load test failed: %v", err)
	}

	t.Logf("Stress test completed: %d total, %d success, %d errors, %.1f req/s",
		res.TotalRequests, res.SuccessCount, res.ErrorCount, res.RequestsPerSec)

	// Invariant: ZERO 502 Bad Gateway errors on healthy upstreams
	badGateways := res.StatusCodes[http.StatusBadGateway]
	if badGateways != 0 {
		t.Errorf("VIOLATION: encountered %d 502 Bad Gateway responses under load!", badGateways)
	}

	serviceUnavailable := res.StatusCodes[http.StatusServiceUnavailable]
	if serviceUnavailable != 0 {
		t.Errorf("VIOLATION: encountered %d 503 Service Unavailable responses!", serviceUnavailable)
	}

	if res.SuccessCount < reqCount {
		t.Errorf("expected %d successes, got %d", reqCount, res.SuccessCount)
	}
}
