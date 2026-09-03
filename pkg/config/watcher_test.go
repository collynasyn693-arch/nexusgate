package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcherFileModification(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nexusgate.yaml")

	initialYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: 1
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	initialCfg, err := ParseFile(configPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	ApplyDefaults(initialCfg)

	holder := NewConfigHolder(initialCfg)
	var reloadFired atomic.Bool

	holder.Subscribe(func(oldCfg, newCfg *GatewayConfig) {
		reloadFired.Store(true)
	})

	watcher := NewWatcher(configPath, holder,
		WithPollInterval(20*time.Millisecond),
		WithDebounceDuration(10*time.Millisecond),
	)
	watcher.Start()
	defer watcher.Stop()

	// Update file with new weight
	time.Sleep(30 * time.Millisecond)
	updatedYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: 5
`
	if err := os.WriteFile(configPath, []byte(updatedYAML), 0600); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Wait for poll loop to catch change
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reloadFired.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !reloadFired.Load() {
		t.Fatalf("expected watcher to detect file modification and trigger reload")
	}

	active := holder.Get()
	if active.Upstreams[0].Targets[0].Weight != 5 {
		t.Errorf("expected updated weight 5, got %d", active.Upstreams[0].Targets[0].Weight)
	}
}

func TestWatcherSIGHUPReload(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nexusgate.yaml")

	initialYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: 2
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	initialCfg, err := ParseFile(configPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	ApplyDefaults(initialCfg)

	holder := NewConfigHolder(initialCfg)
	var reloaded atomic.Bool

	holder.Subscribe(func(oldCfg, newCfg *GatewayConfig) {
		if newCfg.Upstreams[0].Targets[0].Weight == 10 {
			reloaded.Store(true)
		}
	})

	watcher := NewWatcher(configPath, holder,
		WithPollInterval(10*time.Second), // High poll interval so only SIGHUP triggers
	)
	watcher.Start()
	defer watcher.Stop()

	// Modify file on disk
	updatedYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: 10
`
	if err := os.WriteFile(configPath, []byte(updatedYAML), 0600); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Trigger SIGHUP
	watcher.TriggerSIGHUP()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reloaded.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !reloaded.Load() {
		t.Fatalf("expected SIGHUP to trigger reload and update target weight to 10")
	}

	if holder.Get().Upstreams[0].Targets[0].Weight != 10 {
		t.Errorf("expected weight 10, got %d", holder.Get().Upstreams[0].Targets[0].Weight)
	}
}

func TestWatcherDryRunRejectionPreservesActiveConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nexusgate.yaml")

	validYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: 3
`
	if err := os.WriteFile(configPath, []byte(validYAML), 0600); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	initialCfg, _ := ParseFile(configPath)
	ApplyDefaults(initialCfg)
	holder := NewConfigHolder(initialCfg)

	var errorReported atomic.Bool
	watcher := NewWatcher(configPath, holder,
		WithErrorHandler(func(err error) {
			errorReported.Store(true)
		}),
	)

	// Overwrite file with invalid YAML (negative weight)
	invalidYAML := `
version: "1.0"
listener:
  port: 8080
upstreams:
  - id: "pool-1"
    algorithm: "swwr"
    targets:
      - url: "http://10.0.0.1:8080"
        weight: -99
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	// Trigger reload
	_, err := watcher.Reload()
	if err == nil {
		t.Fatalf("expected Reload() to return error for invalid configuration")
	}
	if !errorReported.Load() {
		t.Errorf("expected error handler to be invoked")
	}

	// Invariant: active configuration MUST be 100% preserved
	active := holder.Get()
	if active.Upstreams[0].Targets[0].Weight != 3 {
		t.Errorf("active configuration was corrupted: expected weight 3, got %d", active.Upstreams[0].Targets[0].Weight)
	}
}
