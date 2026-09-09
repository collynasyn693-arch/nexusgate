package router

import (
	"errors"
	"net/http"
	"testing"
)

type testHandler struct {
	tag string
}

func (th *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, p Params) {}

// dummyHandler creates a simple Handler for testing.
func dummyHandler(tag string) Handler {
	return &testHandler{tag: tag}
}

func TestStaticRouteInsertionBasic(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("root")

	if err := root.AddRoute("/users", h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if root.path != "/users" {
		t.Errorf("expected root.path to be /users, got %q", root.path)
	}
	if root.handler == nil {
		t.Error("expected root.handler to be set")
	}
}

func TestStaticRouteTreeSplitting(t *testing.T) {
	root := newNode("", NodeStatic)

	h1 := dummyHandler("user")
	h2 := dummyHandler("users")
	h3 := dummyHandler("v2")
	h4 := dummyHandler("orders")

	// Insert "/api/v1/user"
	if err := root.AddRoute("/api/v1/user", h1); err != nil {
		t.Fatalf("AddRoute(/api/v1/user) failed: %v", err)
	}

	// Insert "/api/v1/users" (shares "/api/v1/user" prefix)
	if err := root.AddRoute("/api/v1/users", h2); err != nil {
		t.Fatalf("AddRoute(/api/v1/users) failed: %v", err)
	}

	// Insert "/api/v2" (splits at "/api/")
	if err := root.AddRoute("/api/v2", h3); err != nil {
		t.Fatalf("AddRoute(/api/v2) failed: %v", err)
	}

	// Insert "/orders" (splits at "/")
	if err := root.AddRoute("/orders", h4); err != nil {
		t.Fatalf("AddRoute(/orders) failed: %v", err)
	}

	// Check root
	if root.path != "/" {
		t.Errorf("expected root.path '/', got %q", root.path)
	}
	if len(root.indices) != 2 {
		t.Errorf("expected 2 indices at root ('a', 'o'), got %q", root.indices)
	}
	if len(root.children) != 2 {
		t.Fatalf("expected 2 children at root, got %d", len(root.children))
	}
}

func TestStaticRouteDuplicateRejection(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("dup")

	if err := root.AddRoute("/test", h); err != nil {
		t.Fatalf("unexpected error on first insert: %v", err)
	}

	err := root.AddRoute("/test", h)
	if !errors.Is(err, ErrDuplicateRoute) {
		t.Fatalf("expected ErrDuplicateRoute, got %v", err)
	}
}

func TestStaticRouteValidation(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("val")

	if err := root.AddRoute("", h); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("expected ErrInvalidPath on empty path, got %v", err)
	}

	if err := root.AddRoute("no-leading-slash", h); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("expected ErrInvalidPath on path without leading slash, got %v", err)
	}

	if err := root.AddRoute("/valid", nil); !errors.Is(err, ErrNilHandler) {
		t.Errorf("expected ErrNilHandler on nil handler, got %v", err)
	}
}

func TestStaticRouteTreeCloning(t *testing.T) {
	root := newNode("", NodeStatic)
	_ = root.AddRoute("/api/v1/users", dummyHandler("users"))
	_ = root.AddRoute("/api/v1/posts", dummyHandler("posts"))
	_ = root.AddRoute("/api/v2", dummyHandler("v2"))

	cloned := root.clone()

	if cloned == root {
		t.Fatal("cloned pointer should not equal original root pointer")
	}
	if cloned.path != root.path {
		t.Errorf("path mismatch: %q vs %q", cloned.path, root.path)
	}
	if len(cloned.children) != len(root.children) {
		t.Errorf("children length mismatch: %d vs %d", len(cloned.children), len(root.children))
	}

	// Mutating original must not affect clone
	_ = root.AddRoute("/api/v3", dummyHandler("v3"))
	if len(cloned.children) == len(root.children) {
		t.Error("clone was unexpectedly mutated after adding route to original")
	}
}
