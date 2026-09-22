package proxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotHijackable is returned when the underlying ResponseWriter cannot be hijacked.
	ErrNotHijackable = errors.New("underlying response writer does not support connection hijacking")
	// ErrUpgradeFailed is returned when the upstream backend rejects the WebSocket upgrade.
	ErrUpgradeFailed = errors.New("upstream failed to return 101 switching protocols")
)

// IsWebSocketRequest checks if an HTTP request is an RFC 6455 WebSocket handshake request.
// It verifies that:
// 1. HTTP method is GET.
// 2. 'Upgrade' header contains 'websocket' (case-insensitive).
// 3. 'Connection' header tokens contain 'upgrade' (case-insensitive).
func IsWebSocketRequest(r *http.Request) bool {
	if r == nil || !strings.EqualFold(r.Method, http.MethodGet) {
		return false
	}

	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}

	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}
	return false
}

// ResolveHijacker resolves the http.Hijacker interface by recursively unwrapping
// any middleware ResponseWriter decorators until the raw socket hijacker is found.
func ResolveHijacker(w http.ResponseWriter) (http.Hijacker, error) {
	curr := w
	for curr != nil {
		if h, ok := curr.(http.Hijacker); ok {
			return h, nil
		}
		if u, ok := curr.(interface{ Unwrap() http.ResponseWriter }); ok {
			curr = u.Unwrap()
		} else {
			break
		}
	}
	return nil, ErrNotHijackable
}

// HijackConnection hijacks the underlying client network connection and returns the raw net.Conn
// and *bufio.ReadWriter.
func HijackConnection(w http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
	// First attempt via Go 1.20+ http.ResponseController
	rc := http.NewResponseController(w)
	clientConn, clientRw, err := rc.Hijack()
	if err == nil {
		return clientConn, clientRw, nil
	}

	// Fallback to recursive manual unwrapping
	hijacker, err := ResolveHijacker(w)
	if err != nil {
		return nil, nil, err
	}
	return hijacker.Hijack()
}

// DialUpstreamWebSocket connects to the target upstream URL for WebSocket proxying.
// Supports both plain TCP (ws:// or http://) and TLS-wrapped TCP (wss:// or https://).
func DialUpstreamWebSocket(target *url.URL, dialTimeout time.Duration, tlsConfig *tls.Config) (net.Conn, error) {
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}

	host := target.Host
	scheme := strings.ToLower(target.Scheme)

	// Ensure port is present
	if _, _, err := net.SplitHostPort(host); err != nil {
		if scheme == "wss" || scheme == "https" {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}

	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}

	if scheme == "wss" || scheme == "https" {
		cfg := tlsConfig
		if cfg == nil {
			serverName := target.Hostname()
			cfg = &tls.Config{ServerName: serverName}
		}
		return tls.DialWithDialer(dialer, "tcp", host, cfg)
	}

	return dialer.Dial("tcp", host)
}

// ForwardWebSocketHandshake writes the client's handshake request to the upstream connection,
// verifies that the upstream responds with HTTP 101 Switching Protocols, and relays the
// 101 response back to the client. Returns the upstream response and the buffered reader
// to prevent dropping early pipelined frames.
func ForwardWebSocketHandshake(r *http.Request, clientRw *bufio.ReadWriter, upstreamConn net.Conn, target *url.URL) (*http.Response, io.Reader, error) {
	// 1. Clone request and prepare headers for upstream
	outReq := r.Clone(r.Context())
	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host

	// Ensure required WebSocket upgrade headers are explicitly set
	outReq.Header.Set("Upgrade", "websocket")
	outReq.Header.Set("Connection", "Upgrade")
	MutateForwardedHeaders(outReq)

	// Synchronize Host header after mutating forwarded headers
	outReq.Host = target.Host

	// Remove hop-by-hop headers OTHER than Upgrade and Connection
	for hop := range hopByHopHeaders {
		if hop == "upgrade" || hop == "connection" {
			continue
		}
		outReq.Header.Del(hop)
	}

	// 2. Write client handshake request to upstream
	if err := outReq.Write(upstreamConn); err != nil {
		return nil, nil, fmt.Errorf("failed to write websocket handshake to upstream: %w", err)
	}

	// 3. Read upstream response
	upstreamReader := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(upstreamReader, outReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read websocket response from upstream: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, nil, ErrUpgradeFailed
	}

	// 4. Relay 101 response back to client and flush
	if err := resp.Write(clientRw); err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("failed to relay 101 response to client: %w", err)
	}
	if err := clientRw.Flush(); err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("failed to flush 101 response to client: %w", err)
	}

	return resp, upstreamReader, nil
}

// PumpWebSocket executes a full-duplex, bidirectional byte copy between the client
// and upstream connections using recycled buffers from pool.
// It coordinates symmetric connection teardown: when either side disconnects or errors,
// both connections are closed, unblocking the opposing goroutine immediately.
// Crucial: Client reads are drained from clientRw.Reader, and upstream reads are drained
// from upstreamReader (buffered reader) to ensure no early WebSocket frames sent in either
// direction are dropped!
func PumpWebSocket(clientConn net.Conn, clientRw *bufio.ReadWriter, upstreamConn net.Conn, upstreamReader io.Reader, pool BufferPool) {
	if pool == nil {
		pool = DefaultBufferPool
	}
	if upstreamReader == nil {
		upstreamReader = upstreamConn
	}

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}
	defer closeBoth()

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Client -> Upstream
	go func() {
		defer wg.Done()
		defer closeBoth()
		buf := pool.GetLarge()
		defer pool.PutLarge(buf)
		if len(buf) == 0 {
			buf = buf[:cap(buf)]
		}
		// Read from clientRw.Reader to avoid dropping bytes pre-buffered during handshake
		_, _ = io.CopyBuffer(upstreamConn, clientRw.Reader, buf)
	}()

	// Goroutine 2: Upstream -> Client
	go func() {
		defer wg.Done()
		defer closeBoth()
		buf := pool.GetLarge()
		defer pool.PutLarge(buf)
		if len(buf) == 0 {
			buf = buf[:cap(buf)]
		}
		// Read from upstreamReader to avoid dropping early server frames
		_, _ = io.CopyBuffer(clientConn, upstreamReader, buf)
	}()

	wg.Wait()
}

// ServeWebSocket upgrades, connects, handshakes, and pumps full-duplex WebSocket traffic.
func ServeWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL, pool BufferPool, dialTimeout time.Duration, tlsConfig *tls.Config) error {
	clientConn, clientRw, err := HijackConnection(w)
	if err != nil {
		return fmt.Errorf("websocket hijack failed: %w", err)
	}

	upstreamConn, err := DialUpstreamWebSocket(target, dialTimeout, tlsConfig)
	if err != nil {
		_ = clientConn.Close()
		return fmt.Errorf("websocket upstream dial failed: %w", err)
	}

	resp, upstreamReader, err := ForwardWebSocketHandshake(r, clientRw, upstreamConn, target)
	if err != nil {
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		return fmt.Errorf("websocket handshake forward failed: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	PumpWebSocket(clientConn, clientRw, upstreamConn, upstreamReader, pool)
	return nil
}

