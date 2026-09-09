package config

import (
	"testing"
)

func TestComputeDiff(t *testing.T) {
	cfg1 := NewDefaultConfig()
	cfg1.Routes = []RouteConfig{
		{ID: "r1", Path: "/v1", UpstreamID: "u1"},
		{ID: "r2", Path: "/v2", UpstreamID: "u1"},
	}
	cfg1.Upstreams = []UpstreamConfig{
		{
			ID:        "u1",
			Algorithm: "swwr",
			Targets:   []TargetConfig{{URL: "http://10.0.0.1:8080", Weight: 1}},
		},
	}

	// 1. Identical configs should have no changes
	diffNoChange := ComputeDiff(cfg1, cfg1.Clone())
	if diffNoChange.HasChanges() {
		t.Errorf("expected no changes between identical configs, got: %s", diffNoChange.String())
	}

	// 2. Modify route, remove route, add route
	cfg2 := cfg1.Clone()
	cfg2.Routes = []RouteConfig{
		{ID: "r1", Path: "/v1/new", UpstreamID: "u1"}, // modified
		{ID: "r3", Path: "/v3", UpstreamID: "u1"},     // added
		// r2 removed
	}

	diffRoutes := ComputeDiff(cfg1, cfg2)
	if len(diffRoutes.AddedRoutes) != 1 || diffRoutes.AddedRoutes[0] != "r3" {
		t.Errorf("expected added route r3, got: %v", diffRoutes.AddedRoutes)
	}
	if len(diffRoutes.RemovedRoutes) != 1 || diffRoutes.RemovedRoutes[0] != "r2" {
		t.Errorf("expected removed route r2, got: %v", diffRoutes.RemovedRoutes)
	}
	if len(diffRoutes.ModifiedRoutes) != 1 || diffRoutes.ModifiedRoutes[0] != "r1" {
		t.Errorf("expected modified route r1, got: %v", diffRoutes.ModifiedRoutes)
	}

	// 3. Port change detection
	cfg3 := cfg1.Clone()
	cfg3.Listener.Port = 9090
	diffPort := ComputeDiff(cfg1, cfg3)
	if !diffPort.PortChanged {
		t.Errorf("expected PortChanged == true when listener port changes")
	}

	// 4. Upstream modifications
	cfg4 := cfg1.Clone()
	cfg4.Upstreams[0].Targets[0].Weight = 10
	diffUpstreams := ComputeDiff(cfg1, cfg4)
	if len(diffUpstreams.ModifiedUpstreams) != 1 || diffUpstreams.ModifiedUpstreams[0] != "u1" {
		t.Errorf("expected modified upstream u1, got: %v", diffUpstreams.ModifiedUpstreams)
	}
}

func TestDryRun(t *testing.T) {
	current := NewDefaultConfig()
	current.Listener.Port = 8080

	// 1. Valid dry-run candidate
	validYAML := `
version: "1.0"
listener:
  port: 8443
upstreams:
  - id: "u-new"
    algorithm: "round_robin"
    targets:
      - url: "http://10.0.0.2:9000"
        weight: 2
routes:
  - id: "r-new"
    path: "/api"
    upstream_id: "u-new"
`
	candidate, diff, err := DryRun([]byte(validYAML), current)
	if err != nil {
		t.Fatalf("DryRun failed on valid YAML: %v", err)
	}
	if candidate.Listener.Port != 8443 {
		t.Errorf("expected candidate port 8443, got %d", candidate.Listener.Port)
	}
	if !diff.PortChanged {
		t.Errorf("expected PortChanged == true in diff")
	}
	if len(diff.AddedRoutes) != 1 || diff.AddedRoutes[0] != "r-new" {
		t.Errorf("expected added route r-new, got %v", diff.AddedRoutes)
	}

	// 2. Syntax error dry-run
	badYAML := `
version: "1.0"
listener:
	tab_error: true
`
	_, _, err = DryRun([]byte(badYAML), current)
	if err == nil {
		t.Fatalf("expected error on dry run with tab syntax error")
	}

	// 3. Semantic validation failure (privileged port for non-root)
	invalidConfigYAML := `
version: "1.0"
listener:
  port: 80
`
	_, _, err = DryRun([]byte(invalidConfigYAML), current)
	if err == nil {
		t.Fatalf("expected error on dry run with privileged port")
	}

	// Ensure current config remains untouched
	if current.Listener.Port != 8080 {
		t.Errorf("expected current config to remain port 8080, got %d", current.Listener.Port)
	}
}
