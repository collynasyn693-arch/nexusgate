package resilience

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestFallback_Default503Formatting(t *testing.T) {
	cfg := Config{
		ResetTimeout: 10 * time.Second,
	}
	cb := NewBreaker("backend-auth", cfg)
	cb.fsm.Trip()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	rec := httptest.NewRecorder()

	WriteFallbackResponse(rec, req, cb)

	// 1. Status Code
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503, got %d", rec.Code)
	}

	// 2. Headers
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Fatalf("expected application/json; charset=utf-8, got %q", ct)
	}
	connHdr := rec.Header().Get("Connection")
	if connHdr != "close" {
		t.Fatalf("expected Connection: close, got %q", connHdr)
	}
	circuitHdr := rec.Header().Get("X-NexusGate-Circuit")
	if circuitHdr != "Open" {
		t.Fatalf("expected X-NexusGate-Circuit: Open, got %q", circuitHdr)
	}
	retryAfterStr := rec.Header().Get("Retry-After")
	retryAfter, err := strconv.Atoi(retryAfterStr)
	if err != nil || retryAfter < 1 || retryAfter > 10 {
		t.Fatalf("invalid Retry-After header %q: %v", retryAfterStr, err)
	}

	// 3. JSON Body Schema
	var payload FallbackPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if payload.Error != "circuit_breaker_open" {
		t.Fatalf("expected error 'circuit_breaker_open', got %q", payload.Error)
	}
	if payload.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected code 503, got %d", payload.Code)
	}
	if payload.Upstream != "backend-auth" {
		t.Fatalf("expected upstream 'backend-auth', got %q", payload.Upstream)
	}
	if payload.RetryAfter != retryAfter {
		t.Fatalf("payload retry_after (%d) does not match header (%d)", payload.RetryAfter, retryAfter)
	}
	if payload.Timestamp <= 0 {
		t.Fatalf("expected non-zero timestamp")
	}
}

func TestFallback_CalculateRetryAfter(t *testing.T) {
	// 1. Nil breaker fallback
	if ra := CalculateRetryAfter(nil); ra != 15 {
		t.Fatalf("expected 15 for nil breaker, got %d", ra)
	}

	// 2. Active Open breaker
	cfg := Config{
		ResetTimeout: 5 * time.Second,
	}
	cb := NewBreaker("backend-data", cfg)
	cb.fsm.Trip()

	ra := CalculateRetryAfter(cb)
	if ra < 4 || ra > 5 {
		t.Fatalf("expected Retry-After between 4 and 5 seconds, got %d", ra)
	}

	// 3. Half-Open state returns 1
	_ = cb.fsm.TransitionTo(StateHalfOpen)
	if ra := CalculateRetryAfter(cb); ra != 1 {
		t.Fatalf("expected Retry-After 1 in Half-Open state, got %d", ra)
	}
}

func TestFallback_CustomHandlerDelegation(t *testing.T) {
	cb := NewBreaker("backend-custom", DefaultConfig())
	cb.fsm.Trip()

	customInvoked := false
	customHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customInvoked = true
		w.Header().Set("X-Custom-Fallback", "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cached":true}`))
	})

	fb := NewFallbackHandler(cb, customHandler)

	req := httptest.NewRequest(http.MethodGet, "/cache", nil)
	rec := httptest.NewRecorder()

	fb.ServeHTTP(rec, req)

	if !customInvoked {
		t.Fatalf("custom handler was not invoked")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from custom handler, got %d", rec.Code)
	}
	if rec.Header().Get("X-Custom-Fallback") != "true" {
		t.Fatalf("expected X-Custom-Fallback header")
	}
}
