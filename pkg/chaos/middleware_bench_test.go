package chaos

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type noopResponseWriter struct{}

func (n *noopResponseWriter) Header() http.Header         { return nil }
func (n *noopResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noopResponseWriter) WriteHeader(statusCode int)  {}

func BenchmarkMiddleware_Disabled(b *testing.B) {
	eng, _ := NewEngine(Config{Enabled: false}, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := eng.Wrap(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?id=12345&filter=active", nil)
	w := &noopResponseWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_DisabledParallel(b *testing.B) {
	eng, _ := NewEngine(Config{Enabled: false}, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := eng.Wrap(next)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users?id=12345&filter=active", nil)
		w := &noopResponseWriter{}
		for pb.Next() {
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkMiddleware_Enabled_NoChaos(b *testing.B) {
	eng, _ := NewEngine(Config{
		Enabled:  true,
		AdminKey: "adminkey",
	}, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := eng.Wrap(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?id=12345&sort=desc", nil)
	w := &noopResponseWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMiddleware_Enabled_NoChaosParallel(b *testing.B) {
	eng, _ := NewEngine(Config{
		Enabled:  true,
		AdminKey: "adminkey",
	}, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := eng.Wrap(next)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users?id=12345&sort=desc", nil)
		w := &noopResponseWriter{}
		for pb.Next() {
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkParseQueryParams_Inactive(b *testing.B) {
	query := "id=9999&limit=100&offset=50&category=electronics"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = ParseQueryParams(query, 30*time.Second, 64*1024)
	}
}

func BenchmarkParseQueryParams_Active(b *testing.B) {
	query := "id=9999&__delay=50ms&__status=503"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = ParseQueryParams(query, 30*time.Second, 64*1024)
	}
}

func BenchmarkFaultInjector_ShouldInject(b *testing.B) {
	fi := NewFaultInjector(0.10)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = fi.ShouldInject(0.10)
	}
}

func BenchmarkSecurityGuard_Authorize(b *testing.B) {
	sg, _ := NewSecurityGuard(Config{
		Enabled:  true,
		AdminKey: "secret-vault-key-xyz",
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderChaosKey, "secret-vault-key-xyz")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = sg.Authorize(req)
	}
}
