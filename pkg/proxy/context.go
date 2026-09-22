package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewUpstreamContext creates a derived context linking the client's request context
// with an optional upstream execution deadline.
// If the client disconnects, the returned context is cancelled immediately.
// If timeout > 0, the deadline is enforced.
func NewUpstreamContext(clientCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if clientCtx == nil {
		clientCtx = context.Background()
	}

	if timeout > 0 {
		return context.WithTimeout(clientCtx, timeout)
	}

	return context.WithCancel(clientCtx)
}

// IsClientCanceled reports whether an error was caused by the client canceling the request.
func IsClientCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	// Also check for common broken pipe / connection reset strings
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer")
}

// IsGatewayTimeout reports whether an error was caused by a context deadline expiration
// or network dial timeout.
func IsGatewayTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout")
}

// BuildUpstreamURL resolves the target upstream URL for an incoming client request.
// It joins the target URL path and incoming request path cleanly.
func BuildUpstreamURL(target *url.URL, reqURL *url.URL) *url.URL {
	out := *target

	targetPath := target.Path
	reqPath := reqURL.Path

	if targetPath == "" || targetPath == "/" {
		out.Path = reqPath
	} else if reqPath == "" || reqPath == "/" {
		out.Path = targetPath
	} else {
		// Join cleanly avoiding double slashes
		out.Path = strings.TrimSuffix(targetPath, "/") + "/" + strings.TrimPrefix(reqPath, "/")
	}

	// Preserve incoming query parameters
	if reqURL.RawQuery != "" {
		if target.RawQuery != "" {
			out.RawQuery = target.RawQuery + "&" + reqURL.RawQuery
		} else {
			out.RawQuery = reqURL.RawQuery
		}
	}

	return &out
}

// NewUpstreamRequest constructs a sanitized outbound http.Request bound to the given context.
// It applies:
// 1. Context propagation with start timestamp and metadata.
// 2. RFC 7230 hop-by-hop header removal.
// 3. Client IP and protocol forwarding header computation (X-Forwarded-*).
// 4. Host header synchronization with the upstream target host.
func NewUpstreamRequest(ctx context.Context, r *http.Request, target *url.URL) (*http.Request, error) {
	if ctx == nil {
		ctx = r.Context()
	}

	outURL := BuildUpstreamURL(target, r.URL)

	// Clone request with the new context
	outReq := r.Clone(ctx)
	outReq.URL = outURL
	outReq.Host = target.Host
	outReq.RequestURI = "" // RequestURI must be empty for client requests

	// Annotate context with tracing metadata
	outReq = outReq.WithContext(WithStartTime(outReq.Context(), time.Now()))
	outReq = outReq.WithContext(WithTargetURL(outReq.Context(), target))
	clientIP := ExtractClientIP(r)
	if clientIP != "" {
		outReq = outReq.WithContext(WithClientIP(outReq.Context(), clientIP))
	}

	// Strip incoming hop-by-hop headers
	RemoveHopByHopHeaders(outReq.Header)

	// Attach X-Forwarded-* and X-Real-IP headers
	MutateForwardedHeaders(outReq)

	return outReq, nil
}
