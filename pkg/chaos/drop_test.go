package chaos

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// mockConn tracks Close calls for socket drop testing.
type mockConn struct {
	net.Conn
	closed atomic.Bool
}

func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// mockHijackWriter implements http.ResponseWriter and http.Hijacker.
type mockHijackWriter struct {
	*httptest.ResponseRecorder
	conn *mockConn
}

func (m *mockHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	rw := bufio.NewReadWriter(bufio.NewReader(m.conn), bufio.NewWriter(m.conn))
	return m.conn, rw, nil
}

// wrappedHijackWriter wraps a writer and implements Unwrap().
type wrappedHijackWriter struct {
	inner http.ResponseWriter
}

func (w *wrappedHijackWriter) Header() http.Header         { return w.inner.Header() }
func (w *wrappedHijackWriter) Write(b []byte) (int, error) { return w.inner.Write(b) }
func (w *wrappedHijackWriter) WriteHeader(statusCode int)  { w.inner.WriteHeader(statusCode) }
func (w *wrappedHijackWriter) Unwrap() http.ResponseWriter { return w.inner }

func TestDropConnection_DirectHijacker(t *testing.T) {
	conn := &mockConn{}
	w := &mockHijackWriter{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             conn,
	}

	err := DropConnection(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !conn.closed.Load() {
		t.Errorf("expected underlying socket to be closed")
	}
}

func TestDropConnection_WrappedHijacker(t *testing.T) {
	conn := &mockConn{}
	inner := &mockHijackWriter{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             conn,
	}
	// Wrap twice
	wrapped := &wrappedHijackWriter{
		inner: &wrappedHijackWriter{
			inner: inner,
		},
	}

	err := DropConnection(wrapped)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !conn.closed.Load() {
		t.Errorf("expected wrapped connection to be closed")
	}
}

func TestDropConnection_NonHijackerPanic(t *testing.T) {
	w := httptest.NewRecorder() // Does not implement http.Hijacker

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on non-hijackable writer")
		}
		if r != http.ErrAbortHandler {
			t.Errorf("expected panic(http.ErrAbortHandler), got %v", r)
		}
	}()

	_ = DropConnection(w)
}

func TestSimulateTruncation(t *testing.T) {
	conn := &mockConn{}
	w := &mockHijackWriter{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             conn,
	}

	partial := []byte("incomplete data chunk")
	err := SimulateTruncation(w, partial, 5000)
	if err != nil {
		t.Fatalf("SimulateTruncation failed: %v", err)
	}

	if !conn.closed.Load() {
		t.Errorf("expected connection to be closed after truncation")
	}

	if w.Header().Get("Content-Length") != "5000" {
		t.Errorf("expected Content-Length: 5000, got %s", w.Header().Get("Content-Length"))
	}
}
