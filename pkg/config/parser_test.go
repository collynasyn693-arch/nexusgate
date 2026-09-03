package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseValidYAML(t *testing.T) {
	yamlData := `
version: "1.0"
listener:
  host: "0.0.0.0"
  port: 8080
  read_timeout: "5s"
  write_timeout: "10s"
  idle_timeout: "120s"
  read_header_timeout: "2s"
  max_header_bytes: 1048576
  max_body_bytes: 10485760

routes:
  - id: "api-route"
    path: "/api/v1"
    methods: [GET, POST]
    upstream_id: "backend-pool"
    strip_prefix: "/api"
    timeout: "3s"
    headers:
      X-Gateway: "NexusGate"

upstreams:
  - id: "backend-pool"
    algorithm: "swwr"
    targets:
      - url: "http://127.0.0.1:9001"
        weight: 3
        max_conns: 100
      - url: "http://127.0.0.1:9002"
        weight: 1
    health_check:
      enabled: true
      path: "/healthz"
      interval: "10s"
      timeout: "2s"
      healthy_threshold: 2
      unhealthy_threshold: 3

resilience:
  circuit_breaker:
    enabled: true
    consecutive_failures: 5
    failure_rate_threshold: 0.5
    min_requests: 10
    reset_timeout: "15s"
    half_open_max_requests: 3

telemetry:
  uds_path: "/data/data/com.termux/files/usr/var/run/nexusgate.sock"
  ring_buffer_size: 65536
  idle_sleep_timeout: "3s"
  snapshot_interval: "1s"

chaos:
  enabled: true
  header_key: "X-NexusGate-Chaos"
  allowed_subnets:
    - "127.0.0.1/32"
`
	cfg, err := Parse([]byte(yamlData))
	if err != nil {
		t.Fatalf("Parse(valid YAML) failed: %v", err)
	}

	if cfg.Version != "1.0" {
		t.Errorf("expected version 1.0, got %q", cfg.Version)
	}
	if cfg.Listener.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Listener.Port)
	}
	if cfg.Listener.ReadTimeout != 5*time.Second {
		t.Errorf("expected read_timeout 5s, got %v", cfg.Listener.ReadTimeout)
	}
	if cfg.Listener.ReadHeaderTimeout != 2*time.Second {
		t.Errorf("expected read_header_timeout 2s, got %v", cfg.Listener.ReadHeaderTimeout)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(cfg.Routes))
	}
	if cfg.Routes[0].ID != "api-route" {
		t.Errorf("expected route ID api-route, got %q", cfg.Routes[0].ID)
	}
	if len(cfg.Routes[0].Methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(cfg.Routes[0].Methods))
	}
	if cfg.Routes[0].Timeout != 3*time.Second {
		t.Errorf("expected route timeout 3s, got %v", cfg.Routes[0].Timeout)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
	}
	if len(cfg.Upstreams[0].Targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(cfg.Upstreams[0].Targets))
	}
	if cfg.Resilience.CircuitBreaker.ResetTimeout != 15*time.Second {
		t.Errorf("expected CB reset_timeout 15s, got %v", cfg.Resilience.CircuitBreaker.ResetTimeout)
	}
}

func TestParseValidJSON(t *testing.T) {
	jsonData := `{
  "version": "1.0",
  "listener": {
    "host": "127.0.0.1",
    "port": 9090,
    "read_timeout": "2s",
    "write_timeout": "4s",
    "idle_timeout": "60s"
  },
  "routes": [
    {
      "id": "json-route",
      "path": "/json",
      "upstream_id": "json-upstream"
    }
  ],
  "upstreams": [
    {
      "id": "json-upstream",
      "algorithm": "round_robin",
      "targets": [
        {
          "url": "http://127.0.0.1:8001",
          "weight": 1
        }
      ]
    }
  ]
}`

	cfg, err := Parse([]byte(jsonData))
	if err != nil {
		t.Fatalf("Parse(valid JSON) failed: %v", err)
	}
	if cfg.Listener.Port != 9090 {
		t.Errorf("expected listener port 9090, got %d", cfg.Listener.Port)
	}
	if cfg.Listener.ReadTimeout != 2*time.Second {
		t.Errorf("expected read timeout 2s, got %v", cfg.Listener.ReadTimeout)
	}
}

func TestParseEmptyAndCommentOnly(t *testing.T) {
	_, err := Parse([]byte(""))
	if err == nil {
		t.Fatalf("expected error for empty data")
	}

	_, err = Parse([]byte("   \n\n\t   \n"))
	if err == nil {
		t.Fatalf("expected error for whitespace only")
	}

	_, err = Parse([]byte("# Just a comment\n# Another comment\n"))
	if err == nil {
		t.Fatalf("expected error for comments only")
	}
}

func TestParseTabIndentationRejection(t *testing.T) {
	tabYAML := "version: \"1.0\"\nlistener:\n\tport: 8080\n"
	_, err := Parse([]byte(tabYAML))
	if err == nil {
		t.Fatalf("expected error when tab character is used for indentation")
	}
	if !strings.Contains(err.Error(), "tabs are not allowed") {
		t.Errorf("expected tabs error message, got: %v", err)
	}
}

func TestParseInvalidDuration(t *testing.T) {
	badDurationYAML := `
version: "1.0"
listener:
  port: 8080
  read_timeout: "5xyz"
`
	_, err := Parse([]byte(badDurationYAML))
	if err == nil {
		t.Fatalf("expected error for invalid duration format")
	}
}

func TestParseUnknownFieldsRejection(t *testing.T) {
	unknownFieldYAML := `
version: "1.0"
listener:
  port: 8080
  unknown_unsupported_key: "danger"
`
	_, err := Parse([]byte(unknownFieldYAML))
	if err == nil {
		t.Fatalf("expected error for unknown config field")
	}
}

func TestParseFile(t *testing.T) {
	tempDir := t.TempDir()
	validFile := filepath.Join(tempDir, "config.yaml")
	content := "version: \"1.0\"\nlistener:\n  port: 8080\n"
	if err := os.WriteFile(validFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := ParseFile(validFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if cfg.Listener.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Listener.Port)
	}

	// Non-existent file
	_, err = ParseFile(filepath.Join(tempDir, "does_not_exist.yaml"))
	if err == nil {
		t.Fatalf("expected error reading non-existent file")
	}
}
