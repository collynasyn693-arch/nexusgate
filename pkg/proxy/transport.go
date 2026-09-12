package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// TransportOptions configures outbound upstream connection parameters for high-performance reverse proxying.
type TransportOptions struct {
	// DialTimeout specifies the maximum amount of time a dial will wait for a connect to complete.
	DialTimeout time.Duration

	// KeepAlive specifies the interval between keep-alive probes for an active network connection.
	KeepAlive time.Duration

	// IdleConnTimeout is the maximum amount of time an idle (keep-alive) connection will remain idle.
	IdleConnTimeout time.Duration

	// TLSHandshakeTimeout specifies the maximum amount of time waiting to complete TLS handshake.
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout specifies the amount of time to wait for a server's response headers.
	ResponseHeaderTimeout time.Duration

	// ExpectContinueTimeout specifies the amount of time to wait for a server's first response
	// headers after sending an Expect: 100-continue request.
	ExpectContinueTimeout time.Duration

	// MaxIdleConns controls the maximum number of idle (keep-alive) connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost controls the maximum idle (keep-alive) connections to keep per-host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost limits the total number of connections per host, including active and idle.
	MaxConnsPerHost int

	// TLSClientConfig specifies the TLS configuration to use for https upstreams.
	TLSClientConfig *tls.Config
}

// DefaultTransportOptions provides battle-tested defaults optimized for Android Termux ARM64:
// - Keep-Alive at 30s to keep mobile radio active during bursts without excessive battery drain.
// - DisableCompression: true to pass compressed bytes directly to client without decompressing in memory,
//   preserving real-time SSE chunk latency and saving ARM64 CPU cycles.
var DefaultTransportOptions = TransportOptions{
	DialTimeout:           10 * time.Second,
	KeepAlive:             30 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	MaxIdleConns:          128,
	MaxIdleConnsPerHost:   32,
	MaxConnsPerHost:       0, // unlimited
}

// DefaultTransport is the standard preconfigured UpstreamTransport.
var DefaultTransport UpstreamTransport = NewUpstreamTransport(DefaultTransportOptions)

// NewUpstreamTransport constructs an optimized *http.Transport tailored for reverse proxying.
func NewUpstreamTransport(opts TransportOptions) *http.Transport {
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = DefaultTransportOptions.DialTimeout
	}
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = DefaultTransportOptions.KeepAlive
	}
	if opts.IdleConnTimeout <= 0 {
		opts.IdleConnTimeout = DefaultTransportOptions.IdleConnTimeout
	}
	if opts.TLSHandshakeTimeout <= 0 {
		opts.TLSHandshakeTimeout = DefaultTransportOptions.TLSHandshakeTimeout
	}
	if opts.ResponseHeaderTimeout <= 0 {
		opts.ResponseHeaderTimeout = DefaultTransportOptions.ResponseHeaderTimeout
	}
	if opts.ExpectContinueTimeout <= 0 {
		opts.ExpectContinueTimeout = DefaultTransportOptions.ExpectContinueTimeout
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = DefaultTransportOptions.MaxIdleConns
	}
	if opts.MaxIdleConnsPerHost <= 0 {
		opts.MaxIdleConnsPerHost = DefaultTransportOptions.MaxIdleConnsPerHost
	}

	dialer := &net.Dialer{
		Timeout:   opts.DialTimeout,
		KeepAlive: opts.KeepAlive,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          opts.MaxIdleConns,
		MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
		MaxConnsPerHost:       opts.MaxConnsPerHost,
		IdleConnTimeout:       opts.IdleConnTimeout,
		TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
		ExpectContinueTimeout: opts.ExpectContinueTimeout,
		TLSClientConfig:       opts.TLSClientConfig,
		// CRITICAL ARCHITECTURAL DIRECTIVE: Disable transparent decompression!
		// Transparent gzip decompression forces Go's transport to buffer chunks into 32KB windows,
		// which breaks real-time SSE chunk flushing and inflates userland heap memory.
		DisableCompression: true,
	}

	return transport
}
