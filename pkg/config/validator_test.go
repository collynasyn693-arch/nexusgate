package config

import (
	"nexusgate/internal/platform"
	"strings"
	"testing"
	"time"
)

func TestUnprivilegedPortValidation(t *testing.T) {
	// Simulate non-root user
	cleanupNonRoot := platform.SetMockUID(10142)
	defer cleanupNonRoot()

	// Privileged ports should be rejected
	for _, port := range []int{80, 443, 800, 1023} {
		err := ValidatePort(port)
		if err == nil {
			t.Errorf("expected port %d to be rejected for non-root user", port)
		} else if !strings.Contains(err.Error(), "privileged port") {
			t.Errorf("expected privileged port error message for port %d, got %v", port, err)
		}
	}

	// Unprivileged ports should be accepted
	for _, port := range []int{1024, 1025, 8080, 8443, 65535} {
		if err := ValidatePort(port); err != nil {
			t.Errorf("expected port %d to be allowed for non-root user, got %v", port, err)
		}
	}

	// Out of bounds ports should be rejected
	for _, port := range []int{-1, 0, 65536, 70000} {
		if err := ValidatePort(port); err == nil {
			t.Errorf("expected out-of-bounds port %d to be rejected", port)
		}
	}
}

func TestRootPortValidation(t *testing.T) {
	// Simulate root user (UID 0)
	cleanupRoot := platform.SetMockUID(0)
	defer cleanupRoot()

	// Privileged ports are allowed for root
	for _, port := range []int{80, 443, 1023, 8080} {
		if err := ValidatePort(port); err != nil {
			t.Errorf("expected port %d to be allowed for root user, got %v", port, err)
		}
	}

	// Port 0 and negative should still be rejected
	if err := ValidatePort(0); err == nil {
		t.Errorf("expected port 0 to be rejected for root")
	}
}

func TestHostValidation(t *testing.T) {
	validHosts := []string{
		"",
		"0.0.0.0",
		"127.0.0.1",
		"::",
		"::1",
		"localhost",
		"edge.local",
		"my-gateway.internal",
	}

	for _, host := range validHosts {
		if err := ValidateHost(host); err != nil {
			t.Errorf("expected host %q to be valid, got %v", host, err)
		}
	}

	invalidHosts := []string{
		strings.Repeat("a", 254), // > 253 characters
		"invalid..host",
	}

	for _, host := range invalidHosts {
		if err := ValidateHost(host); err == nil {
			t.Errorf("expected invalid host %q to be rejected", host)
		}
	}
}

func TestListenerValidation(t *testing.T) {
	cleanupNonRoot := platform.SetMockUID(10142)
	defer cleanupNonRoot()

	validListener := &ListenerConfig{
		Host:              "0.0.0.0",
		Port:              8080,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}
	if err := ValidateListener(validListener); err != nil {
		t.Fatalf("expected valid listener to pass, got: %v", err)
	}

	// Negative timeouts
	badListener := *validListener
	badListener.ReadTimeout = -1 * time.Second
	if err := ValidateListener(&badListener); err == nil {
		t.Fatalf("expected error for negative read timeout")
	}
}
