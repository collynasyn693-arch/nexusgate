package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDefaultErrorHandler_502(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	testErr := errors.New("dial tcp 127.0.0.1:9999: connect: connection refused")

	DefaultErrorHandler(rec, req, testErr, http.StatusBadGateway)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json; charset=utf-8, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Connection") != "close" {
		t.Errorf("expected Connection: close, got %q", rec.Header().Get("Connection"))
	}

	var payload GatewayError
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode JSON error payload: %v", err)
	}
	if payload.Code != 502 {
		t.Errorf("expected payload.Code=502, got %d", payload.Code)
	}
	if payload.Error != "Bad Gateway" {
		t.Errorf("expected payload.Error='Bad Gateway', got %q", payload.Error)
	}
	if payload.Message != testErr.Error() {
		t.Errorf("expected payload.Message=%q, got %q", testErr.Error(), payload.Message)
	}
	if payload.Timestamp <= 0 {
		t.Errorf("expected valid timestamp, got %d", payload.Timestamp)
	}
}

func TestDefaultErrorHandler_504(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	testErr := context.DeadlineExceeded

	DefaultErrorHandler(rec, req, testErr, http.StatusGatewayTimeout)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d", rec.Code)
	}

	var payload GatewayError
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode JSON error payload: %v", err)
	}
	if payload.Code != 504 {
		t.Errorf("expected payload.Code=504, got %d", payload.Code)
	}
	if payload.Error != "Gateway Timeout" {
		t.Errorf("expected payload.Error='Gateway Timeout', got %q", payload.Error)
	}
}

func TestWriteGatewayError_DoubleWriteProtection(t *testing.T) {
	rec := httptest.NewRecorder()
	tracker := WrapTracker(rec)

	// Simulate response headers already sent
	tracker.WriteHeader(http.StatusOK)
	_, _ = tracker.Write([]byte("streaming data started"))

	// Subsequent error write must be a safe no-op
	WriteGatewayError(tracker, nil, errors.New("upstream connection reset"), nil)

	// Response code must remain 200 OK
	if tracker.StatusCode() != http.StatusOK {
		t.Errorf("status code mutated after write: got %d", tracker.StatusCode())
	}
	if rec.Body.String() != "streaming data started" {
		t.Errorf("body corrupted: got %q", rec.Body.String())
	}
}

func TestWriteGatewayError_ClientCanceledSuppression(t *testing.T) {
	rec := httptest.NewRecorder()
	tracker := WrapTracker(rec)

	WriteGatewayError(tracker, nil, context.Canceled, nil)

	if tracker.Written() {
		t.Error("WriteGatewayError must not write any response when client cancels")
	}
	if rec.Body.Len() > 0 {
		t.Errorf("expected empty body for canceled request, got %d bytes", rec.Body.Len())
	}
}

func TestWriteGatewayError_CustomHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	tracker := WrapTracker(rec)

	customCalled := false
	customHandler := func(w http.ResponseWriter, r *http.Request, err error, statusCode int) {
		customCalled = true
		w.Header().Set("X-Custom-Error", "true")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(`custom error`))
	}

	WriteGatewayError(tracker, nil, errors.New("dial failed"), customHandler)

	if !customCalled {
		t.Fatal("expected custom handler to be called")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
	if rec.Header().Get("X-Custom-Error") != "true" {
		t.Error("expected custom header")
	}
}

func TestReverseProxy_IntegrationGatewayErrors(t *testing.T) {
	// 1. Target with no listener -> 502 Bad Gateway
	badTarget, _ := url.Parse("http://127.0.0.1:49999") // closed port
	proxy := NewReverseProxy(Config{})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "http://nexusgate.local/api", nil)
	proxy.ServeProxy(rec, req, badTarget)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for unreachable upstream, got %d", rec.Code)
	}
	if rec.Header().Get("Connection") != "close" {
		t.Errorf("expected Connection: close, got %q", rec.Header().Get("Connection"))
	}

	var payload GatewayError
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode error JSON: %v", err)
	}
	if payload.Code != 502 {
		t.Errorf("expected 502 code in JSON, got %d", payload.Code)
	}

	// 2. Upstream that hangs -> 504 Gateway Timeout via request deadline
	slowUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
	}))
	defer slowUpstream.Close()

	slowTarget, _ := url.Parse(slowUpstream.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	rec2 := httptest.NewRecorder()
	req2, _ := http.NewRequestWithContext(ctx, "GET", "http://nexusgate.local/slow", nil)
	proxy.ServeProxy(rec2, req2, slowTarget)

	if rec2.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 Gateway Timeout for timed out upstream, got %d", rec2.Code)
	}
}
