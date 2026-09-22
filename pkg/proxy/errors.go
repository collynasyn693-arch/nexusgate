package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GatewayError represents a synthetic gateway error payload (502 Bad Gateway or 504 Gateway Timeout).
type GatewayError struct {
	Error     string `json:"error"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// DefaultErrorHandler renders a standard JSON error response for synthetic gateway errors.
// It sets Content-Type: application/json; charset=utf-8 and Connection: close.
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, err error, statusCode int) {
	// Guard against double writes if headers were already sent to client
	if tracker, ok := w.(ResponseWriterTracker); ok && tracker.Written() {
		return
	}

	errorTitle := http.StatusText(statusCode)
	if errorTitle == "" {
		errorTitle = "Gateway Error"
	}

	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	} else {
		errorMessage = errorTitle
	}

	payload := GatewayError{
		Error:     errorTitle,
		Code:      statusCode,
		Message:   errorMessage,
		Timestamp: time.Now().Unix(),
	}

	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		body = []byte(fmt.Sprintf(`{"error":%q,"code":%d,"message":%q}`, errorTitle, statusCode, errorMessage))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Connection", "close")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// WriteGatewayError determines the appropriate HTTP status code (502 vs 504) based on the error,
// and invokes the custom or default error handler.
func WriteGatewayError(w http.ResponseWriter, r *http.Request, err error, customHandler ErrorHandlerFunc) {
	// Guard: if headers have already been transmitted, do not attempt to write synthetic errors
	if tracker, ok := w.(ResponseWriterTracker); ok && tracker.Written() {
		return
	}

	// If client cancelled, do not write a response
	if IsClientCanceled(err) {
		return
	}

	statusCode := http.StatusBadGateway
	if IsGatewayTimeout(err) {
		statusCode = http.StatusGatewayTimeout
	}

	if customHandler != nil {
		customHandler(w, r, err, statusCode)
		return
	}

	DefaultErrorHandler(w, r, err, statusCode)
}
