package platform

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestMockUID(t *testing.T) {
	cleanup := SetMockUID(0)
	if !IsRoot() {
		t.Errorf("expected IsRoot() == true when mock UID is 0")
	}
	if uid := GetUID(); uid != 0 {
		t.Errorf("expected GetUID() == 0, got %d", uid)
	}
	cleanup()

	cleanupNonRoot := SetMockUID(10142)
	if IsRoot() {
		t.Errorf("expected IsRoot() == false when mock UID is 10142")
	}
	if uid := GetUID(); uid != 10142 {
		t.Errorf("expected GetUID() == 10142, got %d", uid)
	}
	cleanupNonRoot()

	ResetMockUID()
}

func TestIsTermux(t *testing.T) {
	origTermux := os.Getenv("TERMUX_VERSION")
	defer os.Setenv("TERMUX_VERSION", origTermux)

	os.Setenv("TERMUX_VERSION", "0.118.1")
	if !IsTermux() {
		t.Errorf("expected IsTermux() == true when TERMUX_VERSION is set")
	}

	os.Unsetenv("TERMUX_VERSION")
	origPrefix := os.Getenv("PREFIX")
	defer os.Setenv("PREFIX", origPrefix)

	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	if !IsTermux() {
		t.Errorf("expected IsTermux() == true when PREFIX contains com.termux")
	}
}

func TestGetAndroidAPILevel(t *testing.T) {
	orig := os.Getenv("ANDROID_API_LEVEL")
	defer os.Setenv("ANDROID_API_LEVEL", orig)

	os.Setenv("ANDROID_API_LEVEL", "34")
	if lvl := GetAndroidAPILevel(); lvl != 34 {
		t.Errorf("expected API level 34, got %d", lvl)
	}

	os.Setenv("ANDROID_API_LEVEL", "invalid")
	_ = GetAndroidAPILevel() // should not panic
}

func TestConfigPaths(t *testing.T) {
	tempHome := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tempHome)

	expectedDir := filepath.Join(tempHome, ".nexusgate")
	if dir := DefaultConfigDir(); dir != expectedDir {
		t.Errorf("expected DefaultConfigDir() == %q, got %q", expectedDir, dir)
	}

	expectedCfg := filepath.Join(expectedDir, "nexusgate.yaml")
	if cfg := DefaultConfigPath(); cfg != expectedCfg {
		t.Errorf("expected DefaultConfigPath() == %q, got %q", expectedCfg, cfg)
	}

	// Test environment override
	customCfg := "/custom/path/gateway.yaml"
	os.Setenv("NEXUSGATE_CONFIG", customCfg)
	defer os.Unsetenv("NEXUSGATE_CONFIG")
	if cfg := DefaultConfigPath(); cfg != customCfg {
		t.Errorf("expected DefaultConfigPath() == %q, got %q", customCfg, cfg)
	}
}

func TestSocketPaths(t *testing.T) {
	customSock := "/var/run/custom.sock"
	os.Setenv("NEXUSGATE_UDS_PATH", customSock)
	defer os.Unsetenv("NEXUSGATE_UDS_PATH")

	if sock := DefaultSocketPath(); sock != customSock {
		t.Errorf("expected DefaultSocketPath() == %q, got %q", customSock, sock)
	}
}

func TestEnsureSecureDirectoryAndSocketPermissions(t *testing.T) {
	tempDir := t.TempDir()
	testDir := filepath.Join(tempDir, "secure_dir")

	if err := EnsureSecureDirectory(testDir); err != nil {
		t.Fatalf("EnsureSecureDirectory failed: %v", err)
	}

	info, err := os.Stat(testDir)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("expected directory perm 0700, got %04o", perm)
	}

	// Test socket permissions
	sockFile := filepath.Join(testDir, "test.sock")
	if err := os.WriteFile(sockFile, []byte{}, 0666); err != nil {
		t.Fatalf("failed to create dummy socket file: %v", err)
	}

	if err := EnforceSocketPermissions(sockFile); err != nil {
		t.Fatalf("EnforceSocketPermissions failed: %v", err)
	}

	info, err = os.Stat(sockFile)
	if err != nil {
		t.Fatalf("stat on socket file failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected socket perm 0600, got %04o", perm)
	}
}

func TestCleanupStaleSocket(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "stale.sock")

	// 1. Non-existent socket should succeed (noop)
	if err := CleanupStaleSocket(sockPath); err != nil {
		t.Fatalf("expected non-existent socket cleanup to succeed, got %v", err)
	}

	// 2. Stale socket (file exists but no listener)
	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("failed to write dummy socket file: %v", err)
	}
	if err := CleanupStaleSocket(sockPath); err != nil {
		t.Fatalf("expected stale socket to be cleaned up, got %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale socket file to be deleted")
	}

	// 3. Active socket (live listener)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on socket: %v", err)
	}
	defer ln.Close()

	err = CleanupStaleSocket(sockPath)
	if err == nil {
		t.Fatalf("expected error when cleaning up active socket, got nil")
	}
}
