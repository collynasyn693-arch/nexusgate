package chaos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeStatus_Default503(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	ServeStatus(w, req, http.StatusServiceUnavailable, "", "")

	res := w.Result()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.StatusCode)
	}
	if res.Header.Get(HeaderChaosInjected) != "true" {
		t.Errorf("missing %s header", HeaderChaosInjected)
	}
	if res.Header.Get(HeaderChaosReason) != DefaultReasonForcedStatus {
		t.Errorf("expected reason %s, got %s", DefaultReasonForcedStatus, res.Header.Get(HeaderChaosReason))
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json content-type, got %s", res.Header.Get("Content-Type"))
	}

	var canned CannedErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&canned); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if canned.Status != 503 || !canned.Chaos || canned.Error != http.StatusText(503) {
		t.Errorf("unexpected payload content: %+v", canned)
	}
}

func TestServeStatus_CustomJSONBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()

	customBody := `{"error":"quota_exceeded","limit":100}`
	ServeStatus(w, req, http.StatusTooManyRequests, customBody, "rate-limit-simulation")

	res := w.Result()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.StatusCode)
	}
	if res.Header.Get(HeaderChaosInjected) != "true" {
		t.Errorf("missing %s header", HeaderChaosInjected)
	}
	if res.Header.Get(HeaderChaosReason) != "rate-limit-simulation" {
		t.Errorf("expected custom reason, got %s", res.Header.Get(HeaderChaosReason))
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("expected json content-type, got %s", res.Header.Get("Content-Type"))
	}
	if w.Body.String() != customBody {
		t.Errorf("expected body %q, got %q", customBody, w.Body.String())
	}
}

func TestServeStatus_PlainTextBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()

	plainBody := "Bad Request: malformed parameter"
	ServeStatus(w, req, http.StatusBadRequest, plainBody, "")

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("expected text/plain content-type, got %s", res.Header.Get("Content-Type"))
	}
	if w.Body.String() != plainBody {
		t.Errorf("expected body %q, got %q", plainBody, w.Body.String())
	}
}

func TestServeStatus_AcceptPlainText(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Accept", "text/plain")
	w := httptest.NewRecorder()

	ServeStatus(w, req, http.StatusInternalServerError, "", "")

	res := w.Result()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("expected text/plain content-type, got %s", res.Header.Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Errorf("expected 'Internal Server Error' in body, got %q", w.Body.String())
	}
}

func TestServeStatus_VariousCodes(t *testing.T) {
	codes := []int{400, 401, 403, 404, 429, 500, 502, 503, 504}
	for _, code := range codes {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		ServeStatus(w, req, code, "", "")
		if w.Code != code {
			t.Errorf("expected code %d, got %d", code, w.Code)
		}
	}
}
