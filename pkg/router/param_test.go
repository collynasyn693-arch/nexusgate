package router

import (
	"errors"
	"testing"
)

func TestSingleParamRoute(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("user-detail")

	if err := root.AddRoute("/users/:id", h); err != nil {
		t.Fatalf("AddRoute(/users/:id) failed: %v", err)
	}

	tests := []struct {
		path        string
		expectFound bool
		expectedID  string
	}{
		{"/users/123", true, "123"},
		{"/users/alice", true, "alice"},
		{"/users/u-987-xyz", true, "u-987-xyz"},
		{"/users/", false, ""},
		{"/users", false, ""},
		{"/user/123", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			var params Params
			handler, found := root.Lookup(tc.path, &params)
			if found != tc.expectFound {
				t.Fatalf("Lookup(%q) found=%v, want %v", tc.path, found, tc.expectFound)
			}
			if tc.expectFound {
				if handler == nil {
					t.Fatalf("Lookup(%q) returned nil handler", tc.path)
				}
				id := params.ByName("id")
				if id != tc.expectedID {
					t.Errorf("expected param 'id'=%q, got %q", tc.expectedID, id)
				}
				val, ok := params.Get("id")
				if !ok || val != tc.expectedID {
					t.Errorf("params.Get('id') returned (%q, %v), want (%q, true)", val, ok, tc.expectedID)
				}
			}
		})
	}
}

func TestMultiParamRoute(t *testing.T) {
	root := newNode("", NodeStatic)
	hPostDetail := dummyHandler("post-detail")
	hPostComment := dummyHandler("post-comment")

	if err := root.AddRoute("/users/:id/posts/:postID", hPostDetail); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	if err := root.AddRoute("/users/:id/posts/:postID/comments/:commentID", hPostComment); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	var params Params
	handler, found := root.Lookup("/users/42/posts/101", &params)
	if !found || handler == nil {
		t.Fatalf("expected /users/42/posts/101 to match")
	}
	if params.ByName("id") != "42" {
		t.Errorf("expected id=42, got %q", params.ByName("id"))
	}
	if params.ByName("postID") != "101" {
		t.Errorf("expected postID=101, got %q", params.ByName("postID"))
	}

	// Test three params
	params = params[:0]
	handler, found = root.Lookup("/users/alice/posts/draft-1/comments/c-99", &params)
	if !found || handler == nil {
		t.Fatalf("expected 3-param route to match")
	}
	if params.ByName("id") != "alice" || params.ByName("postID") != "draft-1" || params.ByName("commentID") != "c-99" {
		t.Errorf("params extraction mismatch: %+v", params)
	}
}

func TestParamAndStaticBacktracking(t *testing.T) {
	root := newNode("", NodeStatic)
	hNew := dummyHandler("user-new")
	hParam := dummyHandler("user-param")

	if err := root.AddRoute("/users/new", hNew); err != nil {
		t.Fatalf("AddRoute(/users/new) failed: %v", err)
	}
	if err := root.AddRoute("/users/:id", hParam); err != nil {
		t.Fatalf("AddRoute(/users/:id) failed: %v", err)
	}

	// Exact match on static route
	var p1 Params
	h1, found1 := root.Lookup("/users/new", &p1)
	if !found1 || h1 != hNew {
		t.Errorf("expected /users/new to match static handler")
	}
	if len(p1) != 0 {
		t.Errorf("expected 0 params for static route, got %d", len(p1))
	}

	// Backtracking: /users/news starts with 'n' (matching 'new' prefix),
	// but dead-ends and must backtrack to /users/:id with id="news"
	var p2 Params
	h2, found2 := root.Lookup("/users/news", &p2)
	if !found2 || h2 != hParam {
		t.Errorf("expected /users/news to backtrack and match param handler")
	}
	if p2.ByName("id") != "news" {
		t.Errorf("expected id='news', got %q", p2.ByName("id"))
	}
}

func TestParamConflictDetection(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("test")

	if err := root.AddRoute("/users/:id", h); err != nil {
		t.Fatalf("first AddRoute failed: %v", err)
	}

	// Conflicting parameter name at the same level
	err := root.AddRoute("/users/:userID", h)
	if !errors.Is(err, ErrParamConflict) {
		t.Errorf("expected ErrParamConflict, got %v", err)
	}
}

func TestEmptyParamValidation(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("test")

	if err := root.AddRoute("/users/:", h); !errors.Is(err, ErrEmptyWildcardName) {
		t.Errorf("expected ErrEmptyWildcardName for '/users/:', got %v", err)
	}

	if err := root.AddRoute("/users/:/posts", h); !errors.Is(err, ErrEmptyWildcardName) {
		t.Errorf("expected ErrEmptyWildcardName for '/users/:/posts', got %v", err)
	}
}
