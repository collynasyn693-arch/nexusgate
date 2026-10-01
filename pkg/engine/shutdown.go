package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

var (
	ErrNotRunning = errors.New("gateway supervisor is not running")
)

// Shutdown performs a graceful zero-loss teardown of the gateway:
// 1. Closes ingress listener to stop accepting new traffic.
// 2. Drains active inflight proxy connections within the context timeout.
// 3. Flushes telemetry dispatchers and closes UDS IPC socket.
// 4. Unlinks the Unix domain socket from the filesystem.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	if !s.running.CompareAndSwap(true, false) {
		return nil
	}

	close(s.stopChan)

	var shutdownErr error

	// 1. Gracefully shut down HTTP server
	if s.HTTPServer != nil {
		if err := s.HTTPServer.Shutdown(ctx); err != nil {
			shutdownErr = fmt.Errorf("http server shutdown error: %w", err)
		}
	}

	// 2. Wait briefly for active connections to drop to zero or timeout
	drainDeadline := time.Now().Add(5 * time.Second)
	for s.ActiveConns.Load() > 0 && time.Now().Before(drainDeadline) {
		select {
		case <-ctx.Done():
			break
		case <-time.After(50 * time.Millisecond):
		}
	}

	// 3. Close UDS server and unlink socket file
	if s.UDSServer != nil {
		if err := s.UDSServer.Stop(); err != nil && shutdownErr == nil {
			shutdownErr = fmt.Errorf("uds server close error: %w", err)
		}
	}

	if s.SocketPath != "" {
		_ = os.Remove(s.SocketPath)
	}

	// 4. Close telemetry dispatcher
	if s.Dispatcher != nil {
		s.Dispatcher.Close()
	}

	return shutdownErr
}
