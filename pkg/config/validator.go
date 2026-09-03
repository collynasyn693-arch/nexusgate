package config

import (
	"fmt"
	"net"
	"net/url"
	"nexusgate/internal/platform"
	"strconv"
	"strings"
)

const (
	// MinUnprivilegedPort defines the lowest port number allowed for non-root users in Termux / Linux.
	MinUnprivilegedPort = 1024
	// MaxPort defines the highest valid TCP port number.
	MaxPort = 65535
)

// AllowedBalancingAlgorithms defines the set of supported load balancing algorithms.
var AllowedBalancingAlgorithms = map[string]bool{
	"swwr":        true,
	"round_robin": true,
	"p2c":         true,
	"peak_ewma":   true,
	"ip_hash":     true,
}

// AllowedHTTPMethods defines valid HTTP methods for route matching.
var AllowedHTTPMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"PATCH":   true,
	"HEAD":    true,
	"OPTIONS": true,
}

// ValidationError indicates a semantic or boundary violation in configuration.
type ValidationError struct {
	Field   string `json:"field"`
	Value   any    `json:"value"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on %s: %s (got %v)", e.Field, e.Message, e.Value)
}

// ValidatePort validates that the port is within bounds and enforces unprivileged constraints for non-root users.
func ValidatePort(port int) error {
	if port <= 0 || port > MaxPort {
		return &ValidationError{
			Field:   "listener.port",
			Value:   port,
			Message: fmt.Sprintf("port must be between 1 and %d", MaxPort),
		}
	}

	// Enforce unprivileged port floor for non-root processes
	if !platform.IsRoot() && port < MinUnprivilegedPort {
		return &ValidationError{
			Field:   "listener.port",
			Value:   port,
			Message: fmt.Sprintf("privileged port %d cannot be bound in non-root Termux userland (minimum allowed is %d)", port, MinUnprivilegedPort),
		}
	}

	return nil
}

// ValidateHost validates that the host string is a valid IP or hostname.
func ValidateHost(host string) error {
	if host == "" || host == "0.0.0.0" || host == "::" || host == "127.0.0.1" || host == "localhost" {
		return nil
	}

	// Check if valid IP address
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}

	// Hostname validation
	if len(host) > 253 {
		return &ValidationError{
			Field:   "listener.host",
			Value:   host,
			Message: "hostname cannot exceed 253 characters",
		}
	}

	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 {
			return &ValidationError{
				Field:   "listener.host",
				Value:   host,
				Message: fmt.Sprintf("invalid host component %q", part),
			}
		}
	}

	return nil
}

// ValidateListener ensures listener host, port, and timeouts satisfy constraints.
func ValidateListener(l *ListenerConfig) error {
	if err := ValidatePort(l.Port); err != nil {
		return err
	}
	if err := ValidateHost(l.Host); err != nil {
		return err
	}

	if l.ReadTimeout < 0 {
		return &ValidationError{Field: "listener.read_timeout", Value: l.ReadTimeout, Message: "timeout cannot be negative"}
	}
	if l.WriteTimeout < 0 {
		return &ValidationError{Field: "listener.write_timeout", Value: l.WriteTimeout, Message: "timeout cannot be negative"}
	}
	if l.IdleTimeout < 0 {
		return &ValidationError{Field: "listener.idle_timeout", Value: l.IdleTimeout, Message: "timeout cannot be negative"}
	}
	if l.ReadHeaderTimeout < 0 {
		return &ValidationError{Field: "listener.read_header_timeout", Value: l.ReadHeaderTimeout, Message: "timeout cannot be negative"}
	}

	return nil
}

// ValidateRoutes checks route semantics, method-aware collisions, and upstream linkage.
func ValidateRoutes(routes []RouteConfig, upstreams []UpstreamConfig, listenerPort int, listenerHost string) error {
	seenRouteIDs := make(map[string]bool)
	upstreamMap := make(map[string]bool)
	for _, u := range upstreams {
		upstreamMap[u.ID] = true
	}

	type routePathMethod struct {
		path   string
		method string
	}
	claimedEndpoints := make(map[routePathMethod]string)
	wildcardRoutes := make(map[string]string) // path -> routeID for routes matching ALL methods

	for i, r := range routes {
		fieldPrefix := fmt.Sprintf("routes[%d]", i)
		if r.ID == "" {
			return &ValidationError{Field: fieldPrefix + ".id", Value: r.ID, Message: "route ID cannot be empty"}
		}
		if seenRouteIDs[r.ID] {
			return &ValidationError{Field: fieldPrefix + ".id", Value: r.ID, Message: fmt.Sprintf("duplicate route ID %q", r.ID)}
		}
		seenRouteIDs[r.ID] = true

		if r.Path == "" || !strings.HasPrefix(r.Path, "/") {
			return &ValidationError{Field: fieldPrefix + ".path", Value: r.Path, Message: "route path must start with '/'"}
		}

		if r.StripPrefix != "" {
			if !strings.HasPrefix(r.StripPrefix, "/") {
				return &ValidationError{Field: fieldPrefix + ".strip_prefix", Value: r.StripPrefix, Message: "strip_prefix must start with '/'"}
			}
			if !strings.HasPrefix(r.Path, r.StripPrefix) {
				return &ValidationError{Field: fieldPrefix + ".strip_prefix", Value: r.StripPrefix, Message: fmt.Sprintf("strip_prefix %q must be a prefix of path %q", r.StripPrefix, r.Path)}
			}
		}

		if r.Timeout < 0 {
			return &ValidationError{Field: fieldPrefix + ".timeout", Value: r.Timeout, Message: "timeout cannot be negative"}
		}

		// Validate upstream reference (unless mock is enabled)
		hasMock := r.Mock != nil && r.Mock.Enabled
		if !hasMock {
			if r.UpstreamID == "" {
				return &ValidationError{Field: fieldPrefix + ".upstream_id", Value: r.UpstreamID, Message: "upstream_id is required when mock is not enabled"}
			}
			if !upstreamMap[r.UpstreamID] {
				return &ValidationError{Field: fieldPrefix + ".upstream_id", Value: r.UpstreamID, Message: fmt.Sprintf("referenced upstream %q does not exist", r.UpstreamID)}
			}
		}

		// Method-aware collision checking
		if len(r.Methods) == 0 {
			// Matches all methods
			if prevID, exists := wildcardRoutes[r.Path]; exists {
				return &ValidationError{Field: fieldPrefix + ".path", Value: r.Path, Message: fmt.Sprintf("route %q conflicts with route %q: both match all methods on path %q", r.ID, prevID, r.Path)}
			}
			// Check if any specific method was already registered on this path
			for pm, prevID := range claimedEndpoints {
				if pm.path == r.Path {
					return &ValidationError{Field: fieldPrefix + ".path", Value: r.Path, Message: fmt.Sprintf("route %q matches all methods but path %q is already claimed for method %q by route %q", r.ID, r.Path, pm.method, prevID)}
				}
			}
			wildcardRoutes[r.Path] = r.ID
		} else {
			if prevID, exists := wildcardRoutes[r.Path]; exists {
				return &ValidationError{Field: fieldPrefix + ".path", Value: r.Path, Message: fmt.Sprintf("route %q conflicts with wildcard route %q on path %q", r.ID, prevID, r.Path)}
			}
			for _, m := range r.Methods {
				upperMethod := strings.ToUpper(strings.TrimSpace(m))
				if !AllowedHTTPMethods[upperMethod] {
					return &ValidationError{Field: fieldPrefix + ".methods", Value: m, Message: fmt.Sprintf("unsupported HTTP method %q", m)}
				}
				key := routePathMethod{path: r.Path, method: upperMethod}
				if prevID, exists := claimedEndpoints[key]; exists {
					return &ValidationError{Field: fieldPrefix + ".methods", Value: upperMethod, Message: fmt.Sprintf("route %q conflicts with route %q on %s %s", r.ID, prevID, upperMethod, r.Path)}
				}
				claimedEndpoints[key] = r.ID
			}
		}
	}

	return nil
}

// ValidateUpstreams validates upstream pools, target URLs, and self-referential loop prevention.
func ValidateUpstreams(upstreams []UpstreamConfig, listenerPort int, listenerHost string) error {
	seenUpstreamIDs := make(map[string]bool)

	for i, u := range upstreams {
		fieldPrefix := fmt.Sprintf("upstreams[%d]", i)
		if u.ID == "" {
			return &ValidationError{Field: fieldPrefix + ".id", Value: u.ID, Message: "upstream ID cannot be empty"}
		}
		if seenUpstreamIDs[u.ID] {
			return &ValidationError{Field: fieldPrefix + ".id", Value: u.ID, Message: fmt.Sprintf("duplicate upstream ID %q", u.ID)}
		}
		seenUpstreamIDs[u.ID] = true

		if !AllowedBalancingAlgorithms[u.Algorithm] {
			return &ValidationError{
				Field:   fieldPrefix + ".algorithm",
				Value:   u.Algorithm,
				Message: fmt.Sprintf("invalid algorithm %q; allowed: swwr, round_robin, p2c, peak_ewma, ip_hash", u.Algorithm),
			}
		}

		if len(u.Targets) == 0 {
			return &ValidationError{Field: fieldPrefix + ".targets", Value: 0, Message: "upstream must have at least one target"}
		}

		seenTargets := make(map[string]bool)
		for j, t := range u.Targets {
			targetField := fmt.Sprintf("%s.targets[%d]", fieldPrefix, j)
			if t.URL == "" {
				return &ValidationError{Field: targetField + ".url", Value: t.URL, Message: "target URL cannot be empty"}
			}

			parsedURL, err := url.ParseRequestURI(t.URL)
			if err != nil {
				return &ValidationError{Field: targetField + ".url", Value: t.URL, Message: fmt.Sprintf("invalid target URL: %v", err)}
			}

			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return &ValidationError{Field: targetField + ".url", Value: parsedURL.Scheme, Message: "target URL scheme must be http or https"}
			}

			if parsedURL.Host == "" {
				return &ValidationError{Field: targetField + ".url", Value: t.URL, Message: "target URL host cannot be empty"}
			}

			// Check duplicate targets within same pool
			if seenTargets[t.URL] {
				return &ValidationError{Field: targetField + ".url", Value: t.URL, Message: fmt.Sprintf("duplicate target URL %q in upstream %q", t.URL, u.ID)}
			}
			seenTargets[t.URL] = true

			// Check target weight
			if t.Weight < 1 {
				return &ValidationError{Field: targetField + ".weight", Value: t.Weight, Message: "target weight must be >= 1"}
			}

			// Check max connections
			if t.MaxConns < 0 {
				return &ValidationError{Field: targetField + ".max_conns", Value: t.MaxConns, Message: "max_conns cannot be negative"}
			}

			// Prevent self-loop / cyclic routing to gateway's own listener
			if isSelfLoopTarget(parsedURL, listenerPort, listenerHost) {
				return &ValidationError{
					Field:   targetField + ".url",
					Value:   t.URL,
					Message: fmt.Sprintf("self-loop detected: target points to gateway listener at %s:%d", listenerHost, listenerPort),
				}
			}
		}

		// Validate HealthCheck if present
		if u.HealthCheck != nil && u.HealthCheck.Enabled {
			hc := u.HealthCheck
			hcPrefix := fieldPrefix + ".health_check"
			if hc.Path == "" || !strings.HasPrefix(hc.Path, "/") {
				return &ValidationError{Field: hcPrefix + ".path", Value: hc.Path, Message: "health check path must start with '/'"}
			}
			if hc.Interval <= 0 {
				return &ValidationError{Field: hcPrefix + ".interval", Value: hc.Interval, Message: "health check interval must be > 0"}
			}
			if hc.Timeout <= 0 {
				return &ValidationError{Field: hcPrefix + ".timeout", Value: hc.Timeout, Message: "health check timeout must be > 0"}
			}
			if hc.Timeout > hc.Interval {
				return &ValidationError{Field: hcPrefix + ".timeout", Value: hc.Timeout, Message: fmt.Sprintf("health check timeout (%v) cannot exceed interval (%v)", hc.Timeout, hc.Interval)}
			}
			if hc.HealthyThreshold < 1 {
				return &ValidationError{Field: hcPrefix + ".healthy_threshold", Value: hc.HealthyThreshold, Message: "healthy_threshold must be >= 1"}
			}
			if hc.UnhealthyThreshold < 1 {
				return &ValidationError{Field: hcPrefix + ".unhealthy_threshold", Value: hc.UnhealthyThreshold, Message: "unhealthy_threshold must be >= 1"}
			}
		}

		// Validate per-upstream circuit breaker if present
		if u.CircuitBreaker != nil && u.CircuitBreaker.Enabled {
			if err := validateCircuitBreaker(u.CircuitBreaker, fieldPrefix+".circuit_breaker"); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateCircuitBreaker validates circuit breaker thresholds and timeouts.
func validateCircuitBreaker(cb *CircuitBreakerConfig, prefix string) error {
	if cb.ConsecutiveFailures < 0 {
		return &ValidationError{Field: prefix + ".consecutive_failures", Value: cb.ConsecutiveFailures, Message: "must be >= 0"}
	}
	if cb.FailureRateThreshold < 0.0 || cb.FailureRateThreshold > 1.0 {
		return &ValidationError{Field: prefix + ".failure_rate_threshold", Value: cb.FailureRateThreshold, Message: "failure rate threshold must be between 0.0 and 1.0"}
	}
	if cb.MinRequests < 0 {
		return &ValidationError{Field: prefix + ".min_requests", Value: cb.MinRequests, Message: "min_requests cannot be negative"}
	}
	if cb.ResetTimeout < 0 {
		return &ValidationError{Field: prefix + ".reset_timeout", Value: cb.ResetTimeout, Message: "reset_timeout cannot be negative"}
	}
	if cb.HalfOpenMaxRequests < 0 {
		return &ValidationError{Field: prefix + ".half_open_max_requests", Value: cb.HalfOpenMaxRequests, Message: "half_open_max_requests cannot be negative"}
	}
	return nil
}

// isSelfLoopTarget checks if target URL references the gateway listener itself.
func isSelfLoopTarget(u *url.URL, listenerPort int, listenerHost string) bool {
	host := u.Hostname()
	portStr := u.Port()

	targetPort := 80
	if u.Scheme == "https" {
		targetPort = 443
	}
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			targetPort = p
		}
	}

	if targetPort != listenerPort {
		return false
	}

	// Compare hosts
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" {
		if listenerHost == "" || listenerHost == "0.0.0.0" || listenerHost == "127.0.0.1" || listenerHost == "localhost" {
			return true
		}
	}

	return strings.EqualFold(host, listenerHost)
}

// ValidateTelemetry checks ring buffer size and intervals.
func ValidateTelemetry(t *TelemetryConfig) error {
	if t.RingBufferSize > 0 {
		// Must be a positive power of 2: (size & (size - 1)) == 0
		if (t.RingBufferSize & (t.RingBufferSize - 1)) != 0 {
			return &ValidationError{
				Field:   "telemetry.ring_buffer_size",
				Value:   t.RingBufferSize,
				Message: "ring buffer size must be a positive power of two (e.g. 1024, 4096, 65536)",
			}
		}
	}
	if t.IdleSleepTimeout < 0 {
		return &ValidationError{Field: "telemetry.idle_sleep_timeout", Value: t.IdleSleepTimeout, Message: "cannot be negative"}
	}
	if t.SnapshotInterval < 0 {
		return &ValidationError{Field: "telemetry.snapshot_interval", Value: t.SnapshotInterval, Message: "cannot be negative"}
	}
	return nil
}

// Validate performs full semantic validation on the GatewayConfig.
func Validate(cfg *GatewayConfig) error {
	if cfg == nil {
		return &ValidationError{Field: "config", Value: nil, Message: "configuration cannot be nil"}
	}

	if err := ValidateListener(&cfg.Listener); err != nil {
		return err
	}

	if err := ValidateUpstreams(cfg.Upstreams, cfg.Listener.Port, cfg.Listener.Host); err != nil {
		return err
	}

	if err := ValidateRoutes(cfg.Routes, cfg.Upstreams, cfg.Listener.Port, cfg.Listener.Host); err != nil {
		return err
	}

	if cfg.Resilience.CircuitBreaker.Enabled {
		if err := validateCircuitBreaker(&cfg.Resilience.CircuitBreaker, "resilience.circuit_breaker"); err != nil {
			return err
		}
	}

	if err := ValidateTelemetry(&cfg.Telemetry); err != nil {
		return err
	}

	return nil
}
