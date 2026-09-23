package chaos

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Engine is the central coordinator for chaos injection, mock responses, security, and telemetry.
type Engine struct {
	config        Config
	enabled       bool
	strictMode    bool
	defaultRate   float64
	defaultDelay  time.Duration
	maxDelay      time.Duration
	maxBodyBytes  int
	guard         *SecurityGuard
	delayInjector *DelayInjector
	faultInjector *FaultInjector
	audit         *AuditRecorder
}

// NewEngine initializes a fully configured, production-ready ChaosEngine.
func NewEngine(cfg Config, logger AuditLogger) (*Engine, error) {
	guard, err := NewSecurityGuard(cfg)
	if err != nil {
		return nil, err
	}

	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}

	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}

	maxConcurrent := cfg.MaxConcurrentDelays
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentDelays
	}

	return &Engine{
		config:        cfg,
		enabled:       cfg.Enabled,
		strictMode:    cfg.StrictMode,
		defaultRate:   cfg.DefaultFailureRate,
		defaultDelay:  cfg.DefaultDelay,
		maxDelay:      maxDelay,
		maxBodyBytes:  maxBody,
		guard:         guard,
		delayInjector: NewDelayInjector(maxConcurrent, maxDelay),
		faultInjector: NewFaultInjector(cfg.DefaultFailureRate),
		audit:         NewAuditRecorder(logger),
	}, nil
}

// Config returns the active engine configuration.
func (e *Engine) Config() Config {
	return e.config
}

// Parse extracts chaos parameters from the request URL.
func (e *Engine) Parse(r *http.Request) (ChaosParams, bool) {
	if r == nil || r.URL == nil {
		return ChaosParams{}, false
	}
	return ParseQueryParams(r.URL.RawQuery, e.maxDelay, e.maxBodyBytes)
}

// Authorize validates incoming administrative security credentials and remote IP subnets.
func (e *Engine) Authorize(r *http.Request) bool {
	if !e.enabled || e.guard == nil {
		return false
	}
	return e.guard.Authorize(r)
}

// InjectDelay executes a non-blocking delay bounded by context cancellation.
func (e *Engine) InjectDelay(ctx context.Context, d time.Duration) error {
	return e.delayInjector.InjectDelay(ctx, d)
}

// ServeStatus terminates the request pipeline with a synthetic status code and error body.
func (e *Engine) ServeStatus(w http.ResponseWriter, r *http.Request, status int, body string) {
	ServeStatus(w, r, status, body, DefaultReasonForcedStatus)
}

// ServeMock serves a static mock response schema without forwarding to upstream backends.
func (e *Engine) ServeMock(w http.ResponseWriter, r *http.Request, mock *MockRule) bool {
	return ServeMockResponse(w, r, mock)
}

// ShouldInjectFault determines whether a probabilistic fault should be triggered.
func (e *Engine) ShouldInjectFault(rate float64) bool {
	return e.faultInjector.ShouldInject(rate)
}

// DropConnection forcefully severs the client TCP connection or aborts the HTTP/2 stream.
func (e *Engine) DropConnection(w http.ResponseWriter) error {
	return DropConnection(w)
}

// Counters returns a point-in-time snapshot of chaos metrics.
func (e *Engine) Counters() AuditCountersSnapshot {
	return e.audit.Counters()
}

// Audit returns the underlying audit recorder.
func (e *Engine) Audit() *AuditRecorder {
	return e.audit
}

// Wrap returns an HTTP middleware wrapping the downstream handler in the chaos evaluation pipeline.
func (e *Engine) Wrap(next http.Handler) http.Handler {
	return e.WrapRoute(nil, next)
}

