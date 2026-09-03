package config

import (
	"fmt"
	"net"
	"nexusgate/internal/platform"
	"strings"
)

const (
	// MinUnprivilegedPort defines the lowest port number allowed for non-root users in Termux / Linux.
	MinUnprivilegedPort = 1024
	// MaxPort defines the highest valid TCP port number.
	MaxPort = 65535
)

// ValidationError indicates a semantic or boundary violation in configuration.
type ValidationError struct {
	Field   string `json:"field"`
	Value   any    `json:"value"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on %s: %s (got %v)", e.Field, e.Message, e.Value)
}

// ValidatePort validates that the port is within bounds and enforces unprivileged constraints for non-root users.
func ValidatePort(port int) error {
	if port <= 0 || port > MaxPort {
		return &ValidationError{
			Field:   "listener.port",
			Value:   port,
			Message: fmt.Sprintf("port must be between 1 and %d", MaxPort),
		}
	}

	// Enforce unprivileged port floor for non-root processes
	if !platform.IsRoot() && port < MinUnprivilegedPort {
		return &ValidationError{
			Field:   "listener.port",
			Value:   port,
			Message: fmt.Sprintf("privileged port %d cannot be bound in non-root Termux userland (minimum allowed is %d)", port, MinUnprivilegedPort),
		}
	}

	return nil
}

// ValidateHost validates that the host string is a valid IP or hostname.
func ValidateHost(host string) error {
	if host == "" || host == "0.0.0.0" || host == "::" || host == "127.0.0.1" || host == "localhost" {
		return nil
	}

	// Check if valid IP address
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}

	// Hostname validation
	if len(host) > 253 {
		return &ValidationError{
			Field:   "listener.host",
			Value:   host,
			Message: "hostname cannot exceed 253 characters",
		}
	}

	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 {
			return &ValidationError{
				Field:   "listener.host",
				Value:   host,
				Message: fmt.Sprintf("invalid host component %q", part),
			}
		}
	}

	return nil
}

// ValidateListener ensures listener host, port, and timeouts satisfy constraints.
func ValidateListener(l *ListenerConfig) error {
	if err := ValidatePort(l.Port); err != nil {
		return err
	}
	if err := ValidateHost(l.Host); err != nil {
		return err
	}

	if l.ReadTimeout < 0 {
		return &ValidationError{Field: "listener.read_timeout", Value: l.ReadTimeout, Message: "timeout cannot be negative"}
	}
	if l.WriteTimeout < 0 {
		return &ValidationError{Field: "listener.write_timeout", Value: l.WriteTimeout, Message: "timeout cannot be negative"}
	}
	if l.IdleTimeout < 0 {
		return &ValidationError{Field: "listener.idle_timeout", Value: l.IdleTimeout, Message: "timeout cannot be negative"}
	}
	if l.ReadHeaderTimeout < 0 {
		return &ValidationError{Field: "listener.read_header_timeout", Value: l.ReadHeaderTimeout, Message: "timeout cannot be negative"}
	}

	return nil
}

// Validate performs initial validation on the GatewayConfig.
func Validate(cfg *GatewayConfig) error {
	if cfg == nil {
		return &ValidationError{Field: "config", Value: nil, Message: "configuration cannot be nil"}
	}
	return ValidateListener(&cfg.Listener)
}
