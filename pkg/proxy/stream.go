package proxy

import (
	"io"
	"net/http"
	"time"
)

// ResponseWriterTracker wraps an http.ResponseWriter to track whether response headers
// have been committed and the total bytes written to the client.
type ResponseWriterTracker interface {
	http.ResponseWriter
	Written() bool
	StatusCode() int
	BytesWritten() int64
}

type responseTracker struct {
	http.ResponseWriter
	written      bool
	statusCode   int
	bytesWritten int64
}

func (rt *responseTracker) WriteHeader(code int) {
	if rt.written {
		return
	}
	rt.written = true
	rt.statusCode = code
	rt.ResponseWriter.WriteHeader(code)
}

func (rt *responseTracker) Write(p []byte) (int, error) {
	if !rt.written {
		rt.WriteHeader(http.StatusOK)
	}
	n, err := rt.ResponseWriter.Write(p)
	rt.bytesWritten += int64(n)
	return n, err
}

func (rt *responseTracker) Written() bool {
	return rt.written
}

func (rt *responseTracker) StatusCode() int {
	return rt.statusCode
}

func (rt *responseTracker) BytesWritten() int64 {
	return rt.bytesWritten
}

// Unwrap enables http.ResponseController to inspect the underlying writer.
func (rt *responseTracker) Unwrap() http.ResponseWriter {
	return rt.ResponseWriter
}

// Flush implements http.Flusher by forwarding to the underlying writer if supported.
func (rt *responseTracker) Flush() {
	if f, ok := rt.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// WrapTracker wraps an http.ResponseWriter with header and byte tracking.
func WrapTracker(w http.ResponseWriter) ResponseWriterTracker {
	return &responseTracker{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// flushWriter is an io.Writer that immediately calls http.Flusher after every successful write.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if n > 0 && fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

// StreamCopy copies bytes from src to dst using a buffer recycled from the BufferPool.
// If flushImmediately is true and dst (or an underlying wrapped writer) implements http.Flusher,
// each written chunk is flushed immediately.
// Invariant: len(buf) > 0 is strictly enforced to prevent io.CopyBuffer runtime panics.
func StreamCopy(dst io.Writer, src io.Reader, pool BufferPool, flushImmediately bool) (int64, error) {
	if pool == nil {
		pool = DefaultBufferPool
	}

	buf := pool.GetLarge()
	defer pool.PutLarge(buf)

	// Ensure slice length matches capacity for io.CopyBuffer
	if len(buf) == 0 {
		buf = buf[:cap(buf)]
	}

	targetWriter := dst
	if flushImmediately {
		if flusher, ok := resolveFlusher(dst); ok {
			targetWriter = &flushWriter{w: dst, f: flusher}
		}
	}

	return io.CopyBuffer(targetWriter, src, buf)
}

// resolveFlusher finds http.Flusher support across nested writer wrappers.
func resolveFlusher(w io.Writer) (http.Flusher, bool) {
	curr := any(w)
	for curr != nil {
		if f, ok := curr.(http.Flusher); ok {
			return f, true
		}
		if u, ok := curr.(interface{ Unwrap() http.ResponseWriter }); ok {
			curr = u.Unwrap()
		} else if u, ok := curr.(interface{ Unwrap() io.Writer }); ok {
			curr = u.Unwrap()
		} else {
			break
		}
	}
	return nil, false
}

// TimedFlusher wraps an io.Writer to periodically flush buffered data at flushInterval.
type TimedFlusher struct {
	w       io.Writer
	flusher http.Flusher
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewTimedFlusher starts a background flusher ticker if interval > 0 and flusher is non-nil.
func NewTimedFlusher(w io.Writer, interval time.Duration) io.Writer {
	if interval <= 0 {
		return w
	}
	flusher, ok := resolveFlusher(w)
	if !ok {
		return w
	}

	tf := &TimedFlusher{
		w:       w,
		flusher: flusher,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	go func() {
		defer close(tf.doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tf.flusher.Flush()
			case <-tf.stopCh:
				tf.flusher.Flush()
				return
			}
		}
	}()

	return tf
}

func (tf *TimedFlusher) Write(p []byte) (int, error) {
	return tf.w.Write(p)
}

// Stop terminates the background flush ticker and performs a final flush.
func (tf *TimedFlusher) Stop() {
	close(tf.stopCh)
	<-tf.doneCh
}
