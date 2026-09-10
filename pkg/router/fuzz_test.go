package router

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// FuzzRouter exercises the Radix Trie router against arbitrary, malformed, and adversarial paths.
// It verifies that Lookup and ServeHTTP never panic or enter infinite loops on any input string.
func FuzzRouter(f *testing.F) {
	seeds := []string{
		"/",
		"/api",
		"/api/v1",
		"/api/v1/users",
		"/api/v1/users/42",
		"/api/v1/users/42/profile",
		"/users/alice",
		"/users/alice/posts/99",
		"/static",
		"/static/css/app.css",
		"/static/../../../etc/passwd",
		"//api///v1//users//",
		"/a/b/c/d/e/f/g/h/i/j",
		"",
		"relative/path",
		"//",
		"///",
		"/..",
		"/../..",
		"/././.",
		"/%20/test",
		"/\x00/nullbyte",
		"/unicode/日本語/path",
		"/emoji/🚀/test",
		"/\r\n/crlf",
		"/////////////////////////////////////////////////////",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	r := New()
	_ = r.Handle(http.MethodGet, "/", dummyHandler("root"))
	_ = r.Handle(http.MethodGet, "/api/v1/users", dummyHandler("users"))
	_ = r.Handle(http.MethodGet, "/api/v1/users/:id", dummyHandler("user-id"))
	_ = r.Handle(http.MethodGet, "/users/:id/posts/:postID", dummyHandler("user-post"))
	_ = r.Handle(http.MethodGet, "/static/*filepath", dummyHandler("static"))
	_ = r.Handle(http.MethodPost, "/api/v1/submit", dummyHandler("submit"))

	f.Fuzz(func(t *testing.T, path string) {
		var p Params
		// Test direct Lookup with zero-alloc parameter extraction
		_, _ = r.Lookup(http.MethodGet, path, &p)

		// Test ServeHTTP through HTTP pipeline for valid HTTP request target URIs
		if strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n\x00 ") {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			r.ServeHTTP(w, req)
		}
	})
}

// TestFuzzPermutations executes 10,000 pseudo-random adversarial path queries to stress-test
// prefix splitting, param extraction, and catch-all backtracking under Termux ARM64.
func TestFuzzPermutations(t *testing.T) {
	r := New()
	_ = r.Handle(http.MethodGet, "/", dummyHandler("root"))
	_ = r.Handle(http.MethodGet, "/api/v1/users", dummyHandler("users"))
	_ = r.Handle(http.MethodGet, "/api/v1/users/:id", dummyHandler("user-id"))
	_ = r.Handle(http.MethodGet, "/api/v1/users/:id/profile", dummyHandler("user-profile"))
	_ = r.Handle(http.MethodGet, "/static/*filepath", dummyHandler("static"))
	_ = r.Handle(http.MethodGet, "/files/:category/*path", dummyHandler("category-files"))

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	charPool := "abcdefghijklmnopqrstuvwxyz0123456789-_./"
	var p Params

	for i := 0; i < 10000; i++ {
		length := rng.Intn(40)
		b := make([]byte, length)
		for j := 0; j < length; j++ {
			b[j] = charPool[rng.Intn(len(charPool))]
		}
		testPath := "/" + string(b)

		// Must never panic
		_, _ = r.Lookup(http.MethodGet, testPath, &p)

		if i%1000 == 0 {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, testPath, nil)
			r.ServeHTTP(w, req)
		}
	}

	// Verify deep recursion protection
	deepPath := "/" + strings.Repeat("segment/", 50)
	_, _ = r.Lookup(http.MethodGet, deepPath, &p)
	if t.Failed() {
		t.Fatal(fmt.Sprintf("Failed during fuzz permutations"))
	}
}
