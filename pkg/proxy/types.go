package proxy

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Standard buffer sizes optimized for Android Termux ARM64 cache hierarchies:
// - SmallBufferSize (4KB) fits within L1 data cache for small frames, SSE, and headers.
// - LargeBufferSize (32KB) matches L2 cache limits for high-throughput streaming.
const (
	SmallBufferSize = 4 * 1024  // 4KB
	LargeBufferSize = 32 * 1024 // 32KB
)

// contextKey defines a private type for context keys to avoid collisions.
type contextKey struct {
	name string
}

func (k *contextKey) String() string {
	return "nexusgate.proxy." + k.name
}

// Proxy context metadata keys for tracing, timing, and downstream hooks.
var (
	ContextKeyStartTime  = &contextKey{"start_time"}
	ContextKeyTargetURL  = &contextKey{"target_url"}
	ContextKeyRouteID    = &contextKey{"route_id"}
	ContextKeyClientIP   = &contextKey{"client_ip"}
	ContextKeyProxyError = &contextKey{"proxy_error"}
)

// BufferPool manages recycling of byte slices to eliminate heap churn on the streaming path.
type BufferPool interface {
	// GetSmall returns a 4KB slice where len == cap == 4096.
	GetSmall() []byte
	// PutSmall returns a 4KB slice to the pool. Slices with cap != 4096 are discarded.
	PutSmall(b []byte)

	// GetLarge returns a 32KB slice where len == cap == 32768.
	GetLarge() []byte
	// PutLarge returns a 32KB slice to the pool. Slices with cap != 32768 are discarded.
	PutLarge(b []byte)

	// Get returns a slice of at least size bytes. If size <= 4KB, returns a small buffer;
	// otherwise returns a large buffer.
	Get(size int) []byte
	// Put returns a slice to the appropriate pool based on its capacity.
	Put(b []byte)
}

// UpstreamTransport handles executing outbound HTTP round trips to upstream target backends.
type UpstreamTransport interface {
	http.RoundTripper
	// CloseIdleConnections closes any connections which were previously connected from
	// previous requests but are now sitting idle in a "keep-alive" pool.
	CloseIdleConnections()
}

// ProxyHandler defines the reverse proxy serving contracts.
type ProxyHandler interface {
	http.Handler
	// ServeProxy handles forwarding the incoming request to the explicitly provided target URL.
	ServeProxy(w http.ResponseWriter, r *http.Request, target *url.URL)
}

// ErrorHandlerFunc defines a function for rendering synthetic gateway errors (502, 504).
type ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error, statusCode int)

// Config configures reverse proxy behavior, timeouts, buffer pools, and error handling.
type Config struct {
	// Transport specifies the outbound upstream transport. If nil, DefaultTransport is used.
	Transport UpstreamTransport

	// BufferPool specifies the buffer recycling manager. If nil, DefaultBufferPool is used.
	BufferPool BufferPool

	// FlushInterval specifies the flush interval for streaming response bodies.
	// If 0, chunked responses are flushed immediately. If negative, flushing is disabled.
	FlushInterval time.Duration

	// ErrorHandler specifies custom error formatting for 502/504 responses.
	ErrorHandler ErrorHandlerFunc
}

// WithStartTime returns a new context containing the request processing start timestamp.
func WithStartTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, ContextKeyStartTime, t)
}

// GetStartTime retrieves the proxy start timestamp from context if present.
func GetStartTime(ctx context.Context) (time.Time, bool) {
	t, ok := ctx.Value(ContextKeyStartTime).(time.Time)
	return t, ok
}

// WithTargetURL returns a new context containing the resolved target upstream URL.
func WithTargetURL(ctx context.Context, u *url.URL) context.Context {
	return context.WithValue(ctx, ContextKeyTargetURL, u)
}

// GetTargetURL retrieves the upstream target URL from context if present.
func GetTargetURL(ctx context.Context) (*url.URL, bool) {
	u, ok := ctx.Value(ContextKeyTargetURL).(*url.URL)
	return u, ok
}

// WithRouteID returns a new context containing the matched route identifier.
func WithRouteID(ctx context.Context, routeID string) context.Context {
	return context.WithValue(ctx, ContextKeyRouteID, routeID)
}

// GetRouteID retrieves the matched route identifier from context if present.
func GetRouteID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ContextKeyRouteID).(string)
	return id, ok
}

// WithClientIP returns a new context containing the extracted client remote IP.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ContextKeyClientIP, ip)
}

// GetClientIP retrieves the client IP from context if present.
func GetClientIP(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(ContextKeyClientIP).(string)
	return ip, ok
}
