package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func BenchmarkBufferPool_GetPutSmall(b *testing.B) {
	pool := NewDualTierBufferPool()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.GetSmall()
		buf[0] = byte(i)
		pool.PutSmall(buf)
	}
}

func BenchmarkBufferPool_GetPutLarge(b *testing.B) {
	pool := NewDualTierBufferPool()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.GetLarge()
		buf[0] = byte(i)
		pool.PutLarge(buf)
	}
}

func BenchmarkBufferPool_Parallel(b *testing.B) {
	pool := NewDualTierBufferPool()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.GetLarge()
			buf[0] = 1
			pool.PutLarge(buf)
		}
	})
}

func BenchmarkStreamCopy_SmallPayload(b *testing.B) {
	pool := NewDualTierBufferPool()
	payload := bytes.Repeat([]byte("nexusgate-stream-test-"), 100) // ~2.2KB
	dst := &countingWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(payload)
		_, _ = StreamCopy(dst, src, pool, false)
	}
}

func BenchmarkStreamCopy_LargePayload(b *testing.B) {
	pool := NewDualTierBufferPool()
	payload := bytes.Repeat([]byte("nexusgate-stream-test-"), 3000) // ~66KB
	dst := &countingWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(payload)
		_, _ = StreamCopy(dst, src, pool, false)
	}
}

func BenchmarkReverseProxy_ServeHTTP_Sequential(b *testing.B) {
	// Mock upstream server returning a small JSON response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","gateway":"nexusgate"}`))
	}))
	defer upstream.Close()

	targetURL, _ := url.Parse(upstream.URL)
	proxy := NewReverseProxy(Config{})

	req, _ := http.NewRequest("GET", "http://nexusgate.local/bench", nil)
	req.RemoteAddr = "192.168.1.10:48291"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		proxy.ServeProxy(rec, req, targetURL)
		if rec.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", rec.Code)
		}
	}
}

func BenchmarkReverseProxy_ServeHTTP_Parallel(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success"}`)
	}))
	defer upstream.Close()

	targetURL, _ := url.Parse(upstream.URL)
	proxy := NewReverseProxy(Config{})

	b.SetParallelism(100) // 100 concurrent workers
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		req, _ := http.NewRequest("GET", "http://nexusgate.local/bench", nil)
		req.RemoteAddr = "192.168.1.10:48291"
		for pb.Next() {
			rec := httptest.NewRecorder()
			proxy.ServeProxy(rec, req, targetURL)
		}
	})
}