// WrapRoute returns an HTTP middleware that applies both route-level static mocks and chaos rules.
func (e *Engine) WrapRoute(mock *MockRule, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Hot path check: if chaos is disabled and no route mock exists, bypass immediately (<2ns, 0 allocs)
		if !e.enabled && (mock == nil || !mock.Enabled) {
			next.ServeHTTP(w, r)
			return
		}

		rawQuery := ""
		if r.URL != nil {
			rawQuery = r.URL.RawQuery
		}

		hasChaosParams := len(rawQuery) > 0 && strings.Contains(rawQuery, "__")
		hasMock := mock != nil && mock.Enabled
		hasGlobalFaults := e.defaultRate > 0 || e.defaultDelay > 0

		// Fast bypass if enabled but request has no chaos triggers, no mocks, and no global defaults
		if !hasChaosParams && !hasMock && !hasGlobalFaults {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := ExtractRemoteIP(r)
		routeID := ""

		// 2. Parse chaos parameters if "__" is present
		var params ChaosParams
		if hasChaosParams {
			params, _ = e.Parse(r)
		}

		// 3. Security authorization guard
		if params.HasChaos {
			if !e.Authorize(r) {
				if e.strictMode {
					e.audit.RecordUnauthorized(routeID, clientIP, "unauthorized_chaos_attempt")
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":"forbidden","message":"unauthorized chaos attempt"}`))
					return
				}
				// Permissive mode: silently ignore chaos parameters, strip credentials, and continue
				e.audit.RecordUnauthorized(routeID, clientIP, "unauthorized_chaos_ignored")
				rClean := sanitizeOutboundRequest(r)
				next.ServeHTTP(w, rClean)
				return
			}
		}

		// 4. Latency delay injection
		delay := params.Delay
		if delay <= 0 && e.defaultDelay > 0 {
			delay = e.defaultDelay
		}
		if delay > 0 {
			e.audit.RecordDelay(routeID, clientIP, delay)
			if err := e.InjectDelay(r.Context(), delay); err != nil {
				// Client canceled or connection aborted during delay
				return
			}
		}

		// 5. Connection drop injection
		if params.Drop {
			e.audit.RecordDrop(routeID, clientIP)
			_ = e.DropConnection(w)
			return
		}

		// 6. Forced status code and body override
		if params.Status > 0 {
			e.audit.RecordStatus(routeID, clientIP, params.Status, DefaultReasonForcedStatus)
			e.ServeStatus(w, r, params.Status, params.Body)
			return
		}

		// 7. Static mock response evaluation
		if hasMock {
			e.audit.RecordMock(routeID, clientIP, mock.StatusCode)
			if e.ServeMock(w, r, mock) {
				return
			}
		}

		// 8. Probabilistic fault evaluation
		faultRate := params.FaultRate
		if faultRate <= 0 && e.defaultRate > 0 {
			faultRate = e.defaultRate
		}
		if faultRate > 0 && e.ShouldInjectFault(faultRate) {
			e.audit.RecordRandomFault(routeID, clientIP)
			ServeStatus(w, r, http.StatusInternalServerError, `{"error":"synthetic_random_fault","chaos":true}`, "random-fault")
			return
		}

		// 9. Outbound request sanitization before hitting next handler/upstream proxy
		rClean := sanitizeOutboundRequest(r)
		next.ServeHTTP(w, rClean)
	})
}

// sanitizeOutboundRequest strips chaos parameters from URL and credentials from headers.
func sanitizeOutboundRequest(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}

	needsClone := false
	hasChaosHeader := r.Header.Get(HeaderChaosKey) != ""
	hasChaosQuery := r.URL != nil && strings.Contains(r.URL.RawQuery, "__")

	if !hasChaosHeader && !hasChaosQuery {
		return r
	}

	needsClone = true
	var r2 *http.Request
	if needsClone {
		r2 = r.Clone(r.Context())
		if hasChaosHeader {
			r2.Header.Del(HeaderChaosKey)
		}
		if hasChaosQuery && r2.URL != nil {
			r2.URL = SanitizeRequestURL(r2.URL)
		}
	}
	return r2
}
