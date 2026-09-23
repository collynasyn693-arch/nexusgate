package resilience

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// FallbackPayload defines the structured JSON response returned when traffic is rejected
// by an Open or saturated Half-Open circuit breaker.
type FallbackPayload struct {
	Error      string `json:"error"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
	Upstream   string `json:"upstream,omitempty"`
	RetryAfter int    `json:"retry_after"`
	Timestamp  int64  `json:"timestamp"`
}

// FallbackHandler generates RFC 7231-compliant HTTP 503 responses with Retry-After headers,
// or delegates to an optional user-defined custom handler.
type FallbackHandler struct {
	breaker CircuitBreaker
	custom  http.Handler
}

// NewFallbackHandler creates a new FallbackHandler bound to a circuit breaker.
func NewFallbackHandler(breaker CircuitBreaker, custom http.Handler) *FallbackHandler {
	return &FallbackHandler{
		breaker: breaker,
		custom:  custom,
	}
}

// ServeHTTP implements http.Handler, executing either custom fallback logic or writing
// the synthetic 503 Service Unavailable response.
func (h *FallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.custom != nil {
		h.custom.ServeHTTP(w, r)
		return
	}
	WriteFallbackResponse(w, r, h.breaker)
}

// CalculateRetryAfter computes the remaining duration in seconds until the circuit breaker
// may attempt to recover (cooldown expiry).
// Invariant (RFC 7231 / RFC 9110): returns an integer number of seconds, with a minimum floor of 1.
func CalculateRetryAfter(breaker CircuitBreaker) int {
	if breaker == nil {
		return 15 // Default 15s fallback
	}

	b, ok := breaker.(*Breaker)
	if !ok || b.fsm == nil {
		return 15
	}

	st := b.State()
	if st == StateHalfOpen {
		return 1 // In Half-Open trial phase, retry almost immediately
	}

	openUntil := b.fsm.OpenUntil()
	now := monotonicNano()
	if openUntil <= now {
		return 1
	}

	remainingNanos := openUntil - now
	// Ceiling division to round up to full seconds
	seconds := int((remainingNanos + int64(time.Second) - 1) / int64(time.Second))
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

// WriteFallbackResponse emits the RFC 7231 synthetic 503 Service Unavailable response.
func WriteFallbackResponse(w http.ResponseWriter, r *http.Request, breaker CircuitBreaker) {
	retryAfter := CalculateRetryAfter(breaker)
	upstreamName := ""
	stateStr := "open"
	if breaker != nil {
		upstreamName = breaker.Name()
		stateStr = breaker.State().String()
	}

	payload := FallbackPayload{
		Error:      "circuit_breaker_open",
		Code:       http.StatusServiceUnavailable,
		Message:    fmt.Sprintf("Service unavailable: circuit breaker is %s for upstream target", stateStr),
		Upstream:   upstreamName,
		RetryAfter: retryAfter,
		Timestamp:  time.Now().Unix(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"error":"circuit_breaker_open","code":503,"message":"circuit breaker open"}`)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.Header().Set("X-NexusGate-Circuit", stateStr)
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write(body)
}
