package telemetry

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"nexusgate/internal/platform"
)

// LinuxSunPathMax is the Linux kernel limit for sockaddr_un.sun_path (108 bytes).
const LinuxSunPathMax = 108

var (
	ErrPathTooLong = errors.New("UDS socket path exceeds Linux 108-byte sun_path limit")
	ErrServerClosed = errors.New("UDS server is closed")
)

// UDSServer provides a Unix Domain Socket server for high-efficiency IPC streaming
// to observatory and dashboard subscribers under Android Termux.
type UDSServer struct {
	socketPath   string
	listener     net.Listener
	mu           sync.Mutex
	clients      map[net.Conn]struct{}
	closed       atomic.Bool
	done         chan struct{}
	onConnect    func(conn net.Conn)
	onDisconnect func(conn net.Conn)
}

// NewUDSServer creates an initialized UDSServer bound to the specified socket path.
// If socketPath is empty, it resolves to platform.DefaultSocketPath().
func NewUDSServer(socketPath string, onConnect, onDisconnect func(net.Conn)) (*UDSServer, error) {
	if socketPath == "" {
		socketPath = platform.DefaultSocketPath()
	}
	if len(socketPath) >= LinuxSunPathMax {
		return nil, fmt.Errorf("%w: %q (%d >= %d)", ErrPathTooLong, socketPath, len(socketPath), LinuxSunPathMax)
	}

	return &UDSServer{
		socketPath:   socketPath,
		clients:      make(map[net.Conn]struct{}),
		done:         make(chan struct{}),
		onConnect:    onConnect,
		onDisconnect: onDisconnect,
	}, nil
}

// SocketPath returns the canonical path to the Unix Domain Socket.
func (s *UDSServer) SocketPath() string {
	return s.socketPath
}

// Start creates secure directories, cleans up stale sockets, binds the UDS listener,
// enforces 0600 POSIX permissions, and starts accepting client connections.
func (s *UDSServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return ErrServerClosed
	}
	if s.listener != nil {
		return fmt.Errorf("UDS server is already listening on %q", s.socketPath)
	}

	// 1. Ensure containing directory exists with 0700 permissions to neutralize TOCTOU windows
	dir := filepath.Dir(s.socketPath)
	if err := platform.EnsureSecureDirectory(dir); err != nil {
		return fmt.Errorf("failed to secure socket directory %q: %w", dir, err)
	}

	// 2. Clean up any stale socket from a previous terminated instance
	if err := platform.CleanupStaleSocket(s.socketPath); err != nil {
		return fmt.Errorf("socket pre-flight check failed: %w", err)
	}

	// 3. Bind Unix Domain Socket listener
	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to bind UDS listener on %q: %w", s.socketPath, err)
	}
	s.listener = l

	// 4. Enforce strict 0600 permissions
	if err := platform.EnforceSocketPermissions(s.socketPath); err != nil {
		_ = l.Close()
		_ = os.Remove(s.socketPath)
		return fmt.Errorf("failed to enforce 0600 permissions on socket %q: %w", s.socketPath, err)
	}

	// 5. Launch asynchronous accept loop
	go s.acceptLoop(l)
	return nil
}

func (s *UDSServer) acceptLoop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed or fatal error
			if s.closed.Load() {
				return
			}
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}

		s.registerClient(conn)
	}
}

func (s *UDSServer) registerClient(conn net.Conn) {
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		_ = conn.Close()
		return
	}
	s.clients[conn] = struct{}{}
	s.mu.Unlock()

	if s.onConnect != nil {
		s.onConnect(conn)
	}
}

// RemoveClient unregisters and closes a client connection.
func (s *UDSServer) RemoveClient(conn net.Conn) {
	s.mu.Lock()
	_, exists := s.clients[conn]
	if exists {
		delete(s.clients, conn)
	}
	s.mu.Unlock()

	if exists {
		_ = conn.Close()
		if s.onDisconnect != nil {
			s.onDisconnect(conn)
		}
	}
}

// ClientCount returns the number of currently connected clients.
func (s *UDSServer) ClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Stop gracefully terminates the UDS server, closes all active client connections,
// shuts down the listener, and unlinks the socket file.
func (s *UDSServer) Stop() error {
	s.mu.Lock()
	if s.closed.Swap(true) {
		s.mu.Unlock()
		return nil
	}
	close(s.done)

	// Copy and close all active clients
	activeClients := make([]net.Conn, 0, len(s.clients))
	for conn := range s.clients {
		activeClients = append(activeClients, conn)
	}
	s.clients = make(map[net.Conn]struct{})

	var listenerErr error
	if s.listener != nil {
		listenerErr = s.listener.Close()
		s.listener = nil
	}
	s.mu.Unlock()

	// Close clients outside lock
	for _, conn := range activeClients {
		_ = conn.Close()
		if s.onDisconnect != nil {
			s.onDisconnect(conn)
		}
	}

	// Unlink socket file
	_ = os.Remove(s.socketPath)
	return listenerErr
}
