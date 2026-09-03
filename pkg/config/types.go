package config

import (
	"time"
)

// GatewayConfig is the root configuration structure for NexusGate.
type GatewayConfig struct {
	Version    string           `json:"version" yaml:"version"`
	Listener   ListenerConfig   `json:"listener" yaml:"listener"`
	Routes     []RouteConfig    `json:"routes" yaml:"routes"`
	Upstreams  []UpstreamConfig `json:"upstreams" yaml:"upstreams"`
	Resilience ResilienceConfig `json:"resilience" yaml:"resilience"`
	Telemetry  TelemetryConfig  `json:"telemetry" yaml:"telemetry"`
	Chaos      ChaosConfig      `json:"chaos" yaml:"chaos"`
}

// ListenerConfig defines inbound HTTP listener settings.
type ListenerConfig struct {
	Host              string        `json:"host" yaml:"host"`
	Port              int           `json:"port" yaml:"port"`
	ReadTimeout       time.Duration `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout      time.Duration `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout       time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
	ReadHeaderTimeout time.Duration `json:"read_header_timeout" yaml:"read_header_timeout"`
	MaxHeaderBytes    int           `json:"max_header_bytes" yaml:"max_header_bytes"`
	MaxBodyBytes      int64         `json:"max_body_bytes,omitempty" yaml:"max_body_bytes,omitempty"`
	TLS               *TLSConfig    `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// TLSConfig defines optional TLS parameters for Stage 09.
type TLSConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	CertFile string `json:"cert_file" yaml:"cert_file"`
	KeyFile  string `json:"key_file" yaml:"key_file"`
}

// RouteConfig specifies routing match rules and target destinations.
type RouteConfig struct {
	ID          string            `json:"id" yaml:"id"`
	Host        string            `json:"host,omitempty" yaml:"host,omitempty"`
	Path        string            `json:"path" yaml:"path"`
	Methods     []string          `json:"methods" yaml:"methods"`
	StripPrefix string            `json:"strip_prefix,omitempty" yaml:"strip_prefix,omitempty"`
	UpstreamID  string            `json:"upstream_id,omitempty" yaml:"upstream_id,omitempty"`
	Headers     map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Mock        *MockResponse     `json:"mock,omitempty" yaml:"mock,omitempty"`
}

// MockResponse configures static mock responses for testing.
type MockResponse struct {
	Enabled    bool              `json:"enabled" yaml:"enabled"`
	StatusCode int               `json:"status_code" yaml:"status_code"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body       string            `json:"body,omitempty" yaml:"body,omitempty"`
}

// UpstreamConfig defines a pool of backend targets and balancing policy.
type UpstreamConfig struct {
	ID             string                `json:"id" yaml:"id"`
	Algorithm      string                `json:"algorithm" yaml:"algorithm"` // swwr, round_robin, p2c, peak_ewma, ip_hash
	Targets        []TargetConfig        `json:"targets" yaml:"targets"`
	HealthCheck    *HealthCheckConfig    `json:"health_check,omitempty" yaml:"health_check,omitempty"`
	CircuitBreaker *CircuitBreakerConfig `json:"circuit_breaker,omitempty" yaml:"circuit_breaker,omitempty"`
}

// TargetConfig defines an individual backend target node.
type TargetConfig struct {
	URL      string `json:"url" yaml:"url"`
	Weight   int    `json:"weight" yaml:"weight"`
	MaxConns int    `json:"max_conns,omitempty" yaml:"max_conns,omitempty"`
	Drain    bool   `json:"drain,omitempty" yaml:"drain,omitempty"`
}

// HealthCheckConfig defines active health probing settings.
type HealthCheckConfig struct {
	Enabled            bool          `json:"enabled" yaml:"enabled"`
	Path               string        `json:"path" yaml:"path"`
	Interval           time.Duration `json:"interval" yaml:"interval"`
	Timeout            time.Duration `json:"timeout" yaml:"timeout"`
	HealthyThreshold   int           `json:"healthy_threshold" yaml:"healthy_threshold"`
	UnhealthyThreshold int           `json:"unhealthy_threshold" yaml:"unhealthy_threshold"`
}

