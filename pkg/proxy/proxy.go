package proxy

import (
	"net/http"
	"net/url"
)

// ReverseProxy is the core streaming reverse proxy engine for NexusGate.
// It coordinates header mutations, dual-tier buffer recycling, upstream transports,
// Server-Sent Events flushing, and WebSocket connection hijacking.
type ReverseProxy struct {
	transport  UpstreamTransport
	bufferPool BufferPool
	config     Config
}

// NewReverseProxy constructs an initialized ReverseProxy instance.
func NewReverseProxy(config Config) *ReverseProxy {
	transport := config.Transport
	if transport == nil {
		transport = DefaultTransport
	}

	pool := config.BufferPool
	if pool == nil {
		pool = DefaultBufferPool
	}

	return &ReverseProxy{
		transport:  transport,
		bufferPool: pool,
		config:     config,
	}
}

// ServeHTTP implements http.Handler. It expects ContextKeyTargetURL to be stored in the request context.
// If not present, it attempts to proxy to r.URL.
func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target, ok := GetTargetURL(r.Context())
	if !ok || target == nil {
		target = r.URL
	}
	p.ServeProxy(w, r, target)
}

// ServeProxy proxies an incoming HTTP or WebSocket request to the designated target upstream URL.
func (p *ReverseProxy) ServeProxy(w http.ResponseWriter, r *http.Request, target *url.URL) {
	tracker := WrapTracker(w)

	// 1. WebSocket connection hijacking check
	if IsWebSocketRequest(r) {
		err := ServeWebSocket(tracker, r, target, p.bufferPool, 10*DefaultTransportOptions.DialTimeout, nil)
		if err != nil {
			WriteGatewayError(tracker, r, err, p.config.ErrorHandler)
		}
		return
	}

	// 2. Prepare outbound upstream request bound to client context
	outReq, err := NewUpstreamRequest(r.Context(), r, target)
	if err != nil {
		WriteGatewayError(tracker, r, err, p.config.ErrorHandler)
		return
	}

	// 3. Execute upstream round-trip
	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		WriteGatewayError(tracker, r, err, p.config.ErrorHandler)
		return
	}
	defer resp.Body.Close()

	// 4. Stream response body directly to client with zero heap accumulation
	_, _ = ServeStream(tracker, resp, p.bufferPool, p.config.FlushInterval)
}
