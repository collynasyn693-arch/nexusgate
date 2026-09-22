package balancer

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// BalancingAlgorithm represents the upstream load balancing strategy name.
type BalancingAlgorithm string

const (
	// AlgorithmRoundRobin distributes requests sequentially across healthy targets.
	AlgorithmRoundRobin BalancingAlgorithm = "round_robin"
	// AlgorithmSWWR is the Nginx Smooth Weighted Round-Robin algorithm.
	AlgorithmSWWR BalancingAlgorithm = "swwr"
	// AlgorithmSWRR is the canonical alias for Smooth Weighted Round-Robin.
	AlgorithmSWRR BalancingAlgorithm = "swrr"
	// AlgorithmP2C selects between two random targets using Power-of-Two-Choices least loaded.
	AlgorithmP2C BalancingAlgorithm = "p2c"
	// AlgorithmPeakEWMA scores backends based on moving average latency with exponential time decay.
	AlgorithmPeakEWMA BalancingAlgorithm = "peak_ewma"
	// AlgorithmIPHash hashes client IP addresses for deterministic sticky upstream routing.
	AlgorithmIPHash BalancingAlgorithm = "ip_hash"
)

// Sentinel error definitions for balancer and target pool operations.
var (
	// ErrNoHealthyBackends is returned when no healthy targets are available in the pool.
	ErrNoHealthyBackends = errors.New("nexusgate: no healthy backends available in pool")
	// ErrBackendDraining is returned when an operation is attempted on a draining backend.
	ErrBackendDraining = errors.New("nexusgate: target backend is draining connections")
	// ErrBackendOverloaded is returned when a backend exceeds its max_conns limit.
	ErrBackendOverloaded = errors.New("nexusgate: target backend active connections saturated")
	// ErrBackendNotFound is returned when a target URL is not present in the pool.
	ErrBackendNotFound = errors.New("nexusgate: target backend not found in pool")
	// ErrInvalidAlgorithm is returned when an unrecognized balancing algorithm is requested.
	ErrInvalidAlgorithm = errors.New("nexusgate: unsupported balancing algorithm")
	// ErrDrainTimeout is returned when draining does not complete within the specified timeout.
	ErrDrainTimeout = errors.New("nexusgate: backend drain timed out with active connections")
)

// Balancer defines the core interface implemented by all load balancing algorithms.
type Balancer interface {
	// Name returns the balancing algorithm identifier.
	Name() string
	// Select chooses a healthy target backend according to the algorithm strategy.
	Select(ctx context.Context, req *http.Request) (*Backend, error)
	// UpdateTargets refreshes the active candidate set of healthy backends.
	UpdateTargets(targets []*Backend)
}

// Backend represents an upstream destination target node with atomic runtime tracking.
type Backend struct {
	// Read-mostly configuration metadata
	URL              *url.URL
	RawURL           string
	ConfiguredWeight int64
	MaxConns         int64

	// Write-hot atomic runtime state (64-bit aligned for ARM64)
	activeConns     atomic.Int64
	totalRequests   atomic.Int64
	totalErrors     atomic.Int64
	effectiveWeight atomic.Int64
	latencyEWMA     atomic.Int64 // nanos
	lastUpdateNanos atomic.Int64 // unix nanos
	draining        atomic.Bool
	healthy         atomic.Bool

	// Drain synchronization
	drainDone chan struct{}
	drainOnce sync.Once
	drainMu   sync.Mutex
}

// TargetPool manages a dynamic pool of upstream backend targets for a route or service.
type TargetPool interface {
	// ID returns the unique identifier of the target pool.
	ID() string
	// Get retrieves a backend target by its raw URL string.
	Get(rawURL string) (*Backend, bool)
	// Add registers a new backend target into the pool.
	Add(backend *Backend) error
	// Remove deregisters a backend target from the pool by its raw URL string.
	Remove(rawURL string) bool
	// Drain initiates graceful connection draining for a target with a timeout.
	Drain(ctx context.Context, rawURL string, timeout time.Duration) error
	// Targets returns a snapshot of all registered backend targets in the pool.
	Targets() []*Backend
	// HealthyTargets returns a snapshot of currently healthy, non-draining backend targets.
	HealthyTargets() []*Backend
	// SetHealth updates the health status of a backend target.
	SetHealth(rawURL string, healthy bool) bool
	// Balancer returns the currently active load balancer strategy.
	Balancer() Balancer
	// SetBalancer atomically swaps the load balancer strategy for this pool.
	SetBalancer(b Balancer)
	// Select routes a request to an upstream backend using the active balancer.
	Select(ctx context.Context, req *http.Request) (*Backend, error)
}
