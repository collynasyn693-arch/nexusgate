package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// repeatingReader generates an infinite stream of a repeating byte pattern without memory allocation.
type repeatingReader struct {
	pattern []byte
	idx     int
}

func newRepeatingReader(pattern string) *repeatingReader {
	return &repeatingReader{pattern: []byte(pattern)}
}

func (r *repeatingReader) Read(p []byte) (n int, err error) {
	if len(r.pattern) == 0 {
		return 0, io.EOF
	}
	for n < len(p) {
		copied := copy(p[n:], r.pattern[r.idx:])
		n += copied
		r.idx = (r.idx + copied) % len(r.pattern)
	}
	return n, nil
}

// countingWriter counts total written bytes and tracks memory usage.
type countingWriter struct {
	total int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	cw.total += int64(len(p))
	return len(p), nil
}

func TestStreamCopy_LargeFileMemoryBound(t *testing.T) {
	const streamSize = int64(50 * 1024 * 1024) // 50 Megabytes
	src := io.LimitReader(newRepeatingReader("NEXUSGATE_ARM64_STREAMING_ZERO_ALLOC_BUFFER_TEST_PAYLOAD"), streamSize)
	dst := &countingWriter{}
	pool := NewDualTierBufferPool()

	// Measure baseline memory
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	written, err := StreamCopy(dst, src, pool, false)
	if err != nil {
		t.Fatalf("StreamCopy failed: %v", err)
	}
	if written != streamSize {
		t.Fatalf("expected written=%d bytes, got %d", streamSize, written)
	}
	if dst.total != streamSize {
		t.Fatalf("expected dst.total=%d bytes, got %d", streamSize, dst.total)
	}

	// Measure memory after 50MB stream transfer
	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	// Memory accumulation must not exceed 2MB on Termux ARM64
	// (Proves that the 50MB stream was NOT buffered into heap memory)
	var heapDelta int64
	if mAfter.HeapAlloc > mBefore.HeapAlloc {
		heapDelta = int64(mAfter.HeapAlloc - mBefore.HeapAlloc)
	}
	const maxAllowedGrowth = int64(2 * 1024 * 1024) // 2MB
	if heapDelta > maxAllowedGrowth {
		t.Fatalf("memory leaked during 50MB stream: heap grew by %d bytes (limit: %d bytes)",
			heapDelta, maxAllowedGrowth)
	}
}

func TestResponseTracker(t *testing.T) {
	rec := httptest.NewRecorder()
	tracker := WrapTracker(rec)

	if tracker.Written() {
		t.Error("expected tracker.Written() == false initially")
	}

	tracker.WriteHeader(http.StatusAccepted)
	if !tracker.Written() {
		t.Error("expected tracker.Written() == true after WriteHeader")
	}
	if tracker.StatusCode() != http.StatusAccepted {
		t.Errorf("expected StatusCode=%d, got %d", http.StatusAccepted, tracker.StatusCode())
	}

	// Test double WriteHeader does not override status code
	tracker.WriteHeader(http.StatusInternalServerError)
	if tracker.StatusCode() != http.StatusAccepted {
		t.Errorf("expected StatusCode to remain %d, got %d", http.StatusAccepted, tracker.StatusCode())
	}

	// Test write tracking
	payload := []byte("hello stream")
	n, err := tracker.Write(payload)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(payload) {
		t.Errorf("expected written %d bytes, got %d", len(payload), n)
	}
	if tracker.BytesWritten() != int64(len(payload)) {
		t.Errorf("expected BytesWritten()=%d, got %d", len(payload), tracker.BytesWritten())
	}
}

type mockFlusherWriter struct {
	bytes.Buffer
	flushedCount int
}

func (mfw *mockFlusherWriter) Flush() {
	mfw.flushedCount++
}

func TestStreamCopy_FlushImmediately(t *testing.T) {
	mockWriter := &mockFlusherWriter{}
	data := []byte("chunk-1-data-chunk-2-data")
	src := bytes.NewReader(data)
	pool := NewDualTierBufferPool()

	written, err := StreamCopy(mockWriter, src, pool, true)
	if err != nil {
		t.Fatalf("StreamCopy failed: %v", err)
	}
	if written != int64(len(data)) {
		t.Errorf("expected %d bytes written, got %d", len(data), written)
	}
	if mockWriter.flushedCount == 0 {
		t.Error("expected Flush to be called when flushImmediately is true")
	}
}

func TestTimedFlusher(t *testing.T) {
	mockWriter := &mockFlusherWriter{}
	tf := NewTimedFlusher(mockWriter, 10*time.Millisecond)

	_, err := tf.Write([]byte("periodic data"))
	if err != nil {
		t.Fatalf("tf.Write failed: %v", err)
	}

	// Wait for ticker to fire
	time.Sleep(30 * time.Millisecond)

	if closer, ok := tf.(interface{ Stop() }); ok {
		closer.Stop()
	}

	if mockWriter.flushedCount == 0 {
		t.Error("expected TimedFlusher to trigger Flush at interval")
	}
}
