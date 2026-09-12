package proxy

import (
	"testing"
	"time"
)

func TestNewUpstreamTransport_Defaults(t *testing.T) {
	tr := NewUpstreamTransport(TransportOptions{})
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}

	if !tr.DisableCompression {
		t.Error("expected DisableCompression = true to protect SSE and memory")
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 = true")
	}
	if tr.MaxIdleConns != 128 {
		t.Errorf("expected MaxIdleConns = 128, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 32 {
		t.Errorf("expected MaxIdleConnsPerHost = 32, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("expected IdleConnTimeout = 90s, got %v", tr.IdleConnTimeout)
	}
}

func TestNewUpstreamTransport_CustomOptions(t *testing.T) {
	custom := TransportOptions{
		DialTimeout:           5 * time.Second,
		KeepAlive:             15 * time.Second,
		IdleConnTimeout:       45 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		MaxConnsPerHost:       50,
	}

	tr := NewUpstreamTransport(custom)
	if tr.MaxIdleConns != 64 {
		t.Errorf("expected MaxIdleConns = 64, got %d", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 16 {
		t.Errorf("expected MaxIdleConnsPerHost = 16, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxConnsPerHost != 50 {
		t.Errorf("expected MaxConnsPerHost = 50, got %d", tr.MaxConnsPerHost)
	}
	if tr.IdleConnTimeout != 45*time.Second {
		t.Errorf("expected IdleConnTimeout = 45s, got %v", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout != 12*time.Second {
		t.Errorf("expected ResponseHeaderTimeout = 12s, got %v", tr.ResponseHeaderTimeout)
	}
	if !tr.DisableCompression {
		t.Error("expected DisableCompression to remain true")
	}
}
