package stress

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
	"nexusgate/pkg/telemetry"
)

// TestBattery_IdleSleepTransition verifies that the battery conservation state machine
// automatically transitions into sleep mode upon traffic silence and wakes instantly.
func TestBattery_IdleSleepTransition(t *testing.T) {
	idleTimeout := 50 * time.Millisecond
	detector := telemetry.NewIdleDetector(idleTimeout)
	defer detector.Close()

	if detector.IsSleeping() {
		t.Errorf("expected initial state to be active")
	}

	// Wait past idle timeout with zero traffic
	time.Sleep(60 * time.Millisecond)

	if !detector.ShouldSleep() {
		t.Errorf("expected ShouldSleep to be true after idle timeout")
	}

	// Transition to sleep
	slept := detector.EnterSleep(nil)
	if !slept || !detector.IsSleeping() {
		t.Errorf("expected state machine to enter sleep mode")
	}

	// Incoming packet should wake immediately (<100us)
	start := time.Now()
	detector.OnActivity()
	wakeLatency := time.Since(start)

	if detector.IsSleeping() {
		t.Errorf("expected detector to wake up on activity")
	}

	if wakeLatency > 5*time.Millisecond {
		t.Errorf("wakeup took too long: %v (must be near-instant)", wakeLatency)
	}
}

// TestBattery_IdleCPUTimeQuietPeriod verifies that while the gateway is running with no traffic,
// CPU wakeups are suppressed and user+system CPU usage remains negligible (tickless sleep).
func TestBattery_IdleCPUTimeQuietPeriod(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "battery_idle.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8101,
		},
		Routes: []config.RouteConfig{
			{
				ID:      "battery-mock",
				Path:    "/ping",
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
		t.Fatalf("failed to create supervisor: %v", err)
	}

	gwServer := httptest.NewServer(sup.BuildPipeline())
	defer gwServer.Close()

	// 1. Initial request to warm up runtime
	resp, err := http.Get(gwServer.URL + "/ping")
	if err != nil {
		t.Fatalf("warmup request failed: %v", err)
	}
	_ = resp.Body.Close()

	// 2. Measure CPU usage before quiet period
	var rusageBefore syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusageBefore)

	// 3. Enter 300ms of quiet idle period with zero traffic
	quietDuration := 300 * time.Millisecond
	time.Sleep(quietDuration)

	// 4. Measure CPU usage after quiet period
	var rusageAfter syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusageAfter)

	userMicros := (rusageAfter.Utime.Sec-rusageBefore.Utime.Sec)*1_000_000 +
		int64(rusageAfter.Utime.Usec-rusageBefore.Utime.Usec)
	sysMicros := (rusageAfter.Stime.Sec-rusageBefore.Stime.Sec)*1_000_000 +
		int64(rusageAfter.Stime.Usec-rusageBefore.Stime.Usec)
	totalCPUMillis := float64(userMicros+sysMicros) / 1000.0

	t.Logf("Quiet idle period (300ms): consumed %.2f ms CPU time", totalCPUMillis)

	// Invariant: Over a 300ms idle window, CPU consumed must not exceed 50ms (<17% core utilization)
	// confirming that there are no busy loops or high-frequency polling ticks.
	maxAllowedCPUMillis := 50.0
	if totalCPUMillis > maxAllowedCPUMillis {
		t.Errorf("IDLE POWER VIOLATION: consumed %.2f ms CPU during 300ms quiet period (> %.2f ms limit)",
			totalCPUMillis, maxAllowedCPUMillis)
	}
}

// TestBattery_TrafficWakeupLatency ensures that a sleeping gateway serves cold requests with low latency.
func TestBattery_TrafficWakeupLatency(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "battery_wakeup.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8102,
		},
		Routes: []config.RouteConfig{
			{
				ID:      "fast-ping",
				Path:    "/fast-ping",
				Methods: []string{"GET"},
				Mock: &config.MockResponse{
					Enabled:    true,
					StatusCode: http.StatusOK,
					Body:       "fast-pong",
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

	// Sleep 100ms to allow idle detector to park
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	resp, err := http.Get(gwServer.URL + "/fast-ping")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	t.Logf("Wakeup cold request served in %v", elapsed)
	if elapsed > 100*time.Millisecond {
		t.Errorf("cold request took too long: %v", elapsed)
	}
}
