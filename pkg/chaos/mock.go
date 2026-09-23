package chaos

import (
	"net/http"
	"strings"
)

// ServeMockResponse evaluates a route-level MockRule. If enabled, it writes the
// predefined headers, synthetic body, and status code, returning true to indicate
// the request has been fully handled and short-circuited without upstream proxying.
func ServeMockResponse(w http.ResponseWriter, r *http.Request, mock *MockRule) bool {
	if mock == nil || !mock.Enabled {
		return false
	}

	statusCode := mock.StatusCode
	if statusCode < 100 || statusCode > 599 {
		statusCode = http.StatusOK
	}

	header := w.Header()
	header.Set(HeaderMock, "true")

	// Apply configured headers
	for k, v := range mock.Headers {
		header.Set(k, v)
	}

	// Auto-detect Content-Type if omitted
	if header.Get("Content-Type") == "" {
		trimmed := strings.TrimSpace(mock.Body)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			header.Set("Content-Type", "application/json; charset=utf-8")
		} else if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
			header.Set("Content-Type", "application/xml; charset=utf-8")
		} else {
			header.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}

	w.WriteHeader(statusCode)
	if len(mock.Body) > 0 {
		_, _ = w.Write([]byte(mock.Body))
	}
	return true
}

// MockMiddleware wraps an http.Handler with static route mock handling.
func MockMiddleware(mock *MockRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if mock == nil || !mock.Enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ServeMockResponse(w, r, mock) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
