package proxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	if !strings.Contains(host, ":") {
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
// 101 response back to the client.
func ForwardWebSocketHandshake(r *http.Request, clientRw *bufio.ReadWriter, upstreamConn net.Conn, target *url.URL) (*http.Response, error) {
	// 1. Clone request and prepare headers for upstream
	outReq := r.Clone(r.Context())
	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	// Ensure required WebSocket upgrade headers are explicitly set
	outReq.Header.Set("Upgrade", "websocket")
	outReq.Header.Set("Connection", "Upgrade")
	MutateForwardedHeaders(outReq)

	// Remove hop-by-hop headers OTHER than Upgrade and Connection
	for hop := range hopByHopHeaders {
		if hop == "upgrade" || hop == "connection" {
			continue
		}
		outReq.Header.Del(hop)
	}

	// 2. Write client handshake request to upstream
	if err := outReq.Write(upstreamConn); err != nil {
		return nil, fmt.Errorf("failed to write websocket handshake to upstream: %w", err)
	}

	// 3. Read upstream response
	upstreamReader := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(upstreamReader, outReq)
	if err != nil {
		return nil, fmt.Errorf("failed to read websocket response from upstream: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, ErrUpgradeFailed
	}

	// 4. Relay 101 response back to client and flush
	if err := resp.Write(clientRw); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to relay 101 response to client: %w", err)
	}
	if err := clientRw.Flush(); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to flush 101 response to client: %w", err)
	}

	return resp, nil
}
