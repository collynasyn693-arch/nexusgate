package router

import (
	"errors"
	"testing"
)

func TestCatchAllRouteBasic(t *testing.T) {
	root := newNode("", NodeStatic)
	hStatic := dummyHandler("static-files")

	if err := root.AddRoute("/static/*filepath", hStatic); err != nil {
		t.Fatalf("AddRoute(/static/*filepath) failed: %v", err)
	}

	tests := []struct {
		path        string
		expectFound bool
		expectedVal string
	}{
		{"/static/css/app.css", true, "css/app.css"},
		{"/static/images/hero.png", true, "images/hero.png"},
		{"/static/bundle.js", true, "bundle.js"},
		{"/static/", true, ""},
		{"/static", false, ""},
		{"/other/path", false, ""},
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
				val := params.ByName("filepath")
				if val != tc.expectedVal {
					t.Errorf("expected filepath=%q, got %q", tc.expectedVal, val)
				}
			}
		})
	}
}

func TestCatchAllRoot(t *testing.T) {
	root := newNode("", NodeStatic)
	hRoot := dummyHandler("catch-all-root")

	if err := root.AddRoute("/*rest", hRoot); err != nil {
		t.Fatalf("AddRoute(/*rest) failed: %v", err)
	}

	var params Params
	h, found := root.Lookup("/any/arbitrary/path/here", &params)
	if !found || h == nil {
		t.Fatal("expected /*rest to match")
	}
	if params.ByName("rest") != "any/arbitrary/path/here" {
		t.Errorf("expected rest='any/arbitrary/path/here', got %q", params.ByName("rest"))
	}
}

func TestRoutePrecedenceOrdering(t *testing.T) {
	root := newNode("", NodeStatic)

	hStatic := dummyHandler("static-default")
	hParam := dummyHandler("param-name")
	hCatchAll := dummyHandler("catchall-all")

	// Precedence order: Static > Param > Catch-All
	if err := root.AddRoute("/files/default", hStatic); err != nil {
		t.Fatalf("AddRoute(/files/default) failed: %v", err)
	}
	if err := root.AddRoute("/files/:name", hParam); err != nil {
		t.Fatalf("AddRoute(/files/:name) failed: %v", err)
	}
	if err := root.AddRoute("/files/*all", hCatchAll); err != nil {
		t.Fatalf("AddRoute(/files/*all) failed: %v", err)
	}

	// 1. Static match
	var p1 Params
	h, found := root.Lookup("/files/default", &p1)
	if !found || h != hStatic {
		t.Errorf("expected /files/default to match static handler")
	}
	if len(p1) != 0 {
		t.Errorf("expected 0 params for static match, got %d", len(p1))
	}

	// 2. Param match (single segment)
	var p2 Params
	h, found = root.Lookup("/files/document.pdf", &p2)
	if !found || h != hParam {
		t.Errorf("expected /files/document.pdf to match param handler")
	}
	if p2.ByName("name") != "document.pdf" {
		t.Errorf("expected name='document.pdf', got %q", p2.ByName("name"))
	}

	// 3. Catch-All match (multiple slash segments)
	var p3 Params
	h, found = root.Lookup("/files/archive/2026/report.pdf", &p3)
	if !found || h != hCatchAll {
		t.Errorf("expected /files/archive/2026/report.pdf to match catchall handler")
	}
	if p3.ByName("all") != "archive/2026/report.pdf" {
		t.Errorf("expected all='archive/2026/report.pdf', got %q", p3.ByName("all"))
	}
}

func TestCatchAllValidation(t *testing.T) {
	root := newNode("", NodeStatic)
	h := dummyHandler("test")

	// Non-terminal catch-all
	err := root.AddRoute("/static/*filepath/extra", h)
	if !errors.Is(err, ErrInvalidCatchAll) {
		t.Errorf("expected ErrInvalidCatchAll for non-terminal wildcard, got %v", err)
	}

	// Empty wildcard name
	err = root.AddRoute("/static/*", h)
	if !errors.Is(err, ErrEmptyWildcardName) {
		t.Errorf("expected ErrEmptyWildcardName for '/static/*', got %v", err)
	}

	// Conflicting wildcard name
	if err := root.AddRoute("/docs/*path", h); err != nil {
		t.Fatalf("AddRoute(/docs/*path) failed: %v", err)
	}
	err = root.AddRoute("/docs/*different", h)
	if !errors.Is(err, ErrParamConflict) {
		t.Errorf("expected ErrParamConflict, got %v", err)
	}
}
