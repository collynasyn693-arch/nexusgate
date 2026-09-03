package config

import (
	"os"
	"testing"
	"time"
)

func TestApplyDefaults(t *testing.T) {
	cfg := &GatewayConfig{
		Upstreams: []UpstreamConfig{
			{
				ID: "up1",
				Targets: []TargetConfig{
					{URL: "http://10.0.0.1:8080"},
				},
				HealthCheck: &HealthCheckConfig{
					Enabled: true,
				},
			},
		},
		Resilience: ResilienceConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled: true,
			},
		},
	}

	ApplyDefaults(cfg)

	if cfg.Version != DefaultVersion {
		t.Errorf("expected version %q, got %q", DefaultVersion, cfg.Version)
	}
	if cfg.Listener.Host != DefaultHost {
		t.Errorf("expected host %q, got %q", DefaultHost, cfg.Listener.Host)
	}
	if cfg.Listener.Port != DefaultPort {
		t.Errorf("expected port %d, got %d", DefaultPort, cfg.Listener.Port)
	}
	if cfg.Listener.ReadTimeout != DefaultReadTimeout {
		t.Errorf("expected read timeout %v, got %v", DefaultReadTimeout, cfg.Listener.ReadTimeout)
	}
	if cfg.Listener.ReadHeaderTimeout != DefaultReadHeaderTimeout {
		t.Errorf("expected read header timeout %v, got %v", DefaultReadHeaderTimeout, cfg.Listener.ReadHeaderTimeout)
	}
	if cfg.Listener.MaxHeaderBytes != DefaultMaxHeaderBytes {
		t.Errorf("expected max header bytes %d, got %d", DefaultMaxHeaderBytes, cfg.Listener.MaxHeaderBytes)
	}
	if cfg.Listener.MaxBodyBytes != DefaultMaxBodyBytes {
		t.Errorf("expected max body bytes %d, got %d", DefaultMaxBodyBytes, cfg.Listener.MaxBodyBytes)
	}
	if cfg.Upstreams[0].Algorithm != DefaultAlgorithm {
		t.Errorf("expected algorithm %q, got %q", DefaultAlgorithm, cfg.Upstreams[0].Algorithm)
	}
	if cfg.Upstreams[0].Targets[0].Weight != DefaultTargetWeight {
		t.Errorf("expected target weight %d, got %d", DefaultTargetWeight, cfg.Upstreams[0].Targets[0].Weight)
	}
	if cfg.Upstreams[0].HealthCheck.Path != DefaultHealthCheckPath {
		t.Errorf("expected health check path %q, got %q", DefaultHealthCheckPath, cfg.Upstreams[0].HealthCheck.Path)
	}
	if cfg.Resilience.CircuitBreaker.ResetTimeout != DefaultCBResetTimeout {
		t.Errorf("expected CB reset timeout %v, got %v", DefaultCBResetTimeout, cfg.Resilience.CircuitBreaker.ResetTimeout)
	}
	if cfg.Telemetry.RingBufferSize != DefaultRingBufferSize {
		t.Errorf("expected ring buffer size %d, got %d", DefaultRingBufferSize, cfg.Telemetry.RingBufferSize)
	}
	if cfg.Chaos.HeaderKey != DefaultChaosHeaderKey {
		t.Errorf("expected chaos header key %q, got %q", DefaultChaosHeaderKey, cfg.Chaos.HeaderKey)
	}
}

func TestApplyDefaultsDoesNotOverwriteCustomValues(t *testing.T) {
	cfg := &GatewayConfig{
		Version: "2.0",
		Listener: ListenerConfig{
			Host:        "127.0.0.1",
			Port:        9000,
			ReadTimeout: 42 * time.Second,
		},
	}

	ApplyDefaults(cfg)

	if cfg.Version != "2.0" {
		t.Errorf("expected custom version 2.0 to be retained, got %q", cfg.Version)
	}
	if cfg.Listener.Host != "127.0.0.1" {
		t.Errorf("expected custom host 127.0.0.1 to be retained, got %q", cfg.Listener.Host)
	}
	if cfg.Listener.Port != 9000 {
		t.Errorf("expected custom port 9000 to be retained, got %d", cfg.Listener.Port)
	}
	if cfg.Listener.ReadTimeout != 42*time.Second {
		t.Errorf("expected custom read timeout 42s to be retained, got %v", cfg.Listener.ReadTimeout)
	}
	// Unset write timeout should still be defaulted
	if cfg.Listener.WriteTimeout != DefaultWriteTimeout {
		t.Errorf("expected unset write timeout to be filled with default, got %v", cfg.Listener.WriteTimeout)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	os.Setenv("NEXUSGATE_PORT", "9999")
	os.Setenv("NEXUSGATE_HOST", "192.168.1.50")
	os.Setenv("NEXUSGATE_UDS_PATH", "/tmp/env_test.sock")
	os.Setenv("NEXUSGATE_RING_BUFFER_SIZE", "16384")
	defer func() {
		os.Unsetenv("NEXUSGATE_PORT")
		os.Unsetenv("NEXUSGATE_HOST")
		os.Unsetenv("NEXUSGATE_UDS_PATH")
		os.Unsetenv("NEXUSGATE_RING_BUFFER_SIZE")
	}()

	cfg := NewDefaultConfig()

	if cfg.Listener.Port != 9999 {
		t.Errorf("expected port overridden to 9999, got %d", cfg.Listener.Port)
	}
	if cfg.Listener.Host != "192.168.1.50" {
		t.Errorf("expected host overridden to 192.168.1.50, got %q", cfg.Listener.Host)
	}
	if cfg.Telemetry.UDSPath != "/tmp/env_test.sock" {
		t.Errorf("expected socket path overridden to /tmp/env_test.sock, got %q", cfg.Telemetry.UDSPath)
	}
	if cfg.Telemetry.RingBufferSize != 16384 {
		t.Errorf("expected ring buffer size overridden to 16384, got %d", cfg.Telemetry.RingBufferSize)
	}
}
