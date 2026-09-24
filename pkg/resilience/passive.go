package resilience

import (
	"context"
	"errors"
	"net/http"
)

// FailureClassifier defines a function that categorizes an HTTP status code and error
// as an upstream failure (true) or success (false).
type FailureClassifier func(statusCode int, err error) bool

// DefaultFailureClassifier classifies HTTP 5xx responses (500, 502, 503, 504) and
// network errors as failures.
//
// Invariant (from Pre-Audit):
// - Client cancellations (context.Canceled) are explicitly NOT recorded as upstream failures.
// - Client errors (4xx: 400, 401, 403, 404) are NOT failures (prevents DoS via deliberate 404s).
func DefaultFailureClassifier(statusCode int, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false // Downstream client aborted, not upstream fault
		}
		return true // Connection refused, dial timeout, socket reset, etc.
	}

	// Status >= 500 represents upstream server error
	return statusCode >= 500
}

// PassiveTap hooks into the proxy execution pipeline to passively monitor
// upstream responses and report success/failure metrics to the circuit breaker.
type PassiveTap struct {
	breaker    CircuitBreaker
	classifier FailureClassifier
}

// NewPassiveTap constructs a PassiveTap for the provided circuit breaker.
func NewPassiveTap(breaker CircuitBreaker, classifier FailureClassifier) *PassiveTap {
	if classifier == nil {
		classifier = DefaultFailureClassifier
	}
	return &PassiveTap{
		breaker:    breaker,
		classifier: classifier,
	}
}

// Breaker returns the underlying CircuitBreaker.
func (t *PassiveTap) Breaker() CircuitBreaker {
	return t.breaker
}

// Allow reports whether a new request is permitted to proceed upstream.
func (t *PassiveTap) Allow() bool {
	if t.breaker == nil {
		return true
	}
	return t.breaker.Allow()
}

// RecordResponse intercepts the completed upstream round-trip status and error,
// classifying it and notifying the circuit breaker.
func (t *PassiveTap) RecordResponse(reqCtx context.Context, statusCode int, err error) {
	if t.breaker == nil {
		return
	}

	// Filter out downstream client cancellations
	if reqCtx != nil && reqCtx.Err() != nil && errors.Is(reqCtx.Err(), context.Canceled) {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}

	isFail := t.classifier(statusCode, err)
	if isFail {
		t.breaker.RecordFailure()
	} else {
		t.breaker.RecordSuccess()
	}
}

// PassiveTransport wraps an existing http.RoundTripper with passive circuit breaker tapping.
type PassiveTransport struct {
	transport  http.RoundTripper
	tap        *PassiveTap
	fallback   http.Handler
}

// NewPassiveTransport wraps an upstream round-tripper with circuit breaker enforcement.
func NewPassiveTransport(transport http.RoundTripper, tap *PassiveTap, fallback http.Handler) *PassiveTransport {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &PassiveTransport{
		transport: transport,
		tap:       tap,
		fallback:  fallback,
	}
}

// RoundTrip executes the HTTP round-trip through the circuit breaker tap.
func (pt *PassiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if pt.tap != nil && !pt.tap.Allow() {
		return nil, ErrCircuitOpen
	}

	resp, err := pt.transport.RoundTrip(req)

	if pt.tap != nil {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		pt.tap.RecordResponse(req.Context(), statusCode, err)
	}

	return resp, err
}

// CloseIdleConnections closes any idle connections on the underlying transport if supported.
func (pt *PassiveTransport) CloseIdleConnections() {
	if closer, ok := pt.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// PassiveMiddleware wraps an http.Handler with circuit breaker enforcement.
// If the circuit breaker rejects the request, it delegates to the fallback handler
// (or returns HTTP 503 if no fallback handler is set).
func PassiveMiddleware(breaker CircuitBreaker, fallback http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if breaker != nil && !breaker.Allow() {
				fb := fallback
				if fb == nil {
					fb = breaker.FallbackHandler()
				}
				if fb != nil {
					fb.ServeHTTP(w, r)
					return
				}
				w.Header().Set("Retry-After", "15")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"circuit_breaker_open","code":503,"message":"upstream circuit breaker open"}`))
				return
			}

			// Wrap ResponseWriter to record status code passively
			tw := &statusCodeTracker{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(tw, r)

			if breaker != nil {
				// Prevent developer chaos injection faults from tripping production circuit breakers
				if tw.Header().Get("X-NexusGate-Chaos-Injected") != "" {
					return
				}
				if r.Context().Err() != nil {
					// Client aborted request before completion; release canary permit without recording failure
					if breaker.State() == StateHalfOpen {
						breaker.ReleaseInflight()
					}
					return
				}
				if DefaultFailureClassifier(tw.statusCode, nil) {
					breaker.RecordFailure()
				} else {
					breaker.RecordSuccess()
				}
			}
		})
	}
}

// statusCodeTracker captures the HTTP response status code without extra allocations.
type statusCodeTracker struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (t *statusCodeTracker) WriteHeader(code int) {
	if !t.written {
		t.statusCode = code
		t.written = true
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *statusCodeTracker) Write(b []byte) (int, error) {
	if !t.written {
		t.written = true
	}
	return t.ResponseWriter.Write(b)
}
