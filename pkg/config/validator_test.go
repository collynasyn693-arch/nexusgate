package config

import (
	"nexusgate/internal/platform"
	"strings"
	"testing"
	"time"
)

func TestUnprivilegedPortValidation(t *testing.T) {
	// Simulate non-root user
	cleanupNonRoot := platform.SetMockUID(10142)
	defer cleanupNonRoot()

	// Privileged ports should be rejected
	for _, port := range []int{80, 443, 800, 1023} {
		err := ValidatePort(port)
		if err == nil {
			t.Errorf("expected port %d to be rejected for non-root user", port)
		} else if !strings.Contains(err.Error(), "privileged port") {
			t.Errorf("expected privileged port error message for port %d, got %v", port, err)
		}
	}

	// Unprivileged ports should be accepted
	for _, port := range []int{1024, 1025, 8080, 8443, 65535} {
		if err := ValidatePort(port); err != nil {
			t.Errorf("expected port %d to be allowed for non-root user, got %v", port, err)
		}
	}

	// Out of bounds ports should be rejected
	for _, port := range []int{-1, 0, 65536, 70000} {
		if err := ValidatePort(port); err == nil {
			t.Errorf("expected out-of-bounds port %d to be rejected", port)
		}
	}
}

func TestRootPortValidation(t *testing.T) {
	// Simulate root user (UID 0)
	cleanupRoot := platform.SetMockUID(0)
	defer cleanupRoot()

	// Privileged ports are allowed for root
	for _, port := range []int{80, 443, 1023, 8080} {
		if err := ValidatePort(port); err != nil {
			t.Errorf("expected port %d to be allowed for root user, got %v", port, err)
		}
	}

	// Port 0 and negative should still be rejected
	if err := ValidatePort(0); err == nil {
		t.Errorf("expected port 0 to be rejected for root")
	}
}

func TestHostValidation(t *testing.T) {
	validHosts := []string{
		"",
		"0.0.0.0",
		"127.0.0.1",
		"::",
		"::1",
		"localhost",
		"edge.local",
		"my-gateway.internal",
	}

	for _, host := range validHosts {
		if err := ValidateHost(host); err != nil {
			t.Errorf("expected host %q to be valid, got %v", host, err)
		}
	}

	invalidHosts := []string{
		strings.Repeat("a", 254), // > 253 characters
		"invalid..host",
	}

	for _, host := range invalidHosts {
		if err := ValidateHost(host); err == nil {
			t.Errorf("expected invalid host %q to be rejected", host)
		}
	}
}

func TestListenerValidation(t *testing.T) {
	cleanupNonRoot := platform.SetMockUID(10142)
	defer cleanupNonRoot()

	validListener := &ListenerConfig{
		Host:              "0.0.0.0",
		Port:              8080,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}
	if err := ValidateListener(validListener); err != nil {
		t.Fatalf("expected valid listener to pass, got: %v", err)
	}

	// Negative timeouts
	badListener := *validListener
	badListener.ReadTimeout = -1 * time.Second
	if err := ValidateListener(&badListener); err == nil {
		t.Fatalf("expected error for negative read timeout")
	}
}

func TestRouteValidationCollisionsAndSemantics(t *testing.T) {
	cleanupNonRoot := platform.SetMockUID(10142)
	defer cleanupNonRoot()

	baseUpstreams := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets: []TargetConfig{
				{URL: "http://10.0.0.1:8000", Weight: 1},
			},
		},
	}

	// 1. Duplicate Route ID
	dupIDRoutes := []RouteConfig{
		{ID: "r1", Path: "/a", UpstreamID: "u1"},
		{ID: "r1", Path: "/b", UpstreamID: "u1"},
	}
	if err := ValidateRoutes(dupIDRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for duplicate route IDs")
	}

	// 2. Missing Upstream Reference
	missingUpstreamRoutes := []RouteConfig{
		{ID: "r1", Path: "/a", UpstreamID: "non-existent"},
	}
	if err := ValidateRoutes(missingUpstreamRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for missing upstream reference")
	}

	// 3. Mock Route without Upstream (Allowed)
	mockRoutes := []RouteConfig{
		{
			ID:   "r-mock",
			Path: "/mock",
			Mock: &MockResponse{
				Enabled:    true,
				StatusCode: 200,
				Body:       "OK",
			},
		},
	}
	if err := ValidateRoutes(mockRoutes, baseUpstreams, 8080, "0.0.0.0"); err != nil {
		t.Errorf("expected mock route without upstream to be valid, got: %v", err)
	}

	// 4. Disjoint Methods on Same Path (Allowed)
	disjointRoutes := []RouteConfig{
		{ID: "r1", Path: "/users", Methods: []string{"GET"}, UpstreamID: "u1"},
		{ID: "r2", Path: "/users", Methods: []string{"POST"}, UpstreamID: "u1"},
	}
	if err := ValidateRoutes(disjointRoutes, baseUpstreams, 8080, "0.0.0.0"); err != nil {
		t.Errorf("expected disjoint methods on same path to be valid, got: %v", err)
	}

	// 5. Overlapping Methods Collision
	overlappingRoutes := []RouteConfig{
		{ID: "r1", Path: "/users", Methods: []string{"GET", "POST"}, UpstreamID: "u1"},
		{ID: "r2", Path: "/users", Methods: []string{"POST"}, UpstreamID: "u1"},
	}
	if err := ValidateRoutes(overlappingRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for overlapping route methods on same path")
	}

	// 6. Wildcard Method Collision with Specific Method
	wildcardConflictRoutes := []RouteConfig{
		{ID: "r1", Path: "/users", Methods: []string{"GET"}, UpstreamID: "u1"},
		{ID: "r2", Path: "/users", Methods: nil, UpstreamID: "u1"}, // all methods
	}
	if err := ValidateRoutes(wildcardConflictRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for wildcard method collision with specific method")
	}

	// 7. Invalid Path (not starting with /)
	badPathRoutes := []RouteConfig{
		{ID: "r1", Path: "users", UpstreamID: "u1"},
	}
	if err := ValidateRoutes(badPathRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for path not starting with /")
	}

	// 8. Invalid StripPrefix (not prefix of path)
	badStripPrefixRoutes := []RouteConfig{
		{ID: "r1", Path: "/api/v1", StripPrefix: "/v2", UpstreamID: "u1"},
	}
	if err := ValidateRoutes(badStripPrefixRoutes, baseUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for strip_prefix that is not prefix of path")
	}
}

