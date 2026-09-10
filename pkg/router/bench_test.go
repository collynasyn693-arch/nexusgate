package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkStaticExactLookup(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/api/v1/health", dummyHandler("ok"))
	_ = r.Handle(http.MethodGet, "/api/v1/users", dummyHandler("users"))
	_ = r.Handle(http.MethodGet, "/api/v1/metrics", dummyHandler("metrics"))

	var p Params
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p = p[:0]
		h, ok := r.Lookup(http.MethodGet, "/api/v1/health", &p)
		if !ok || h == nil {
			b.Fatal("lookup failed")
		}
	}
}

func BenchmarkStaticExactParallel(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/api/v1/health", dummyHandler("ok"))
	_ = r.Handle(http.MethodGet, "/api/v1/users", dummyHandler("users"))

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var p Params
		for pb.Next() {
			p = p[:0]
			h, ok := r.Lookup(http.MethodGet, "/api/v1/health", &p)
			if !ok || h == nil {
				b.Fatal("lookup failed")
			}
		}
	})
}

func BenchmarkParamSingleLookup(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/users/:id", dummyHandler("user"))
	_ = r.Handle(http.MethodGet, "/users/:id/profile", dummyHandler("profile"))

	p := make(Params, 0, 8)
	path := "/users/42"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p = p[:0]
		h, ok := r.Lookup(http.MethodGet, path, &p)
		if !ok || h == nil {
			b.Fatal("lookup failed")
		}
	}
}

func BenchmarkParamMultiLookup(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/orgs/:org/repos/:repo/issues/:id", dummyHandler("issue"))

	p := make(Params, 0, 8)
	path := "/orgs/nexusgate/repos/core/issues/101"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p = p[:0]
		h, ok := r.Lookup(http.MethodGet, path, &p)
		if !ok || h == nil {
			b.Fatal("lookup failed")
		}
	}
}

func BenchmarkCatchAllLookup(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/static/*filepath", dummyHandler("static"))

	p := make(Params, 0, 8)
	path := "/static/css/theme/dark/app.min.css"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p = p[:0]
		h, ok := r.Lookup(http.MethodGet, path, &p)
		if !ok || h == nil {
			b.Fatal("lookup failed")
		}
	}
}

func BenchmarkMuxServeHTTP_ZeroAlloc(b *testing.B) {
	r := New()
	_ = r.Handle(http.MethodGet, "/api/v1/users/:id", dummyHandler("user"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}
