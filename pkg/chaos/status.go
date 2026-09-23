package chaos

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// DefaultReasonForcedStatus indicates a manual status override.
const DefaultReasonForcedStatus = "forced-status"

// CannedErrorResponse represents the default JSON body for synthetic chaos responses.
type CannedErrorResponse struct {
	Status int    `json:"status"`
	Error  string `json:"error"`
	Chaos  bool   `json:"chaos"`
	Reason string `json:"reason,omitempty"`
}

// ServeStatus writes a synthetic HTTP status code, chaos headers, and body,
// immediately terminating the request pipeline without upstream proxying.
func ServeStatus(w http.ResponseWriter, r *http.Request, status int, body string, reason string) {
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	if reason == "" {
		reason = DefaultReasonForcedStatus
	}

	header := w.Header()
	header.Set(HeaderChaosInjected, "true")
	header.Set(HeaderChaosReason, reason)

	var (
		payload     []byte
		contentType string
	)

	if len(body) > 0 {
		payload = []byte(body)
		trimmed := strings.TrimSpace(body)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			contentType = "application/json; charset=utf-8"
		} else {
			contentType = "text/plain; charset=utf-8"
		}
	} else {
		// Detect whether client prefers JSON
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "application/json") || strings.Contains(accept, "*/*") || accept == "" {
			resp := CannedErrorResponse{
				Status: status,
				Error:  http.StatusText(status),
				Chaos:  true,
				Reason: reason,
			}
			data, err := json.Marshal(resp)
			if err == nil {
				payload = data
				contentType = "application/json; charset=utf-8"
			}
		}

		if payload == nil {
			statusText := http.StatusText(status)
			if statusText == "" {
				statusText = fmt.Sprintf("HTTP %d", status)
			}
			payload = []byte(statusText + "\n")
			contentType = "text/plain; charset=utf-8"
		}
	}

	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", contentType)
	}

	w.WriteHeader(status)
	_, _ = w.Write(payload)
}
