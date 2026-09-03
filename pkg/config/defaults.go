package config

import (
	"nexusgate/internal/platform"
	"os"
	"strconv"
	"time"
)

const (
	DefaultVersion           = "1.0"
	DefaultHost              = "0.0.0.0"
	DefaultPort              = 8080
	DefaultReadTimeout       = 5 * time.Second
	DefaultWriteTimeout      = 10 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultReadHeaderTimeout = 2 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20  // 1 MB
	DefaultMaxBodyBytes      = 10 << 20 // 10 MB

	DefaultAlgorithm           = "swwr"
	DefaultTargetWeight        = 1
	DefaultHealthCheckPath     = "/healthz"
	DefaultHealthCheckInterval = 10 * time.Second
	DefaultHealthCheckTimeout  = 2 * time.Second
	DefaultHealthyThreshold    = 2
	DefaultUnhealthyThreshold  = 3

	DefaultCBConsecutiveFailures  = 5
	DefaultCBFailureRateThreshold = 0.5
	DefaultCBMinRequests          = 10
	DefaultCBResetTimeout         = 15 * time.Second
	DefaultCBHalfOpenMaxRequests  = 3

	DefaultRingBufferSize   = 65536
	DefaultIdleSleepTimeout = 3 * time.Second
	DefaultSnapshotInterval = 1 * time.Second
	DefaultChaosHeaderKey   = "X-NexusGate-Chaos"
)

// ApplyDefaults populates any unset or zero fields in GatewayConfig with production defaults.
func ApplyDefaults(cfg *GatewayConfig) {
	if cfg == nil {
		return
	}

	if cfg.Version == "" {
		cfg.Version = DefaultVersion
	}

	// Listener defaults
	if cfg.Listener.Host == "" {
		cfg.Listener.Host = DefaultHost
	}
	if cfg.Listener.Port == 0 {
		cfg.Listener.Port = DefaultPort
	}
	if cfg.Listener.ReadTimeout == 0 {
		cfg.Listener.ReadTimeout = DefaultReadTimeout
	}
	if cfg.Listener.WriteTimeout == 0 {
		cfg.Listener.WriteTimeout = DefaultWriteTimeout
	}
	if cfg.Listener.IdleTimeout == 0 {
		cfg.Listener.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.Listener.ReadHeaderTimeout == 0 {
		cfg.Listener.ReadHeaderTimeout = DefaultReadHeaderTimeout
	}
	if cfg.Listener.MaxHeaderBytes == 0 {
		cfg.Listener.MaxHeaderBytes = DefaultMaxHeaderBytes
	}
	if cfg.Listener.MaxBodyBytes == 0 {
		cfg.Listener.MaxBodyBytes = DefaultMaxBodyBytes
	}

	// Upstream defaults
	for i := range cfg.Upstreams {
		u := &cfg.Upstreams[i]
		if u.Algorithm == "" {
			u.Algorithm = DefaultAlgorithm
		}
		for j := range u.Targets {
			t := &u.Targets[j]
			if t.Weight == 0 {
				t.Weight = DefaultTargetWeight
			}
		}
		if u.HealthCheck != nil && u.HealthCheck.Enabled {
			if u.HealthCheck.Path == "" {
				u.HealthCheck.Path = DefaultHealthCheckPath
			}
			if u.HealthCheck.Interval == 0 {
				u.HealthCheck.Interval = DefaultHealthCheckInterval
			}
			if u.HealthCheck.Timeout == 0 {
				u.HealthCheck.Timeout = DefaultHealthCheckTimeout
			}
			if u.HealthCheck.HealthyThreshold == 0 {
				u.HealthCheck.HealthyThreshold = DefaultHealthyThreshold
			}
			if u.HealthCheck.UnhealthyThreshold == 0 {
				u.HealthCheck.UnhealthyThreshold = DefaultUnhealthyThreshold
			}
		}
	}

	// Resilience / Circuit breaker defaults
	if cfg.Resilience.CircuitBreaker.Enabled {
		cb := &cfg.Resilience.CircuitBreaker
		if cb.ConsecutiveFailures == 0 {
			cb.ConsecutiveFailures = DefaultCBConsecutiveFailures
		}
		if cb.FailureRateThreshold == 0 {
			cb.FailureRateThreshold = DefaultCBFailureRateThreshold
		}
		if cb.MinRequests == 0 {
			cb.MinRequests = DefaultCBMinRequests
		}
		if cb.ResetTimeout == 0 {
			cb.ResetTimeout = DefaultCBResetTimeout
		}
		if cb.HalfOpenMaxRequests == 0 {
			cb.HalfOpenMaxRequests = DefaultCBHalfOpenMaxRequests
		}
	}

	// Telemetry defaults
	if cfg.Telemetry.UDSPath == "" {
		cfg.Telemetry.UDSPath = platform.DefaultSocketPath()
	}
	if cfg.Telemetry.RingBufferSize == 0 {
		cfg.Telemetry.RingBufferSize = DefaultRingBufferSize
	}
	if cfg.Telemetry.IdleSleepTimeout == 0 {
		cfg.Telemetry.IdleSleepTimeout = DefaultIdleSleepTimeout
	}
	if cfg.Telemetry.SnapshotInterval == 0 {
		cfg.Telemetry.SnapshotInterval = DefaultSnapshotInterval
	}

	// Chaos defaults
	if cfg.Chaos.HeaderKey == "" {
		cfg.Chaos.HeaderKey = DefaultChaosHeaderKey
	}
}

// ApplyEnvironmentOverrides applies environment variable overrides to GatewayConfig.
func ApplyEnvironmentOverrides(cfg *GatewayConfig) {
	if cfg == nil {
		return
	}

	if portStr := os.Getenv("NEXUSGATE_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.Listener.Port = p
		}
	}

	if host := os.Getenv("NEXUSGATE_HOST"); host != "" {
		cfg.Listener.Host = host
	}

	if uds := os.Getenv("NEXUSGATE_UDS_PATH"); uds != "" {
		cfg.Telemetry.UDSPath = uds
	}

	if ringStr := os.Getenv("NEXUSGATE_RING_BUFFER_SIZE"); ringStr != "" {
		if sz, err := strconv.Atoi(ringStr); err == nil && sz > 0 {
			cfg.Telemetry.RingBufferSize = sz
		}
	}
}

// NewDefaultConfig returns a completely initialized GatewayConfig populated with defaults.
func NewDefaultConfig() *GatewayConfig {
	cfg := &GatewayConfig{}
	ApplyDefaults(cfg)
	ApplyEnvironmentOverrides(cfg)
	return cfg
}
