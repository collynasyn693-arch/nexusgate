package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMethodIsolation(t *testing.T) {
	mux := NewMux()

	var calledMethod string
	hGet := HandlerFunc(func(w http.ResponseWriter, r *http.Request, p Params) {
		calledMethod = "GET"
		w.WriteHeader(http.StatusOK)
	})
	hPost := HandlerFunc(func(w http.ResponseWriter, r *http.Request, p Params) {
		calledMethod = "POST"
		w.WriteHeader(http.StatusCreated)
	})

	if err := mux.Handle(http.MethodGet, "/items", hGet); err != nil {
		t.Fatalf("Handle GET failed: %v", err)
	}
	if err := mux.Handle(http.MethodPost, "/items", hPost); err != nil {
		t.Fatalf("Handle POST failed: %v", err)
	}

	// Request GET /items
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/items", nil)
	mux.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK || calledMethod != "GET" {
		t.Errorf("expected GET /items to return 200, got %d, called=%s", w1.Code, calledMethod)
	}

	// Request POST /items
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/items", nil)
	mux.ServeHTTP(w2, r2)
	if w2.Code != http.StatusCreated || calledMethod != "POST" {
		t.Errorf("expected POST /items to return 201, got %d, called=%s", w2.Code, calledMethod)
	}
}

func TestMethodNotAllowed405(t *testing.T) {
	mux := NewMux()

	_ = mux.Handle(http.MethodGet, "/api/resource", dummyHandler("get"))
	_ = mux.Handle(http.MethodPost, "/api/resource", dummyHandler("post"))

	// Request DELETE /api/resource -> 405 Method Not Allowed
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/resource", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", w.Code)
	}

	allowHeader := w.Header().Get("Allow")
	if !strings.Contains(allowHeader, "GET") || !strings.Contains(allowHeader, "POST") || !strings.Contains(allowHeader, "OPTIONS") {
		t.Errorf("Allow header missing expected methods: %q", allowHeader)
	}
}

func TestAutomaticOPTIONS(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(http.MethodGet, "/api/data", dummyHandler("get"))
	_ = mux.Handle(http.MethodPut, "/api/data", dummyHandler("put"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/api/data", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for OPTIONS, got %d", w.Code)
	}

	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "PUT") {
		t.Errorf("expected Allow header to contain GET and PUT, got %q", allow)
	}
}

func TestCustomOPTIONS(t *testing.T) {
	mux := NewMux()
	customCalled := false
	hOptions := HandlerFunc(func(w http.ResponseWriter, r *http.Request, p Params) {
		customCalled = true
		w.Header().Set("X-Custom-Options", "true")
		w.WriteHeader(http.StatusOK)
	})

	_ = mux.Handle(http.MethodGet, "/custom-opt", dummyHandler("get"))
	_ = mux.Handle(http.MethodOptions, "/custom-opt", hOptions)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/custom-opt", nil)
	mux.ServeHTTP(w, r)

	if !customCalled || w.Code != http.StatusOK || w.Header().Get("X-Custom-Options") != "true" {
		t.Errorf("custom OPTIONS handler was not invoked properly: code=%d, called=%v", w.Code, customCalled)
	}
}

func TestCustomNotFoundAndMethodNotAllowed(t *testing.T) {
	mux := NewMux()
	mux.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	mux.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	_ = mux.Handle(http.MethodGet, "/exist", dummyHandler("exist"))

	// 404 test
	w404 := httptest.NewRecorder()
	r404 := httptest.NewRequest(http.MethodGet, "/missing", nil)
	mux.ServeHTTP(w404, r404)
	if w404.Code != http.StatusTeapot {
		t.Errorf("expected custom NotFound 418, got %d", w404.Code)
	}

	// 405 test
	w405 := httptest.NewRecorder()
	r405 := httptest.NewRequest(http.MethodPost, "/exist", nil)
	mux.ServeHTTP(w405, r405)
	if w405.Code != http.StatusConflict {
		t.Errorf("expected custom MethodNotAllowed 409, got %d", w405.Code)
	}
}

func TestInvalidMethodHandling(t *testing.T) {
	mux := NewMux()
	err := mux.Handle("INVALID_METHOD", "/test", dummyHandler("test"))
	if !errors.Is(err, ErrInvalidMethod) {
		t.Errorf("expected ErrInvalidMethod, got %v", err)
	}

	// Lookup with invalid method
	var params Params
	h, ok := mux.Lookup("FOOBAR", "/test", &params)
	if ok || h != nil {
		t.Errorf("expected lookup with invalid method to return (nil, false)")
	}
}

func TestMuxClone(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(http.MethodGet, "/api/v1/users", dummyHandler("users"))

	clone := mux.clone()
	if clone == mux {
		t.Fatal("clone pointer should not equal original mux pointer")
	}

	var p Params
	h, ok := clone.Lookup(http.MethodGet, "/api/v1/users", &p)
	if !ok || h == nil {
		t.Error("expected cloned mux to match registered route")
	}

	// Mutating original must not affect clone
	_ = mux.Handle(http.MethodGet, "/api/v1/posts", dummyHandler("posts"))
	_, ok = clone.Lookup(http.MethodGet, "/api/v1/posts", &p)
	if ok {
		t.Error("clone unexpectedly has newly registered route from original mux")
	}
}
