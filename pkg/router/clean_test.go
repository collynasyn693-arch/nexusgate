package router

import (
	"strings"
	"testing"
)

func TestCleanPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "/"},
		{"/", "/"},
		{"/api/v1/users", "/api/v1/users"},
		{"//api///v1//users", "/api/v1/users"},
		{"/api/v1/users/", "/api/v1/users/"},
		{"//api///v1//users//", "/api/v1/users/"},
		{"/a/b/../c", "/a/c"},
		{"/a/b/../../c", "/c"},
		{"/a/b/../../../c", "/c"},
		{"/../../etc/passwd", "/etc/passwd"},
		{"/../../../../", "/"},
		{"/./././", "/"},
		{"/a/./b/./c", "/a/b/c"},
		{"no-slash", "/no-slash"},
		{"no-slash/path", "/no-slash/path"},
		{"///", "/"},
		{"/....", "/...."}, // valid file name with multiple dots
		{"/a/..b", "/a/..b"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actual := CleanPath(tc.input)
			if actual != tc.expected {
				t.Errorf("CleanPath(%q) = %q, want %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestIsCleanPath(t *testing.T) {
	cleanPaths := []string{
		"/",
		"/api",
		"/api/v1/users",
		"/static/css/style.css",
		"/healthz",
		"/trailing/slash/",
	}

	dirtyPaths := []string{
		"",
		"relative/path",
		"//double/slash",
		"/./dot",
		"/../dotdot",
		"/a//b",
	}

	for _, p := range cleanPaths {
		if !isCleanPath(p) {
			t.Errorf("expected isCleanPath(%q) = true, got false", p)
		}
	}

	for _, p := range dirtyPaths {
		if isCleanPath(p) {
			t.Errorf("expected isCleanPath(%q) = false, got true", p)
		}
	}
}

func TestStripTrailingSlash(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/", "/"},
		{"/api/", "/api"},
		{"/api/v1/users/", "/api/v1/users"},
		{"/api/v1/users", "/api/v1/users"},
		{"", ""},
	}

	for _, tc := range tests {
		actual := StripTrailingSlash(tc.input)
		if actual != tc.expected {
			t.Errorf("StripTrailingSlash(%q) = %q, want %q", tc.input, actual, tc.expected)
		}
	}
}

func TestEnsureLeadingSlash(t *testing.T) {
	if got := EnsureLeadingSlash("users"); got != "/users" {
		t.Errorf("expected '/users', got %q", got)
	}
	if got := EnsureLeadingSlash("/users"); got != "/users" {
		t.Errorf("expected '/users', got %q", got)
	}
	if got := EnsureLeadingSlash(""); got != "/" {
		t.Errorf("expected '/', got %q", got)
	}
}

func TestCleanPathLargeBuffer(t *testing.T) {
	// Generate path longer than 256 bytes to test dynamic heap buffer fallback
	segments := make([]string, 50)
	for i := range segments {
		segments[i] = "segment"
	}
	longPath := "/" + strings.Join(segments, "/") + "/../terminal"
	cleaned := CleanPath(longPath)

	if !strings.HasPrefix(cleaned, "/segment/") || !strings.HasSuffix(cleaned, "/terminal") {
		t.Fatalf("unexpected cleaned long path: %s", cleaned)
	}
}