func TestUpstreamValidationAndSelfLoops(t *testing.T) {
	// 1. Invalid Algorithm
	badAlgo := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "magic_balancer",
			Targets:   []TargetConfig{{URL: "http://10.0.0.1:8000", Weight: 1}},
		},
	}
	if err := ValidateUpstreams(badAlgo, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for invalid algorithm")
	}

	// 2. Empty Targets
	noTargets := []UpstreamConfig{
		{ID: "u1", Algorithm: "swwr", Targets: []TargetConfig{}},
	}
	if err := ValidateUpstreams(noTargets, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for 0 targets")
	}

	// 3. Duplicate Targets in Same Pool
	dupTargets := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets: []TargetConfig{
				{URL: "http://10.0.0.1:8000", Weight: 1},
				{URL: "http://10.0.0.1:8000", Weight: 2},
			},
		},
	}
	if err := ValidateUpstreams(dupTargets, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for duplicate targets")
	}

	// 4. Invalid Scheme (not http/https)
	badScheme := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets:   []TargetConfig{{URL: "ftp://10.0.0.1:21", Weight: 1}},
		},
	}
	if err := ValidateUpstreams(badScheme, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for non-http scheme")
	}

	// 5. Zero or Negative Target Weight
	badWeight := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets:   []TargetConfig{{URL: "http://10.0.0.1:8000", Weight: 0}},
		},
	}
	if err := ValidateUpstreams(badWeight, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for target weight < 1")
	}

	// 6. Self-Loop Detection (target points to listener port and host)
	selfLoopUpstreams := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets:   []TargetConfig{{URL: "http://127.0.0.1:8080/path", Weight: 1}},
		},
	}
	if err := ValidateUpstreams(selfLoopUpstreams, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error for self-referential loop target pointing to gateway listener")
	}

	// 7. Health Check timeout > interval
	badHC := []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets:   []TargetConfig{{URL: "http://10.0.0.1:8000", Weight: 1}},
			HealthCheck: &HealthCheckConfig{
				Enabled:  true,
				Path:     "/healthz",
				Interval: 2 * time.Second,
				Timeout:  5 * time.Second, // timeout > interval
			},
		},
	}
	if err := ValidateUpstreams(badHC, 8080, "0.0.0.0"); err == nil {
		t.Errorf("expected error when health check timeout > interval")
	}
}

func TestTelemetryAndResilienceValidation(t *testing.T) {
	// Ring buffer not a power of 2
	badTelemetry := &TelemetryConfig{
		RingBufferSize: 50000,
	}
	if err := ValidateTelemetry(badTelemetry); err == nil {
		t.Errorf("expected error for non-power-of-2 ring buffer size")
	}

	goodTelemetry := &TelemetryConfig{
		RingBufferSize: 65536,
	}
	if err := ValidateTelemetry(goodTelemetry); err != nil {
		t.Errorf("expected 65536 to be valid power of 2, got: %v", err)
	}

	// Circuit breaker failure rate > 1.0
	badCB := &CircuitBreakerConfig{
		FailureRateThreshold: 1.5,
	}
	if err := validateCircuitBreaker(badCB, "test"); err == nil {
		t.Errorf("expected error for failure rate threshold > 1.0")
	}
}
