package chaos

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// ErrHijackNotSupported is returned when response writer does not support connection hijacking.
var ErrHijackNotSupported = errors.New("nexusgate: response writer does not support hijacking")

// UnwrapResponseWriter unwraps wrapped http.ResponseWriter layers to locate the underlying transport writer.
func UnwrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	curr := w
	for {
		if u, ok := curr.(interface{ Unwrap() http.ResponseWriter }); ok {
			wrapped := u.Unwrap()
			if wrapped != nil {
				curr = wrapped
				continue
			}
		}
		break
	}
	return curr
}

// DropConnection forcefully terminates the client connection via socket hijacking and TCP RST.
// If the writer does not support hijacking (e.g. HTTP/2 or mock writers), it panics with
// http.ErrAbortHandler, which cleanly aborts the active stream without standard response framing.
func DropConnection(w http.ResponseWriter) error {
	unwrapped := UnwrapResponseWriter(w)

	if hj, ok := unwrapped.(http.Hijacker); ok {
		conn, _, err := hj.Hijack()
		if err == nil {
			closeWithRST(conn)
			return nil
		}
	}

	// Fallback for HTTP/2 or non-hijackable connections
	panic(http.ErrAbortHandler)
}

// SimulateTruncation writes an incomplete response body with a larger Content-Length header,
// then abruptly drops the connection, causing the client to experience an "unexpected EOF".
func SimulateTruncation(w http.ResponseWriter, partialBody []byte, claimedLength int) error {
	if claimedLength <= len(partialBody) {
		claimedLength = len(partialBody) + 1024
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", claimedLength))
	w.WriteHeader(http.StatusOK)

	if len(partialBody) > 0 {
		_, _ = w.Write(partialBody)
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	return DropConnection(w)
}

// closeWithRST attempts to set SO_LINGER = 0 to issue an abrupt TCP RST packet upon Close.
func closeWithRST(conn net.Conn) {
	if conn == nil {
		return
	}

	var tcpConn *net.TCPConn

	switch c := conn.(type) {
	case *net.TCPConn:
		tcpConn = c
	case *tls.Conn:
		if rawConn := c.NetConn(); rawConn != nil {
			if tc, ok := rawConn.(*net.TCPConn); ok {
				tcpConn = tc
			}
		}
	}

	if tcpConn != nil {
		_ = tcpConn.SetLinger(0)
	}

	_ = conn.Close()
}
