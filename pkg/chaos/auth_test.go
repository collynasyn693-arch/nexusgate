package chaos

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityGuard_Disabled(t *testing.T) {
	sg, err := NewSecurityGuard(Config{
		Enabled:  false,
		AdminKey: "supersecret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderChaosKey, "supersecret")

	if sg.Authorize(req) {
		t.Errorf("expected disabled security guard to reject authorization")
	}
}

func TestSecurityGuard_EmptyKeyVulnerabilityProtection(t *testing.T) {
	// If AdminKey is empty, empty header key must NOT authenticate
	sg, err := NewSecurityGuard(Config{
		Enabled:  true,
		AdminKey: "", // empty admin key
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inbound request with no header
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	if sg.Authorize(req1) {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: Empty AdminKey authenticated empty header!")
	}

	// Inbound request with empty header
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set(HeaderChaosKey, "")
	if sg.Authorize(req2) {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: Empty AdminKey authenticated empty header value!")
	}
}

func TestSecurityGuard_KeyAuthentication(t *testing.T) {
	sg, err := NewSecurityGuard(Config{
		Enabled:  true,
		AdminKey: "correct-token-12345",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wrong key
	reqWrong := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqWrong.Header.Set(HeaderChaosKey, "wrong-token-abc")
	if sg.Authorize(reqWrong) {
		t.Errorf("expected wrong key to be rejected")
	}

	// Correct key
	reqCorrect := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqCorrect.Header.Set(HeaderChaosKey, "correct-token-12345")
	if !sg.Authorize(reqCorrect) {
		t.Errorf("expected correct key to be authorized")
	}
}

func TestSecurityGuard_SubnetWhitelisting(t *testing.T) {
	sg, err := NewSecurityGuard(Config{
		Enabled: true,
		AllowedSubnets: []string{
			"127.0.0.1/32",
			"::1/128",
			"192.168.1.0/24",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Allowed IPv4
	reqLocal := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqLocal.RemoteAddr = "127.0.0.1:45678"
	if !sg.Authorize(reqLocal) {
		t.Errorf("expected 127.0.0.1 to be authorized")
	}

	// Allowed IPv6
	reqV6 := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqV6.RemoteAddr = "[::1]:54321"
	if !sg.Authorize(reqV6) {
		t.Errorf("expected ::1 to be authorized")
	}

	// Allowed subnet IP
	reqSubnet := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqSubnet.RemoteAddr = "192.168.1.42:1234"
	if !sg.Authorize(reqSubnet) {
		t.Errorf("expected 192.168.1.42 to be authorized")
	}

	// Blocked external IP
	reqExternal := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqExternal.RemoteAddr = "203.0.113.195:8080"
	if sg.Authorize(reqExternal) {
		t.Errorf("expected 203.0.113.195 to be blocked")
	}
}

func TestSecurityGuard_DefenseInDepth(t *testing.T) {
	// Both key AND subnet required
	sg, err := NewSecurityGuard(Config{
		Enabled:        true,
		AdminKey:       "vault-key-999",
		AllowedSubnets: []string{"127.0.0.1/32"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Valid key, invalid IP -> REJECT
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.Header.Set(HeaderChaosKey, "vault-key-999")
	req1.RemoteAddr = "10.0.0.1:9090"
	if sg.Authorize(req1) {
		t.Errorf("expected rejection when IP is not in subnet")
	}

	// Invalid key, valid IP -> REJECT
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set(HeaderChaosKey, "wrong-key")
	req2.RemoteAddr = "127.0.0.1:9090"
	if sg.Authorize(req2) {
		t.Errorf("expected rejection when key is invalid")
	}

	// Valid key, valid IP -> ACCEPT
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.Header.Set(HeaderChaosKey, "vault-key-999")
	req3.RemoteAddr = "127.0.0.1:9090"
	if !sg.Authorize(req3) {
		t.Errorf("expected authorization when both key and IP are valid")
	}
}

func TestSecurityGuard_InvalidCIDR(t *testing.T) {
	_, err := NewSecurityGuard(Config{
		Enabled:        true,
		AllowedSubnets: []string{"not-a-valid-cidr"},
	})
	if err == nil {
		t.Errorf("expected error for invalid CIDR")
	}
}
