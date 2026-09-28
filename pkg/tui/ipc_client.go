package tui

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nexusgate/pkg/telemetry"
)

// DefaultSocketPath returns the standard Unix Domain Socket path for NexusGate.
func DefaultSocketPath() string {
	prefix := os.Getenv("PREFIX")
	if prefix != "" {
		return filepath.Join(prefix, "tmp", "nexusgate.sock")
	}
	tmp := os.Getenv("TMPDIR")
	if tmp != "" {
		return filepath.Join(tmp, "nexusgate.sock")
	}
	return "/tmp/nexusgate.sock"
}

// IPCClient manages a streaming Unix Domain Socket connection to the gateway daemon.
type IPCClient struct {
	SocketPath  string
	DialTimeout time.Duration
	frameChan   chan telemetry.BinaryFrame
	errChan     chan error

	mu     sync.Mutex
	conn   net.Conn
	closed bool
}

// NewIPCClient creates a new IPC client targeting the specified or default socket path.
func NewIPCClient(socketPath string) *IPCClient {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &IPCClient{
		SocketPath:  socketPath,
		DialTimeout: 2 * time.Second,
		frameChan:   make(chan telemetry.BinaryFrame, 16),
		errChan:     make(chan error, 4),
	}
}

// Frames returns the channel receiving decoded telemetry binary frames.
func (c *IPCClient) Frames() <-chan telemetry.BinaryFrame {
	return c.frameChan
}

// Errors returns the channel receiving non-fatal connection and reading errors.
func (c *IPCClient) Errors() <-chan error {
	return c.errChan
}

// Start begins connecting to the UDS socket and streaming frames until context is done.
func (c *IPCClient) Start(ctx context.Context) {
	go func() {
		defer close(c.frameChan)
		defer close(c.errChan)

		readBuf := make([]byte, telemetry.BinaryFrameSize)

		for {
			select {
			case <-ctx.Done():
				c.Close()
				return
			default:
			}

			// Connect to socket
			conn, err := net.DialTimeout("unix", c.SocketPath, c.DialTimeout)
			if err != nil {
				c.reportError(err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
					continue
				}
			}

			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				conn.Close()
				return
			}
			c.conn = conn
			c.mu.Unlock()

			// Stream frames
			for {
				var frame telemetry.BinaryFrame
				err := telemetry.ReadFrame(conn, &frame, readBuf)
				if err != nil {
					c.reportError(err)
					break
				}

				select {
				case c.frameChan <- frame:
				default:
					// Drop oldest frame if consumer is lagging
					select {
					case <-c.frameChan:
					default:
					}
					c.frameChan <- frame
				}
			}

			c.mu.Lock()
			if c.conn != nil {
				c.conn.Close()
				c.conn = nil
			}
			c.mu.Unlock()

			// Backoff before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	}()
}

func (c *IPCClient) reportError(err error) {
	select {
	case c.errChan <- err:
	default:
	}
}

// Close disconnects the active socket connection.
func (c *IPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
