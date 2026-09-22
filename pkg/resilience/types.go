package resilience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// State represents the operational state of a circuit breaker.
type State int32

const (
	// StateClosed allows traffic to flow normally through to the upstream target.
	StateClosed State = 0
	// StateHalfOpen allows a strictly bounded number of canary trial requests through.
	StateHalfOpen State = 1
	// StateOpen fails fast and short-circuits traffic to a synthetic fallback response.
	StateOpen State = 2
)

// String returns the human-readable name of the circuit breaker state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateHalfOpen:
		return "Half-Open"
	case StateOpen:
		return "Open"
	default:
		return fmt.Sprintf("Unknown(%d)", int32(s))
	}
}

// MarshalJSON serializes the State to its lowercase JSON string representation.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// Sentinel error definitions for resilience and circuit breaking operations.
var (
	// ErrCircuitOpen is returned when a request is rejected because the circuit is Open.
	ErrCircuitOpen = errors.New("nexusgate: circuit breaker is open")
	// ErrTooManyCanaryRequests is returned when the Half-Open concurrent canary limit is reached.
	ErrTooManyCanaryRequests = errors.New("nexusgate: half-open canary concurrency limit reached")
	// ErrInvalidStateTransition is returned when an illegal FSM transition is attempted.
	ErrInvalidStateTransition = errors.New("nexusgate: invalid circuit breaker state transition")
	// ErrBreakerNotFound is returned when querying an unregistered circuit breaker name.
	ErrBreakerNotFound = errors.New("nexusgate: circuit breaker not found in registry")
	// ErrInvalidConfig is returned when configuration parameters fail validation.
	ErrInvalidConfig = errors.New("nexusgate: invalid resilience configuration")
	// ErrProberStopped is returned when an active health check is invoked on a stopped prober.
	ErrProberStopped = errors.New("nexusgate: health prober is stopped")
)

// Counts captures a point-in-time snapshot of circuit breaker traffic metrics.
type Counts struct {
	TotalRequests       int64   `json:"total_requests"`
	Successes           int64   `json:"successes"`
	Failures            int64   `json:"failures"`
	ConsecutiveFailures int64   `json:"consecutive_failures"`
	ConsecutiveSuccess  int64   `json:"consecutive_successes"`
	FailureRate         float64 `json:"failure_rate"`
}

// Config specifies the runtime tuning parameters for a CircuitBreaker.
type Config struct {
	// Enabled toggles circuit breaking functionality.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// ConsecutiveFailures is the number of sequential failures required to trip Closed -> Open.
	ConsecutiveFailures int `json:"consecutive_failures" yaml:"consecutive_failures"`

	// FailureRateThreshold is the percentage (0.0 to 1.0) of failed requests required to trip Closed -> Open.
	FailureRateThreshold float64 `json:"failure_rate_threshold" yaml:"failure_rate_threshold"`

	// MinRequests is the minimum request volume required in the sliding window before
	// the percentage-based FailureRateThreshold is evaluated.
	MinRequests int `json:"min_requests" yaml:"min_requests"`

	// ResetTimeout is the cooldown duration the breaker stays Open before transitioning to Half-Open.
	ResetTimeout time.Duration `json:"reset_timeout" yaml:"reset_timeout"`

	// HalfOpenMaxRequests is the maximum concurrent canary trial requests admitted in Half-Open.
	HalfOpenMaxRequests int `json:"half_open_max_requests" yaml:"half_open_max_requests"`

	// ConsecutiveSuccesses is the required number of sequential successful canaries to recover Half-Open -> Closed.
	ConsecutiveSuccesses int `json:"consecutive_successes" yaml:"consecutive_successes"`

	// Fallback is an optional custom HTTP handler executed when traffic is rejected.
	Fallback http.Handler `json:"-" yaml:"-"`
}

// DefaultConfig returns production defaults optimized for low-overhead mobile edge proxying.
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		ConsecutiveFailures:  5,
		FailureRateThreshold: 0.5,
		MinRequests:          10,
		ResetTimeout:         15 * time.Second,
		HalfOpenMaxRequests:  3,
		ConsecutiveSuccesses: 3,
	}
}

// CircuitBreaker defines the core resilience interface implemented by upstream circuit breakers.
type CircuitBreaker interface {
	// Name returns the identifier of this circuit breaker.
	Name() string

	// State returns the current State (Closed, Half-Open, Open).
	State() State

	// Allow reports whether a new request is permitted to proceed upstream.
	// On the hot path (StateClosed), this executes with sub-10ns latency and 0 heap allocations.
	Allow() bool

	// RecordSuccess records a successful upstream response.
	RecordSuccess()

	// RecordFailure records an upstream failure or error.
	RecordFailure()

	// RecordResult records a result based on boolean success.
	RecordResult(success bool)

	// Counts returns a current snapshot of sliding-window metrics.
	Counts() Counts

	// Reset resets counters and restores the circuit breaker to StateClosed.
	Reset()

	// FallbackHandler returns the HTTP handler used when the circuit is Open or canary is saturated.
	FallbackHandler() http.Handler
}

// HealthChecker defines background active target health checking contracts.
type HealthChecker interface {
	// Target returns the target URL or address string being probed.
	Target() string

	// IsHealthy reports the current health evaluation of the target backend.
	IsHealthy() bool

	// Start launches background periodic probing under the provided context.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the background prober and cleans up all timers.
	Stop()

	// CheckOnce executes a single synchronous health check probe against the target.
	CheckOnce(ctx context.Context) error
}

// Registry defines the contract for managing per-upstream circuit breaker instances.
type Registry interface {
	// Get retrieves a circuit breaker by its upstream identifier.
	Get(name string) (CircuitBreaker, bool)

	// GetOrCreate returns an existing circuit breaker or constructs and registers a new one.
	GetOrCreate(name string, cfg Config) CircuitBreaker

	// Remove deletes a circuit breaker from the registry.
	Remove(name string) bool

	// All returns a point-in-time map snapshot of all registered circuit breakers.
	All() map[string]CircuitBreaker

	// ResetAll resets all circuit breakers in the registry back to Closed state.
	ResetAll()

	// Close shuts down all registered breakers and associated background probers.
	Close()
}
