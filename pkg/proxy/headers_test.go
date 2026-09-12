package proxy

import (
	"net/http"
	"testing"
)

func TestRemoveHopByHopHeaders_StaticHeaders(t *testing.T) {
	h := make(http.Header)
	h.Set("Connection", "close")
	h.Set("Keep-Alive", "timeout=5, max=1000")
	h.Set("Proxy-Authenticate", "Basic")
	h.Set("Proxy-Authorization", "Basic 12345")
	h.Set("Proxy-Connection", "keep-alive")
	h.Set("Trailer", "X-Checksum")
	h.Set("Trailers", "X-Legacy")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("Upgrade", "websocket")
	h.Set("X-Custom-App", "NexusGate")

	RemoveHopByHopHeaders(h)

	// All standard hop-by-hop headers must be gone
	for _, key := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Trailer", "Trailers", "Transfer-Encoding", "Upgrade",
	} {
		if val := h.Get(key); val != "" {
			t.Errorf("expected header %q to be deleted, got %q", key, val)
		}
	}

	// End-to-end application headers must remain intact
	if h.Get("X-Custom-App") != "NexusGate" {
		t.Errorf("expected X-Custom-App to remain, got %q", h.Get("X-Custom-App"))
	}
}

func TestRemoveHopByHopHeaders_CaseInsensitive(t *testing.T) {
	h := make(http.Header)
	h["cOnNeCtIoN"] = []string{"close"}
	h["kEeP-aLiVe"] = []string{"300"}
	h["pRoXy-cOnNeCtIoN"] = []string{"keep-alive"}
	h["tRaIlEr"] = []string{"X-Sig"}

	RemoveHopByHopHeaders(h)

	for _, key := range []string{"Connection", "Keep-Alive", "Proxy-Connection", "Trailer"} {
		if val := h.Get(key); val != "" {
			t.Errorf("expected case-insensitive deletion for %q, got %q", key, val)
		}
	}
}

func TestRemoveHopByHopHeaders_DynamicConnectionTokens(t *testing.T) {
	h := make(http.Header)
	h.Set("Connection", "close, X-Dynamic-One, X-Dynamic-Two, Keep-Alive")
	h.Set("X-Dynamic-One", "val1")
	h.Set("X-Dynamic-Two", "val2")
	h.Set("X-Keep-This", "preserve")

	RemoveHopByHopHeaders(h)

	if val := h.Get("X-Dynamic-One"); val != "" {
		t.Errorf("expected X-Dynamic-One to be stripped, got %q", val)
	}
	if val := h.Get("X-Dynamic-Two"); val != "" {
		t.Errorf("expected X-Dynamic-Two to be stripped, got %q", val)
	}
	if val := h.Get("X-Keep-This"); val != "preserve" {
		t.Errorf("expected X-Keep-This to be preserved, got %q", val)
	}
	if val := h.Get("Connection"); val != "" {
		t.Errorf("expected Connection header itself to be deleted, got %q", val)
	}
}

func TestRemoveHopByHopHeaders_ProtectedHeadersSecurity(t *testing.T) {
	h := make(http.Header)
	// Malicious client attempting to strip critical routing and auth headers via Connection
	h.Set("Connection", "Host, Content-Length, Content-Type, Authorization, X-Forwarded-For")
	h.Set("Host", "api.nexusgate.internal")
	h.Set("Content-Length", "1024")
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer secret-token")
	h.Set("X-Forwarded-For", "192.168.1.1")

	RemoveHopByHopHeaders(h)

	// Protected headers must NOT be deleted
	if h.Get("Host") != "api.nexusgate.internal" {
		t.Errorf("security breach: Host was stripped!")
	}
	if h.Get("Content-Length") != "1024" {
		t.Errorf("security breach: Content-Length was stripped!")
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("security breach: Content-Type was stripped!")
	}
	if h.Get("Authorization") != "Bearer secret-token" {
		t.Errorf("security breach: Authorization was stripped!")
	}
	if h.Get("X-Forwarded-For") != "192.168.1.1" {
		t.Errorf("security breach: X-Forwarded-For was stripped!")
	}
}

func TestRemoveHopByHopHeaders_TEHeader(t *testing.T) {
	// Case 1: Non-trailers TE header -> must be deleted
	h1 := make(http.Header)
	h1.Set("TE", "deflate, gzip")
	RemoveHopByHopHeaders(h1)
	if val := h1.Get("TE"); val != "" {
		t.Errorf("expected non-trailers TE to be deleted, got %q", val)
	}

	// Case 2: TE: trailers (exact) -> must be preserved for gRPC
	h2 := make(http.Header)
	h2.Set("TE", "trailers")
	RemoveHopByHopHeaders(h2)
	if val := h2.Get("TE"); val != "trailers" {
		t.Errorf("expected 'TE: trailers' to be preserved, got %q", val)
	}

	// Case 3: TE: Trailers (mixed case) -> preserved as canonical "trailers"
	h3 := make(http.Header)
	h3.Set("TE", "Trailers")
	RemoveHopByHopHeaders(h3)
	if val := h3.Get("TE"); val != "trailers" {
		t.Errorf("expected mixed-case 'TE: Trailers' to be preserved, got %q", val)
	}
}

func TestIsHopByHopHeader(t *testing.T) {
	if !IsHopByHopHeader("Connection") {
		t.Errorf("expected Connection to be hop-by-hop")
	}
	if !IsHopByHopHeader("proxy-connection") {
		t.Errorf("expected proxy-connection to be hop-by-hop")
	}
	if IsHopByHopHeader("X-Forwarded-For") {
		t.Errorf("expected X-Forwarded-For NOT to be hop-by-hop")
	}
}
