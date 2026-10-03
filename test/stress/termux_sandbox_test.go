package stress

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexusgate/internal/platform"
	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
)

// TestTermux_UnprivilegedSandbox simulates execution inside an Android Termux userland environment.
func TestTermux_UnprivilegedSandbox(t *testing.T) {
	// 1. Mock unprivileged Termux UID (e.g. Android app UID 10464)
	restoreUID := platform.SetMockUID(10464)
	defer restoreUID()

	if platform.IsRoot() {
		t.Fatalf("expected non-root status in simulated Termux sandbox")
	}

	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "termux_sim.sock")

	// 2. Validate rejection of privileged ports (<1024) under non-root
	privCfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 80, // Privileged port
		},
	}
	err := config.Validate(privCfg)
	if err == nil {
		t.Errorf("expected validation to reject port 80 under unprivileged user")
	}

	// 3. Validate success of unprivileged port (8105)
	goodCfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8105,
		},
		Routes: []config.RouteConfig{
			{
				ID:      "termux-route",
				Path:    "/sandbox",
				Methods: []string{"GET"},
				Mock: &config.MockResponse{
					Enabled:    true,
					StatusCode: http.StatusOK,
					Body:       `{"env":"termux_sandbox","uid":10464}`,
				},
			},
		},
	}
	err = config.Validate(goodCfg)
	if err != nil {
		t.Fatalf("expected unprivileged configuration to pass validation: %v", err)
	}

	// 4. Test supervisor execution in simulated sandbox
	sup, err := engine.NewSupervisor(goodCfg, sockPath)
	if err != nil {
		t.Fatalf("failed to create supervisor in sandbox: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sup.Serve(ctx)
	}()

	// Wait for listener to bind
	time.Sleep(50 * time.Millisecond)

	// 5. Query running gateway on unprivileged port
	resp, err := http.Get("http://127.0.0.1:8105/sandbox")
	if err != nil {
		t.Fatalf("failed to query sandbox gateway: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected HTTP 200, got: %d", resp.StatusCode)
	}

	// 6. Verify socket file permissions
	if stat, err := os.Stat(sockPath); err == nil {
		perm := stat.Mode().Perm()
		t.Logf("Socket permissions: %04o", perm)
	}

	// 7. Clean shutdown and verify socket unlink
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("serve returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("shutdown timed out")
	}

	// Socket should be unlinked upon exit
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("expected socket file to be unlinked on shutdown")
	}
}
