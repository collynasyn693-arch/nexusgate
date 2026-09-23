package resilience

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPassiveTap_InterceptionAndTrip(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures: 3,
		ResetTimeout:        5 * time.Second,
	}
	cb := NewBreaker("test-passive", cfg)
	tap := NewPassiveTap(cb, DefaultFailureClassifier)

	ctx := context.Background()

	// 1. Successes (200 OK)
	tap.RecordResponse(ctx, http.StatusOK, nil)
	tap.RecordResponse(ctx, http.StatusCreated, nil)
	counts := cb.Counts()
	if counts.Successes != 2 || counts.Failures != 0 {
		t.Fatalf("expected 2 successes, 0 failures, got: %+v", counts)
	}
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed, got %v", cb.State())
	}

	// 2. Failures (502 Bad Gateway)
	tap.RecordResponse(ctx, http.StatusBadGateway, nil)
	tap.RecordResponse(ctx, http.StatusBadGateway, nil)
	if cb.State() != StateClosed {
		t.Fatalf("circuit tripped before 3rd failure")
	}

	// 3. 3rd Failure trips the breaker
	tap.RecordResponse(ctx, http.StatusServiceUnavailable, nil)
	if cb.State() != StateOpen {
		t.Fatalf("expected circuit to trip to StateOpen on 3rd failure, got %v", cb.State())
	}
	if tap.Allow() {
		t.Fatalf("expected tap.Allow() to return false when circuit is Open")
	}
}

func TestPassiveTap_ClientCanceledFilter(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures: 1, // Any failure would trip
	}
	cb := NewBreaker("test-client-cancel", cfg)
	tap := NewPassiveTap(cb, DefaultFailureClassifier)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel context

	// Record response with canceled context and context.Canceled error
	tap.RecordResponse(canceledCtx, 0, context.Canceled)

	// Must NOT record failure, circuit must remain Closed!
	if cb.State() != StateClosed {
		t.Fatalf("circuit tripped on client cancellation")
	}
	if cb.Counts().Failures != 0 {
		t.Fatalf("client cancellation recorded as failure")
	}
}

func TestPassiveTransport_EndToEndInterception(t *testing.T) {
	var upstreamCalls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503 error
	}))
	defer server.Close()

	cfg := Config{
		ConsecutiveFailures: 3,
		ResetTimeout:        10 * time.Second,
	}
	cb := NewBreaker("test-transport", cfg)
	tap := NewPassiveTap(cb, DefaultFailureClassifier)
	transport := NewPassiveTransport(http.DefaultTransport, tap, nil)

	client := &http.Client{Transport: transport}

	// First 3 requests hit the server and fail with 503
	for i := 0; i < 3; i++ {
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		_ = resp.Body.Close()
	}

	if upstreamCalls.Load() != 3 {
		t.Fatalf("expected 3 upstream calls, got %d", upstreamCalls.Load())
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected circuit to be Open after 3 failures, got %v", cb.State())
	}

	// 4th request must be short-circuited by PassiveTransport without hitting upstream!
	resp, err := client.Get(server.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatalf("expected error from short-circuited request")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	// Upstream call count must still be 3 (no new call reached server)
	if upstreamCalls.Load() != 3 {
		t.Fatalf("expected upstream call count to remain 3, got %d", upstreamCalls.Load())
	}
}

func TestPassiveMiddleware_SyntheticResponse(t *testing.T) {
	cfg := Config{
		ConsecutiveFailures: 2,
		ResetTimeout:        5 * time.Second,
	}
	cb := NewBreaker("test-mw", cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("backend failure"))
	})

	mw := PassiveMiddleware(cb, nil)(handler)

	// Request 1: fails
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w1 := httptest.NewRecorder()
	mw.ServeHTTP(w1, req1)
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w1.Code)
	}

	// Request 2: fails -> trips breaker to Open
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w2 := httptest.NewRecorder()
	mw.ServeHTTP(w2, req2)
	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after 2 failures")
	}

	// Request 3: short-circuited by middleware with 503 and Retry-After header
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w3 := httptest.NewRecorder()
	mw.ServeHTTP(w3, req3)

	if w3.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", w3.Code)
	}
	if w3.Header().Get("Retry-After") != "15" {
		t.Fatalf("expected Retry-After 15, got %q", w3.Header().Get("Retry-After"))
	}
}
