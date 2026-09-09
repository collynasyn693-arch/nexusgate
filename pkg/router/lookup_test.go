package router

import (
	"testing"
)

func TestStaticRouteLookup(t *testing.T) {
	root := newNode("", NodeStatic)

	hRoot := dummyHandler("root")
	hApi := dummyHandler("api")
	hUsers := dummyHandler("users")
	hUser := dummyHandler("user")
	hPosts := dummyHandler("posts")
	hHealth := dummyHandler("health")

	routes := map[string]Handler{
		"/":              hRoot,
		"/api":           hApi,
		"/api/v1/user":   hUser,
		"/api/v1/users":  hUsers,
		"/api/v1/posts":  hPosts,
		"/healthz":       hHealth,
	}

	for path, handler := range routes {
		if err := root.AddRoute(path, handler); err != nil {
			t.Fatalf("AddRoute(%q) failed: %v", path, err)
		}
	}

	tests := []struct {
		path        string
		expectFound bool
		expectedTag string
	}{
		{"/", true, "root"},
		{"/api", true, "api"},
		{"/api/v1/user", true, "user"},
		{"/api/v1/users", true, "users"},
		{"/api/v1/posts", true, "posts"},
		{"/healthz", true, "health"},
		// Non-matching paths
		{"/api/v1/user/", false, ""},
		{"/api/v1/userx", false, ""},
		{"/api/v2", false, ""},
		{"/health", false, ""},
		{"/nonexistent", false, ""},
		{"", false, ""},
		{"/API", false, ""}, // case sensitivity test
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			var params Params
			handler, found := root.Lookup(tc.path, &params)
			if found != tc.expectFound {
				t.Fatalf("Lookup(%q) found=%v, want %v", tc.path, found, tc.expectFound)
			}
			if tc.expectFound && handler == nil {
				t.Fatalf("Lookup(%q) returned nil handler", tc.path)
			}
		})
	}
}

func TestStaticRouteNilNodeAndEmpty(t *testing.T) {
	var n *node
	var params Params
	h, found := n.Lookup("/test", &params)
	if found || h != nil {
		t.Errorf("expected lookup on nil node to return (nil, false), got (%v, %v)", h, found)
	}

	root := newNode("", NodeStatic)
	h, found = root.Lookup("", &params)
	if found || h != nil {
		t.Errorf("expected lookup on empty path to return (nil, false), got (%v, %v)", h, found)
	}
}
