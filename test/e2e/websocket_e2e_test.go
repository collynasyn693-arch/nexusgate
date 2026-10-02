package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexusgate/pkg/config"
	"nexusgate/pkg/engine"
	"nexusgate/pkg/proxy"
)

func TestGateway_E2E_WebSocketFullDuplex(t *testing.T) {
	// 1. Mock Upstream WebSocket Server that handles handshake and echoes data
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !proxy.IsWebSocketRequest(r) {
			http.Error(w, "expected websocket handshake", http.StatusBadRequest)
			return
		}

		conn, rw, err := proxy.HijackConnection(w)
		if err != nil {
			t.Errorf("upstream hijack failed: %v", err)
			return
		}
		defer conn.Close()

		// Return 101 Switching Protocols
		handshakeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"
		if _, err := rw.WriteString(handshakeResp); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}

		// Bidirectional echo: read payload and write back
		buf := make([]byte, 1024)
		for {
			n, err := rw.Reader.Read(buf)
			if n > 0 {
				if _, writeErr := conn.Write(buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}))
	defer upstreamServer.Close()

	upstreamURL, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}

	// 2. Configure Gateway Supervisor routing /ws to upstream
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "ws_test_gateway.sock")

	cfg := &config.GatewayConfig{
		Version: "1.0",
		Listener: config.ListenerConfig{
			Host: "127.0.0.1",
			Port: 8092,
		},
		Routes: []config.RouteConfig{
			{
				ID:         "ws-route",
				Path:       "/ws",
				Methods:    []string{"GET"},
				UpstreamID: "ws-pool",
			},
		},
		Upstreams: []config.UpstreamConfig{
			{
				ID:        "ws-pool",
				Algorithm: "round_robin",
				Targets: []config.TargetConfig{
					{URL: upstreamURL.String(), Weight: 1},
				},
			},
		},
	}

	sup, err := engine.NewSupervisor(cfg, sockPath)
	if err != nil {
		t.Fatalf("failed to construct supervisor: %v", err)
	}

	pipeline := sup.BuildPipeline()
	gwServer := httptest.NewServer(pipeline)
	defer gwServer.Close()

	// 3. Client connects to gateway server over raw TCP
	gwAddr := gwServer.Listener.Addr().String()
	clientConn, err := net.DialTimeout("tcp", gwAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial gateway: %v", err)
	}

	// 4. Send WebSocket Handshake request through Gateway
	handshakeReq := fmt.Sprintf(
		"GET /ws HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
			"Sec-WebSocket-Version: 13\r\n\r\n",
		gwAddr,
	)

	if _, err := clientConn.Write([]byte(handshakeReq)); err != nil {
		t.Fatalf("failed to write handshake request: %v", err)
	}

	// 5. Read Handshake Response from Gateway
	clientReader := bufio.NewReader(clientConn)
	statusLine, err := clientReader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read handshake status line: %v", err)
	}

	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected HTTP 101 Switching Protocols, got: %s", statusLine)
	}

	// Read remaining headers until blank line
	for {
		line, err := clientReader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read handshake header: %v", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// 6. Test Bidirectional Message Exchange
	testPayload := []byte("NexusGate full-duplex RFC6455 verification frame")
	if _, err := clientConn.Write(testPayload); err != nil {
		t.Fatalf("failed to send payload through websocket: %v", err)
	}

	recvBuf := make([]byte, len(testPayload))
	_, err = io.ReadFull(clientReader, recvBuf)
	if err != nil {
		t.Fatalf("failed to read echoed payload from websocket: %v", err)
	}

	if string(recvBuf) != string(testPayload) {
		t.Fatalf("echoed payload mismatch: got %q, want %q", string(recvBuf), string(testPayload))
	}

	// Verify active connection is tracked
	if sup.ActiveConns.Load() < 1 {
		t.Errorf("expected supervisor ActiveConns >= 1, got %d", sup.ActiveConns.Load())
	}

	// 7. Cleanly disconnect client and verify request completion telemetry
	_ = clientConn.Close()

	// Wait up to 500ms for handler goroutine to exit and record telemetry
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sup.TotalReqs.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if sup.TotalReqs.Load() == 0 {
		t.Errorf("expected supervisor telemetry TotalReqs > 0, got 0")
	}
}