// ResilienceConfig specifies global circuit breaking defaults.
type ResilienceConfig struct {
	CircuitBreaker CircuitBreakerConfig `json:"circuit_breaker" yaml:"circuit_breaker"`
}

// CircuitBreakerConfig defines tripping thresholds for upstreams.
type CircuitBreakerConfig struct {
	Enabled              bool          `json:"enabled" yaml:"enabled"`
	ConsecutiveFailures  int           `json:"consecutive_failures" yaml:"consecutive_failures"`
	FailureRateThreshold float64       `json:"failure_rate_threshold" yaml:"failure_rate_threshold"`
	MinRequests          int           `json:"min_requests" yaml:"min_requests"`
	ResetTimeout         time.Duration `json:"reset_timeout" yaml:"reset_timeout"`
	HalfOpenMaxRequests  int           `json:"half_open_max_requests" yaml:"half_open_max_requests"`
}

// TelemetryConfig configures internal observability and IPC socket.
type TelemetryConfig struct {
	UDSPath          string        `json:"uds_path" yaml:"uds_path"`
	RingBufferSize   int           `json:"ring_buffer_size" yaml:"ring_buffer_size"`
	IdleSleepTimeout time.Duration `json:"idle_sleep_timeout" yaml:"idle_sleep_timeout"`
	SnapshotInterval time.Duration `json:"snapshot_interval" yaml:"snapshot_interval"`
}

// ChaosConfig sets developer chaos injection parameters.
type ChaosConfig struct {
	Enabled        bool          `json:"enabled" yaml:"enabled"`
	HeaderKey      string        `json:"header_key,omitempty" yaml:"header_key,omitempty"`
	AllowedSubnets []string      `json:"allowed_subnets,omitempty" yaml:"allowed_subnets,omitempty"`
	FailureRate    float64       `json:"failure_rate,omitempty" yaml:"failure_rate,omitempty"`
	Delay          time.Duration `json:"delay,omitempty" yaml:"delay,omitempty"`
}

// Clone creates a deep copy of GatewayConfig to guarantee immutability across atomic swaps.
func (c *GatewayConfig) Clone() *GatewayConfig {
	if c == nil {
		return nil
	}
	cpy := *c

	// Deep copy listener
	if c.Listener.TLS != nil {
		tlsCpy := *c.Listener.TLS
		cpy.Listener.TLS = &tlsCpy
	}

	// Deep copy routes
	if c.Routes != nil {
		cpy.Routes = make([]RouteConfig, len(c.Routes))
		for i, r := range c.Routes {
			rc := r
			if r.Methods != nil {
				rc.Methods = make([]string, len(r.Methods))
				copy(rc.Methods, r.Methods)
			}
			if r.Headers != nil {
				rc.Headers = make(map[string]string, len(r.Headers))
				for k, v := range r.Headers {
					rc.Headers[k] = v
				}
			}
			if r.Mock != nil {
				mc := *r.Mock
				if r.Mock.Headers != nil {
					mc.Headers = make(map[string]string, len(r.Mock.Headers))
					for k, v := range r.Mock.Headers {
						mc.Headers[k] = v
					}
				}
				rc.Mock = &mc
			}
			cpy.Routes[i] = rc
		}
	}

	// Deep copy upstreams
	if c.Upstreams != nil {
		cpy.Upstreams = make([]UpstreamConfig, len(c.Upstreams))
		for i, u := range c.Upstreams {
			uc := u
			if u.Targets != nil {
				uc.Targets = make([]TargetConfig, len(u.Targets))
				copy(uc.Targets, u.Targets)
			}
			if u.HealthCheck != nil {
				hc := *u.HealthCheck
				uc.HealthCheck = &hc
			}
			if u.CircuitBreaker != nil {
				cb := *u.CircuitBreaker
				uc.CircuitBreaker = &cb
			}
			cpy.Upstreams[i] = uc
		}
	}

	// Deep copy chaos
	if c.Chaos.AllowedSubnets != nil {
		cpy.Chaos.AllowedSubnets = make([]string, len(c.Chaos.AllowedSubnets))
		copy(cpy.Chaos.AllowedSubnets, c.Chaos.AllowedSubnets)
	}

	return &cpy
}
