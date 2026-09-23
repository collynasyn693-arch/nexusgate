package chaos

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeMockResponse_DisabledOrNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	if ServeMockResponse(w, req, nil) {
		t.Errorf("expected false for nil mock")
	}

	disabledMock := &MockRule{
		Enabled:    false,
		StatusCode: 200,
		Body:       "hello",
	}
	if ServeMockResponse(w, req, disabledMock) {
		t.Errorf("expected false for disabled mock")
	}
	if w.Code != 200 || len(w.Body.String()) != 0 {
		t.Errorf("disabled mock wrote response unexpectedly")
	}
}

func TestServeMockResponse_JSONInference(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user", nil)
	w := httptest.NewRecorder()

	mock := &MockRule{
		Enabled:    true,
		StatusCode: 200,
		Headers: map[string]string{
			"X-Custom-Mock": "nexus-mock-engine",
		},
		Body: `{"id": 42, "name": "Nexus Developer"}`,
	}

	handled := ServeMockResponse(w, req, mock)
	if !handled {
		t.Fatalf("expected handled = true")
	}

	res := w.Result()
	if res.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if res.Header.Get(HeaderMock) != "true" {
		t.Errorf("expected header %s: true", HeaderMock)
	}
	if res.Header.Get("X-Custom-Mock") != "nexus-mock-engine" {
		t.Errorf("missing custom mock header")
	}
	if res.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("expected json content-type, got %s", res.Header.Get("Content-Type"))
	}
	if w.Body.String() != mock.Body {
		t.Errorf("expected body %q, got %q", mock.Body, w.Body.String())
	}
}

func TestServeMockResponse_XMLInference(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/feed.xml", nil)
	w := httptest.NewRecorder()

	mock := &MockRule{
		Enabled:    true,
		StatusCode: 201,
		Body:       `<response><status>created</status></response>`,
	}

	handled := ServeMockResponse(w, req, mock)
	if !handled {
		t.Fatalf("expected handled = true")
	}

	if w.Code != 201 {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Errorf("expected xml content-type, got %s", w.Header().Get("Content-Type"))
	}
}

func TestServeMockResponse_CustomContentTypeOverride(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/custom", nil)
	w := httptest.NewRecorder()

	mock := &MockRule{
		Enabled:    true,
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/vnd.api+json",
		},
		Body: `{"data": null}`,
	}

	ServeMockResponse(w, req, mock)

	if w.Header().Get("Content-Type") != "application/vnd.api+json" {
		t.Errorf("expected custom content-type, got %s", w.Header().Get("Content-Type"))
	}
}

func TestServeMockResponse_DefaultStatus200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/default", nil)
	w := httptest.NewRecorder()

	mock := &MockRule{
		Enabled:    true,
		StatusCode: 0, // Unset, should default to 200 OK
		Body:       "plain text body",
	}

	ServeMockResponse(w, req, mock)

	if w.Code != 200 {
		t.Errorf("expected default status 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain, got %s", w.Header().Get("Content-Type"))
	}
}

func TestMockMiddleware(t *testing.T) {
	mock := &MockRule{
		Enabled:    true,
		StatusCode: 200,
		Body:       `{"mocked": true}`,
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	handler := MockMiddleware(mock)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Errorf("expected next handler not to be called when mock is active")
	}
	if w.Body.String() != mock.Body {
		t.Errorf("expected body %q, got %q", mock.Body, w.Body.String())
	}

	// Test with mock disabled
	mock.Enabled = false
	nextCalled = false
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Errorf("expected next handler to be called when mock is disabled")
	}
}
