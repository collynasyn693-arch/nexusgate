package chaos

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiddleware_Disabled(t *testing.T) {
	eng, err := NewEngine(Config{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := eng.Wrap(next)
	req := httptest.NewRequest(http.MethodGet, "/api/data?__status=500", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Errorf("expected next handler to be called when chaos is disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestMiddleware_StaticMockServed(t *testing.T) {
	eng, _ := NewEngine(Config{Enabled: true, AdminKey: "key"}, nil)

	mock := &MockRule{
		Enabled:    true,
		StatusCode: 200,
		Body:       `{"mock": true}`,
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	handler := eng.WrapRoute(mock, next)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mocked", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Errorf("next handler should not be called when mock is served")
	}
	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Header().Get(HeaderMock) != "true" {
		t.Errorf("expected header %s: true", HeaderMock)
	}
	if eng.Counters().MocksServed != 1 {
		t.Errorf("expected MocksServed 1, got %d", eng.Counters().MocksServed)
	}
}

func TestMiddleware_StatusOverride(t *testing.T) {
	eng, _ := NewEngine(Config{
		Enabled:  true,
		AdminKey: "secret-adm",
	}, nil)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	handler := eng.Wrap(next)
	req := httptest.NewRequest(http.MethodGet, "/test?__status=503", nil)
	req.Header.Set(HeaderChaosKey, "secret-adm")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Errorf("next handler should not be called on status override")
	}
	if w.Code != 503 {
		t.Errorf("expected status 503, got %d", w.Code)
	}
	if w.Header().Get(HeaderChaosInjected) != "true" {
		t.Errorf("expected chaos injected header")
	}
	if eng.Counters().StatusesInjected != 1 {
		t.Errorf("expected StatusesInjected 1, got %d", eng.Counters().StatusesInjected)
	}
}

func TestMiddleware_Unauthorized_StrictMode(t *testing.T) {
	eng, _ := NewEngine(Config{
		Enabled:    true,
		AdminKey:   "secret-adm",
		StrictMode: true,
	}, nil)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	handler := eng.Wrap(next)
	// Missing key
	req := httptest.NewRequest(http.MethodGet, "/test?__status=503", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Errorf("next handler should not be called in strict mode")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
	if eng.Counters().UnauthorizedAttempts != 1 {
		t.Errorf("expected UnauthorizedAttempts 1, got %d", eng.Counters().UnauthorizedAttempts)
	}
}

func TestMiddleware_Unauthorized_PermissiveMode(t *testing.T) {
	eng, _ := NewEngine(Config{
		Enabled:    true,
		AdminKey:   "secret-adm",
		StrictMode: false, // Permissive mode
	}, nil)

	var interceptedRawQuery string
	var interceptedKey string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		interceptedRawQuery = r.URL.RawQuery
		interceptedKey = r.Header.Get(HeaderChaosKey)
		w.WriteHeader(http.StatusOK)
	})

	handler := eng.Wrap(next)
	// Request with wrong key and chaos params
	req := httptest.NewRequest(http.MethodGet, "/api/data?id=123&__status=500", nil)
	req.Header.Set(HeaderChaosKey, "wrong-key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 in permissive mode, got %d", w.Code)
	}
	if interceptedRawQuery != "id=123" {
		t.Errorf("expected chaos params stripped from raw query, got %q", interceptedRawQuery)
	}
	if interceptedKey != "" {
		t.Errorf("expected admin key stripped from header, got %q", interceptedKey)
	}
	if eng.Counters().UnauthorizedAttempts != 1 {
		t.Errorf("expected UnauthorizedAttempts 1, got %d", eng.Counters().UnauthorizedAttempts)
	}
}

func TestMiddleware_DelayAndSanitization(t *testing.T) {
	eng, _ := NewEngine(Config{
		Enabled:  true,
		AdminKey: "secret-adm",
	}, nil)

	var upstreamQuery string
	var upstreamKey string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamQuery = r.URL.RawQuery
		upstreamKey = r.Header.Get(HeaderChaosKey)
		w.WriteHeader(http.StatusOK)
	})

	handler := eng.Wrap(next)
	req := httptest.NewRequest(http.MethodGet, "/test?data=true&__delay=25ms", nil)
	req.Header.Set(HeaderChaosKey, "secret-adm")
	w := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if elapsed < 20*time.Millisecond {
		t.Errorf("delay was not applied, elapsed: %v", elapsed)
	}
	if upstreamQuery != "data=true" {
		t.Errorf("expected stripped query 'data=true', got %q", upstreamQuery)
	}
	if upstreamKey != "" {
		t.Errorf("expected stripped chaos key, got %q", upstreamKey)
	}
	if eng.Counters().DelaysInjected != 1 {
		t.Errorf("expected DelaysInjected 1, got %d", eng.Counters().DelaysInjected)
	}
}

func TestMiddleware_RandomFault(t *testing.T) {
	eng, _ := NewEngine(Config{
		Enabled:            true,
		AdminKey:           "secret-adm",
		DefaultFailureRate: 1.0, // 100% failure rate
	}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := eng.Wrap(next)
	req := httptest.NewRequest(http.MethodGet, "/random-test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 error from 100%% random fault, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get(HeaderChaosInjected), "true") {
		t.Errorf("expected chaos injected header")
	}
	if eng.Counters().RandomFaultsInjected != 1 {
		t.Errorf("expected RandomFaultsInjected 1, got %d", eng.Counters().RandomFaultsInjected)
	}
}
