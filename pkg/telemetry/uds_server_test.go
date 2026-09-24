package telemetry

import (
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func createTestSocketPath(t *testing.T, name string) string {
	t.Helper()
	// Use os.TempDir directly to keep path short (<108 bytes on Linux/Android)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ng_%d_%s.sock", rand.Uint32(), name))
	if len(path) >= LinuxSunPathMax {
		t.Fatalf("test socket path too long (%d): %s", len(path), path)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}

func TestUDSServer_LifecycleAndPermissions(t *testing.T) {
	sockPath := createTestSocketPath(t, "lifecycle")

	var (
		connected    = make(chan struct{})
		disconnected = make(chan struct{})
	)

	server, err := NewUDSServer(sockPath, func(c net.Conn) {
		close(connected)
	}, func(c net.Conn) {
		close(disconnected)
	})
	if err != nil {
		t.Fatalf("failed to create UDSServer: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start UDSServer: %v", err)
	}
	defer func() { _ = server.Stop() }()

	// Verify socket file existence and 0600 permissions
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("failed to stat socket file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Connect client
	clientConn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to connect client to UDS: %v", err)
	}

	select {
	case <-connected:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for onConnect callback")
	}

	if count := server.ClientCount(); count != 1 {
		t.Errorf("expected 1 active client, got %d", count)
	}

	// Close client connection to trigger server disconnect detection
	_ = clientConn.Close()

	select {
	case <-disconnected:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for onDisconnect callback")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("error stopping server: %v", err)
	}

	// Verify socket file was unlinked
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("expected socket file to be unlinked after stop, err=%v", err)
	}
}

func TestUDSServer_StaleSocketCleanup(t *testing.T) {
	sockPath := createTestSocketPath(t, "stale")

	// Create a dummy stale file simulating dead socket
	if err := os.WriteFile(sockPath, []byte("stale dead socket"), 0600); err != nil {
		t.Fatalf("failed to create dummy stale socket file: %v", err)
	}

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatalf("expected stale socket file to exist")
	}

	server, err := NewUDSServer(sockPath, nil, nil)
	if err != nil {
		t.Fatalf("NewUDSServer failed: %v", err)
	}

	// Start should safely detect dead socket, unlink it, and start cleanly
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server over stale socket: %v", err)
	}
	defer func() { _ = server.Stop() }()

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatalf("expected live socket file to exist")
	}
}

func TestUDSServer_ActiveInstanceConflict(t *testing.T) {
	sockPath := createTestSocketPath(t, "conflict")

	server1, err := NewUDSServer(sockPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to create server1: %v", err)
	}
	if err := server1.Start(); err != nil {
		t.Fatalf("failed to start server1: %v", err)
	}
	defer func() { _ = server1.Stop() }()

	// Second instance attempting to bind to the same active socket
	server2, err := NewUDSServer(sockPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to create server2: %v", err)
	}

	err = server2.Start()
	if err == nil {
		_ = server2.Stop()
		t.Fatalf("expected server2 to fail binding to active socket")
	}

	if !strings.Contains(err.Error(), "already listening") {
		t.Errorf("expected conflict error message, got: %v", err)
	}

	// Verify server1 is still intact and accepting connections
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("expected server1 to remain active: %v", err)
	}
	_ = conn.Close()
}

func TestUDSServer_PathTooLong(t *testing.T) {
	longPath := "/data/data/com.termux/files/home/" + strings.Repeat("dir/", 25) + "sock.sock"
	_, err := NewUDSServer(longPath, nil, nil)
	if err == nil {
		t.Fatalf("expected error for socket path exceeding 108 bytes")
	}
}

func TestUDSServer_MultipleConcurrentClients(t *testing.T) {
	sockPath := createTestSocketPath(t, "multi")

	server, err := NewUDSServer(sockPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() { _ = server.Stop() }()

	const numClients = 10
	var (
		wg      sync.WaitGroup
		conns   = make([]net.Conn, numClients)
		connErr = make([]error, numClients)
	)

	wg.Add(numClients)
	for i := 0; i < numClients; i++ {
		go func(idx int) {
			defer wg.Done()
			c, err := net.Dial("unix", sockPath)
			conns[idx] = c
			connErr[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range connErr {
		if err != nil {
			t.Errorf("client %d connection failed: %v", i, err)
		}
	}

	time.Sleep(20 * time.Millisecond)
	if count := server.ClientCount(); count != numClients {
		t.Errorf("expected %d connected clients, got %d", numClients, count)
	}

	// Close all clients
	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}
