package platform

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	uidMu   sync.RWMutex
	mockUID *int
)

// SetMockUID allows overriding the UID for testing non-root / root boundary logic.
// The returned cleanup function restores the previous mock state.
func SetMockUID(uid int) func() {
	uidMu.Lock()
	prev := mockUID
	val := uid
	mockUID = &val
	uidMu.Unlock()

	return func() {
		uidMu.Lock()
		mockUID = prev
		uidMu.Unlock()
	}
}

// ResetMockUID clears any configured mock UID.
func ResetMockUID() {
	uidMu.Lock()
	mockUID = nil
	uidMu.Unlock()
}

// GetUID returns the effective user ID of the current process, honoring any test mocks.
func GetUID() int {
	uidMu.RLock()
	defer uidMu.RUnlock()
	if mockUID != nil {
		return *mockUID
	}
	return os.Getuid()
}

// IsRoot returns true if the current process is running as root (UID 0).
func IsRoot() bool {
	return GetUID() == 0
}

// IsTermux detects if the gateway is running within an Android Termux userland environment.
func IsTermux() bool {
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	prefix := os.Getenv("PREFIX")
	if strings.Contains(prefix, "com.termux") {
		return true
	}
	if _, err := os.Stat("/data/data/com.termux/files"); err == nil {
		return true
	}
	return false
}

// GetAndroidAPILevel queries the Android SDK API level via getprop, returning 0 if not on Android.
func GetAndroidAPILevel() int {
	if val := os.Getenv("ANDROID_API_LEVEL"); val != "" {
		if lvl, err := strconv.Atoi(val); err == nil {
			return lvl
		}
	}
	out, err := exec.Command("getprop", "ro.build.version.sdk").Output()
	if err == nil {
		lvlStr := strings.TrimSpace(string(out))
		if lvl, err := strconv.Atoi(lvlStr); err == nil {
			return lvl
		}
	}
	return 0
}

// DefaultConfigDir returns the base directory for NexusGate configuration files.
func DefaultConfigDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".nexusgate")
}

// DefaultConfigPath returns the canonical path to the default configuration YAML.
func DefaultConfigPath() string {
	if env := os.Getenv("NEXUSGATE_CONFIG"); env != "" {
		return env
	}
	return filepath.Join(DefaultConfigDir(), "nexusgate.yaml")
}

// DefaultSocketPath resolves the canonical Unix Domain Socket path for IPC telemetry.
// Under Termux, it favors $PREFIX/var/run/nexusgate.sock or $HOME/.nexusgate/nexusgate.sock.
// On standard Linux, it falls back to /tmp/nexusgate.sock or $HOME/.nexusgate/nexusgate.sock.
func DefaultSocketPath() string {
	if env := os.Getenv("NEXUSGATE_UDS_PATH"); env != "" {
		return env
	}

	if IsTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix != "" {
			runDir := filepath.Join(prefix, "var", "run")
			if err := os.MkdirAll(runDir, 0700); err == nil {
				return filepath.Join(runDir, "nexusgate.sock")
			}
		}
		// Fallback to $HOME/.nexusgate/nexusgate.sock
		cfgDir := DefaultConfigDir()
		_ = os.MkdirAll(cfgDir, 0700)
		return filepath.Join(cfgDir, "nexusgate.sock")
	}

	// Non-Termux / Standard Linux environment
	tmpDir := os.TempDir()
	if tmpDir != "" {
		return filepath.Join(tmpDir, "nexusgate.sock")
	}
	return filepath.Join(DefaultConfigDir(), "nexusgate.sock")
}

// EnsureSecureDirectory creates a directory if it does not exist, enforcing strict 0700 permissions.
func EnsureSecureDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("path %q exists but is not a directory", dir)
		}
		return os.Chmod(dir, 0700)
	}
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0700)
	}
	return err
}

// EnforceSocketPermissions applies 0600 POSIX permissions to the socket file.
func EnforceSocketPermissions(socketPath string) error {
	return os.Chmod(socketPath, 0600)
}

// CleanupStaleSocket checks if a socket file exists, verifies if an active listener is attached,
// and unlinks the socket if it is stale (i.e. connection refused or timed out).
func CleanupStaleSocket(socketPath string) error {
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return nil
	}

	// Attempt a test dial to see if a live gateway process is running
	conn, err := net.DialTimeout("unix", socketPath, 50*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("active gateway instance already listening on socket %q", socketPath)
	}

	// Socket exists but no active process is responding; safely unlink
	return os.Remove(socketPath)
}
