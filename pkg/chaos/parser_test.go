package chaos

import (
	"net/url"
	"testing"
	"time"
)

func TestParseQueryParams_EmptyAndNormal(t *testing.T) {
	// Empty query
	p, ok := ParseQueryParams("", 0, 0)
	if ok || p.HasChaos {
		t.Errorf("expected false for empty query, got ok=%v, hasChaos=%v", ok, p.HasChaos)
	}

	// Normal query without "__"
	p, ok = ParseQueryParams("userId=42&sort=desc&filter=active", 0, 0)
	if ok || p.HasChaos {
		t.Errorf("expected false for normal query, got ok=%v, hasChaos=%v", ok, p.HasChaos)
	}
}

func TestParseQueryParams_Delay(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		maxDelay  time.Duration
		expected  time.Duration
		expectOk  bool
	}{
		{
			name:     "valid ms delay",
			query:    "__delay=200ms",
			maxDelay: 10 * time.Second,
			expected: 200 * time.Millisecond,
			expectOk: true,
		},
		{
			name:     "valid sec delay",
			query:    "foo=bar&__delay=2s",
			maxDelay: 10 * time.Second,
			expected: 2 * time.Second,
			expectOk: true,
		},
		{
			name:     "delay clamped to maxDelay",
			query:    "__delay=100s",
			maxDelay: 10 * time.Second,
			expected: 10 * time.Second,
			expectOk: true,
		},
		{
			name:     "negative delay clamped to zero",
			query:    "__delay=-500ms",
			maxDelay: 10 * time.Second,
			expected: 0,
			expectOk: true,
		},
		{
			name:     "malformed delay ignored",
			query:    "__delay=invalid_duration",
			maxDelay: 10 * time.Second,
			expected: 0,
			expectOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := ParseQueryParams(tc.query, tc.maxDelay, 0)
			if ok != tc.expectOk {
				t.Fatalf("expected ok=%v, got %v", tc.expectOk, ok)
			}
			if p.Delay != tc.expected {
				t.Errorf("expected delay %v, got %v", tc.expected, p.Delay)
			}
		})
	}
}

func TestParseQueryParams_Status(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected int
		expectOk bool
	}{
		{
			name:     "valid 503",
			query:    "__status=503",
			expected: 503,
			expectOk: true,
		},
		{
			name:     "valid 429",
			query:    "__status=429&page=1",
			expected: 429,
			expectOk: true,
		},
		{
			name:     "out of bounds high",
			query:    "__status=600",
			expected: 0,
			expectOk: false,
		},
		{
			name:     "out of bounds low",
			query:    "__status=99",
			expected: 0,
			expectOk: false,
		},
		{
			name:     "invalid non-integer",
			query:    "__status=not_a_number",
			expected: 0,
			expectOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := ParseQueryParams(tc.query, 0, 0)
			if ok != tc.expectOk {
				t.Fatalf("expected ok=%v, got %v", tc.expectOk, ok)
			}
			if p.Status != tc.expected {
				t.Errorf("expected status %d, got %d", tc.expected, p.Status)
			}
		})
	}
}

func TestParseQueryParams_Body(t *testing.T) {
	// Standard body
	p, ok := ParseQueryParams("__body=custom_error_payload", 0, 0)
	if !ok || p.Body != "custom_error_payload" {
		t.Fatalf("expected body custom_error_payload, got %q", p.Body)
	}

	// Body length clamp
	p, ok = ParseQueryParams("__body=toolongpayloadhere", 0, 10)
	if !ok || p.Body != "toolongpay" {
		t.Fatalf("expected clamped body 'toolongpay', got %q", p.Body)
	}
}

func TestParseQueryParams_Drop(t *testing.T) {
	for _, val := range []string{"true", "1", "yes", ""} {
		p, ok := ParseQueryParams("__drop="+val, 0, 0)
		if !ok || !p.Drop {
			t.Errorf("expected drop=true for val=%q", val)
		}
	}

	p, ok := ParseQueryParams("__drop=false", 0, 0)
	if ok || p.Drop {
		t.Errorf("expected drop=false for __drop=false")
	}
}

func TestParseQueryParams_FaultRate(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected float64
		expectOk bool
	}{
		{
			name:     "decimal 0.25",
			query:    "__fault_rate=0.25",
			expected: 0.25,
			expectOk: true,
		},
		{
			name:     "percentage 50%",
			query:    "__fault_rate=50",
			expected: 0.50,
			expectOk: true,
		},
		{
			name:     "clamped above 1.0",
			query:    "__fault_rate=150",
			expected: 1.0,
			expectOk: true,
		},
		{
			name:     "clamped negative",
			query:    "__fault_rate=-0.5",
			expected: 0.0,
			expectOk: true,
		},
		{
			name:     "invalid string",
			query:    "__fault_rate=bad",
			expected: 0.0,
			expectOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := ParseQueryParams(tc.query, 0, 0)
			if ok != tc.expectOk {
				t.Fatalf("expected ok=%v, got %v", tc.expectOk, ok)
			}
			if p.FaultRate != tc.expected {
				t.Errorf("expected fault rate %v, got %v", tc.expected, p.FaultRate)
			}
		})
	}
}

func TestStripChaosParams(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"id=100&sort=asc", "id=100&sort=asc"},
		{"__delay=100ms", ""},
		{"id=100&__delay=100ms", "id=100"},
		{"__delay=100ms&id=100", "id=100"},
		{"foo=1&__status=500&bar=2&__drop=true&baz=3", "foo=1&bar=2&baz=3"},
	}

	for _, tc := range tests {
		got := StripChaosParams(tc.input)
		if got != tc.expected {
			t.Errorf("StripChaosParams(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestSanitizeRequestURL(t *testing.T) {
	u, _ := url.Parse("http://example.com/api/v1/users?id=99&__delay=50ms&active=true")
	sanitized := SanitizeRequestURL(u)

	if sanitized.RawQuery != "id=99&active=true" {
		t.Errorf("expected sanitized RawQuery 'id=99&active=true', got %q", sanitized.RawQuery)
	}

	// Verify original URL was not mutated
	if u.RawQuery != "id=99&__delay=50ms&active=true" {
		t.Errorf("original URL was mutated: %q", u.RawQuery)
	}
}
