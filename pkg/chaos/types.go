package chaos

import (
	"context"
	"net/http"
	"time"

	"nexusgate/pkg/config"
)

// Developer query parameter control keys.
const (
	QueryParamDelay     = "__delay"
	QueryParamStatus    = "__status"
	QueryParamBody      = "__body"
	QueryParamDrop      = "__drop"
	QueryParamFaultRate = "__fault_rate"
)

// Chaos and mock HTTP header keys.
const (
	// HeaderChaosKey is the administrative authentication header required to unlock chaos features.
	HeaderChaosKey = "X-NexusGate-Chaos-Key"

	// HeaderChaosInjected indicates that the response was synthetically generated or altered by chaos.
	HeaderChaosInjected = "X-NexusGate-Chaos-Injected"

	// HeaderChaosReason specifies why chaos was applied (e.g., "forced-status", "random-fault").
	HeaderChaosReason = "X-NexusGate-Chaos-Reason"

	// HeaderMock indicates that the response was produced by a static route mock rule.
	HeaderMock = "X-NexusGate-Mock"
)

// Operational boundary defaults.
const (
	DefaultMaxDelay             = 30 * time.Second
	DefaultMaxBodyBytes         = 64 * 1024 // 64 KB memory clamp
	DefaultMaxConcurrentDelays  = 128
)

type contextKey string

const (
	// ContextKeyChaosParams stores parsed ChaosParams in request context.
	ContextKeyChaosParams = contextKey("nexusgate.chaos.params")

	// ContextKeyMockRule stores active MockRule in request context.
	ContextKeyMockRule = contextKey("nexusgate.chaos.mock_rule")
)

// MockRule represents a predefined mock response schema attached to a route.
// Aliased to config.MockResponse to guarantee 100% schema consistency.
type MockRule = config.MockResponse

// ChaosParams encapsulates extracted developer chaos parameters for an active request.
type ChaosParams struct {
	Delay     time.Duration
	Status    int
	Body      string
	Drop      bool
	FaultRate float64
	HasChaos  bool
}

// Config specifies runtime tuning, authentication, and safety limits for chaos injection.
type Config struct {
	Enabled             bool          `json:"enabled" yaml:"enabled"`
	AdminKey            string        `json:"admin_key,omitempty" yaml:"admin_key,omitempty"`
	HeaderKey           string        `json:"header_key,omitempty" yaml:"header_key,omitempty"`
	AllowedSubnets      []string      `json:"allowed_subnets,omitempty" yaml:"allowed_subnets,omitempty"`
	DefaultFailureRate  float64       `json:"failure_rate,omitempty" yaml:"failure_rate,omitempty"`
	DefaultDelay        time.Duration `json:"delay,omitempty" yaml:"delay,omitempty"`
	MaxDelay            time.Duration `json:"max_delay,omitempty" yaml:"max_delay,omitempty"`
	MaxBodyBytes        int           `json:"max_body_bytes,omitempty" yaml:"max_body_bytes,omitempty"`
	MaxConcurrentDelays int           `json:"max_concurrent_delays,omitempty" yaml:"max_concurrent_delays,omitempty"`
	StrictMode          bool          `json:"strict_mode,omitempty" yaml:"strict_mode,omitempty"`
}

// AuditEvent represents a structured telemetry event emitted when chaos or mock is executed.
type AuditEvent struct {
	Timestamp  time.Time     `json:"timestamp"`
	RouteID    string        `json:"route_id,omitempty"`
	ClientIP   string        `json:"client_ip,omitempty"`
	Action     string        `json:"action"` // "delay", "status", "drop", "mock", "random_fault", "unauthorized"
	Duration   time.Duration `json:"duration,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// AuditCountersSnapshot provides a point-in-time value copy of all atomic chaos telemetry counters.
type AuditCountersSnapshot struct {
	DelaysInjected       uint64 `json:"delays_injected"`
	StatusesInjected     uint64 `json:"statuses_injected"`
	DropsInjected        uint64 `json:"drops_injected"`
	MocksServed          uint64 `json:"mocks_served"`
	RandomFaultsInjected uint64 `json:"random_faults_injected"`
	UnauthorizedAttempts uint64 `json:"unauthorized_attempts"`
}

// AuditLogger defines the interface for recording chaos telemetry events.
type AuditLogger interface {
	LogChaosEvent(event AuditEvent)
}

// ChaosEngine defines the full contract for parsing, authorizing, injecting chaos, and serving mocks.
type ChaosEngine interface {
	// Config returns the current active configuration.
	Config() Config

	// Parse extracts and validates chaos parameters from the request URL without heap allocations.
	Parse(r *http.Request) (ChaosParams, bool)

	// Authorize evaluates security keys and subnet whitelisting for the request.
	Authorize(r *http.Request) bool

	// InjectDelay executes a non-blocking delay bounded by context cancellation.
	InjectDelay(ctx context.Context, d time.Duration) error

	// ServeStatus terminates the request, writing synthetic status code and body.
	ServeStatus(w http.ResponseWriter, r *http.Request, status int, body string)

	// ServeMock serves a predefined static mock response, returning true if handled.
	ServeMock(w http.ResponseWriter, r *http.Request, mock *MockRule) bool

	// ShouldInjectFault evaluates probabilistic failure based on the given rate.
	ShouldInjectFault(rate float64) bool

	// DropConnection terminates the client connection abruptly (TCP RST or socket close).
	DropConnection(w http.ResponseWriter) error

	// Wrap returns an HTTP middleware wrapping a handler in the chaos pipeline.
	Wrap(next http.Handler) http.Handler

	// WrapRoute returns an HTTP middleware applying route-level mock and chaos.
	WrapRoute(mock *MockRule, next http.Handler) http.Handler

	// Counters returns a snapshot of atomic event counters.
	Counters() AuditCountersSnapshot
}
