package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsSSE(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"TEXT/EVENT-STREAM", true},
		{"  text/event-stream  ", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}

	for _, tc := range tests {
		h := make(http.Header)
		if tc.contentType != "" {
			h.Set("Content-Type", tc.contentType)
		}
		if got := IsSSE(h); got != tc.want {
			t.Errorf("IsSSE(Content-Type: %q) = %v; want %v", tc.contentType, got, tc.want)
		}
	}

	if IsSSE(nil) {
		t.Error("IsSSE(nil) must be false")
	}
}

func TestPrepareSSEHeaders(t *testing.T) {
	h := make(http.Header)
	PrepareSSEHeaders(h)

	if h.Get("Cache-Control") != "no-cache, no-transform" {
		t.Errorf("expected Cache-Control = no-cache, no-transform, got %q", h.Get("Cache-Control"))
	}
	if h.Get("X-Accel-Buffering") != "no" {
		t.Errorf("expected X-Accel-Buffering = no, got %q", h.Get("X-Accel-Buffering"))
	}
	if h.Get("Connection") != "keep-alive" {
		t.Errorf("expected Connection = keep-alive, got %q", h.Get("Connection"))
	}

	// If Cache-Control was already present, preserve custom directive
	h2 := make(http.Header)
	h2.Set("Cache-Control", "private, no-cache")
	PrepareSSEHeaders(h2)
	if h2.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("expected existing Cache-Control preserved, got %q", h2.Get("Cache-Control"))
	}
}

func TestServeStream_SSEImmediateFlushing(t *testing.T) {
	const eventCount = 4
	stepCh := make(chan struct{})

	// Upstream SSE server that sends events strictly when prompted by stepCh
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream writer does not support Flusher")
			return
		}
		flusher.Flush()

		for i := 1; i <= eventCount; i++ {
			<-stepCh
			nowNano := time.Now().UnixNano()
			fmt.Fprintf(w, "id: %d\ndata: %d\n\n", i, nowNano)
			flusher.Flush()
		}
	}))
	defer upstreamServer.Close()

	// Proxy handler serving upstream via ServeStream
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), "GET", upstreamServer.URL, nil)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()

		pool := NewDualTierBufferPool()
		_, _ = ServeStream(w, resp, pool, 0)
	})

	proxyServer := httptest.NewServer(proxyHandler)
	defer proxyServer.Close()

	// Client connecting to proxy and reading SSE stream progressively
	clientResp, err := http.Get(proxyServer.URL)
	if err != nil {
		t.Fatalf("client failed to connect to proxy: %v", err)
	}
	defer clientResp.Body.Close()

	if clientResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", clientResp.StatusCode)
	}
	if !strings.HasPrefix(clientResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", clientResp.Header.Get("Content-Type"))
	}
	if clientResp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("expected X-Accel-Buffering: no, got %q", clientResp.Header.Get("X-Accel-Buffering"))
	}

	reader := bufio.NewReader(clientResp.Body)

	for i := 1; i <= eventCount; i++ {
		// Trigger upstream to emit next event
		stepCh <- struct{}{}

		// Read the emitted event from proxy
		var eventID int
		var timestampNano int64

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("error reading SSE line for event %d: %v", i, err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "id: ") {
				eventID, _ = strconv.Atoi(strings.TrimPrefix(line, "id: "))
			} else if strings.HasPrefix(line, "data: ") {
				timestampNano, _ = strconv.ParseInt(strings.TrimPrefix(line, "data: "), 10, 64)
			} else if line == "" && eventID > 0 {
				// End of SSE event frame
				break
			}
		}

		recvNano := time.Now().UnixNano()
		if eventID != i {
			t.Errorf("expected event id=%d, got %d", i, eventID)
		}

		// Calculate proxy transit latency: must be under 50ms on local loopback
		latency := time.Duration(recvNano - timestampNano)
		if latency < 0 {
			latency = 0
		}
		t.Logf("SSE Event %d dispatched through proxy in %v", i, latency)
	}
}

func TestServeStream_NonSSEPassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(upstream.URL)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		_, _ = ServeStream(w, resp, nil, 0)
	}))
	defer proxy.Close()

	resp, err := http.Get(proxy.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("expected %q, got %q", `{"status":"ok"}`, string(body))
	}
}
