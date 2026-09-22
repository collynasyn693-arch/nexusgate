package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIsWebSocketRequest(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		upgrade string
		conn    string
		want    bool
	}{
		{"Valid standard WebSocket", "GET", "websocket", "Upgrade", true},
		{"Valid mixed case", "GET", "WebSocket", "keep-alive, Upgrade", true},
		{"Valid with extra connection tokens", "GET", "websocket", "close, upgrade", true},
		{"Invalid method POST", "POST", "websocket", "Upgrade", false},
		{"Invalid upgrade header", "GET", "http2", "Upgrade", false},
		{"Missing upgrade header", "GET", "", "Upgrade", false},
		{"Missing connection header", "GET", "websocket", "", false},
		{"Connection lacks upgrade", "GET", "websocket", "keep-alive", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, "http://example.com/ws", nil)
			if tc.upgrade != "" {
				req.Header.Set("Upgrade", tc.upgrade)
			}
			if tc.conn != "" {
				req.Header.Set("Connection", tc.conn)
			}
			if got := IsWebSocketRequest(req); got != tc.want {
				t.Errorf("IsWebSocketRequest() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestWebSocket_BidirectionalEcho(t *testing.T) {
	// 1. Mock Upstream WebSocket Server that echoes raw TCP stream back to client
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsWebSocketRequest(r) {
			http.Error(w, "expected websocket handshake", 400)
			return
		}

		rc := http.NewResponseController(w)
		conn, rw, err := rc.Hijack()
		if err != nil {
			t.Errorf("upstream hijack failed: %v", err)
			return
		}
		defer conn.Close()

		// Write standard 101 Switching Protocols response
		handshakeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n\r\n"
		if _, err := rw.WriteString(handshakeResp); err != nil {
			return
		}
		_ = rw.Flush()

		// Echo loop: read from client and write back
		buf := make([]byte, 4096)
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

	// 2. Reverse Proxy Server
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsWebSocketRequest(r) {
			err := ServeWebSocket(w, r, upstreamURL, nil, 5*time.Second, nil)
			if err != nil {
				t.Logf("ServeWebSocket ended: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("failed to parse proxy URL: %v", err)
	}

	// 3. Client: Establish raw TCP connection to proxy and perform handshake
	clientConn, err := net.DialTimeout("tcp", proxyURL.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("client failed to dial proxy: %v", err)
	}
	defer clientConn.Close()

	// Send HTTP handshake
	clientHandshake := fmt.Sprintf(
		"GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n",
		proxyURL.Host,
	)
	if _, err := clientConn.Write([]byte(clientHandshake)); err != nil {
		t.Fatalf("client failed to write handshake: %v", err)
	}

	clientReader := bufio.NewReader(clientConn)
	resp, err := http.ReadResponse(clientReader, nil)
	if err != nil {
		t.Fatalf("client failed to read handshake response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		t.Errorf("expected Upgrade: websocket, got %q", resp.Header.Get("Upgrade"))
	}

	// 4. Test Text Messaging Round-Trip
	textMessage := "NEXUSGATE_FULL_DUPLEX_WS_PUMP_TEST_MESSAGE"
	if _, err := clientConn.Write([]byte(textMessage)); err != nil {
		t.Fatalf("failed to write text message: %v", err)
	}

	recvTextBuf := make([]byte, len(textMessage))
	if _, err := io.ReadFull(clientReader, recvTextBuf); err != nil {
		t.Fatalf("failed to read echoed text message: %v", err)
	}
	if string(recvTextBuf) != textMessage {
		t.Fatalf("expected %q, got %q", textMessage, string(recvTextBuf))
	}

	// 5. Test Binary Data Messaging Round-Trip
	binaryPayload := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE, 0x01, 0x02, 0x03}
	if _, err := clientConn.Write(binaryPayload); err != nil {
		t.Fatalf("failed to write binary payload: %v", err)
	}

	recvBinBuf := make([]byte, len(binaryPayload))
	if _, err := io.ReadFull(clientReader, recvBinBuf); err != nil {
		t.Fatalf("failed to read echoed binary payload: %v", err)
	}
	if !bytes.Equal(recvBinBuf, binaryPayload) {
		t.Fatalf("binary payload mismatch: got %x, want %x", recvBinBuf, binaryPayload)
	}
}

func TestWebSocket_EarlyBufferedDataRetention(t *testing.T) {
	earlyDataReceived := make(chan []byte, 1)

	// Upstream that captures whatever bytes arrive after the handshake
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		conn, rw, err := rc.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()

		buf := make([]byte, 128)
		n, _ := rw.Reader.Read(buf)
		if n > 0 {
			earlyDataReceived <- buf[:n]
		}
	}))
	defer upstreamServer.Close()

	upstreamURL, _ := url.Parse(upstreamServer.URL)

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ServeWebSocket(w, r, upstreamURL, nil, 5*time.Second, nil)
	}))
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)

	clientConn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	// Send handshake AND early payload in the same TCP transmission
	earlyPayload := "EARLY_PIPELINED_FRAME"
	combinedRequest := fmt.Sprintf(
		"GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n%s",
		proxyURL.Host, earlyPayload,
	)
	if _, err := clientConn.Write([]byte(combinedRequest)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	clientReader := bufio.NewReader(clientConn)
	resp, err := http.ReadResponse(clientReader, nil)
	if err != nil {
		t.Fatalf("handshake response failed: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	select {
	case received := <-earlyDataReceived:
		if !strings.Contains(string(received), earlyPayload) {
			t.Fatalf("expected early payload %q, got %q", earlyPayload, string(received))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for early buffered payload: frames were dropped during hijack!")
	}
}
