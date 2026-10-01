package cli

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func startMockGatewayIPCServer(t *testing.T, sockPath string) net.Listener {
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create mock ipc socket: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				cmd := strings.TrimSpace(line)

				if strings.HasPrefix(cmd, "RELOAD") {
					_, _ = c.Write([]byte("OK configuration swapped atomically\n"))
				} else if strings.HasPrefix(cmd, "DRAIN") {
					_, _ = c.Write([]byte("OK target state transitioned to DRAINING\n"))
				} else if strings.HasPrefix(cmd, "UNDRAIN") {
					_, _ = c.Write([]byte("OK target restored to active rotation\n"))
				} else {
					_, _ = c.Write([]byte("ERROR unknown command\n"))
				}
			}(conn)
		}
	}()

	return ln
}

func TestCLI_ReloadCommand(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "mock_nexusgate.sock")
	ln := startMockGatewayIPCServer(t, sockPath)
	defer ln.Close()

	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	// Valid example config
	err := app.Execute(context.Background(), []string{"reload", "-c", "../../nexusgate.example.yaml", "-s", sockPath})
	if err != nil {
		err = app.Execute(context.Background(), []string{"reload", "-c", "nexusgate.example.yaml", "-s", sockPath})
	}
	if err != nil {
		t.Fatalf("expected reload to succeed, got: %v (errOut: %s)", err, errOut.String())
	}

	if !strings.Contains(out.String(), "[✓] Configuration reload successful") {
		t.Fatalf("expected reload success message, got: %s", out.String())
	}
}

func TestCLI_DrainCommand(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "mock_nexusgate.sock")
	ln := startMockGatewayIPCServer(t, sockPath)
	defer ln.Close()

	app := NewApp()
	RegisterAllCommands(app)

	var out bytes.Buffer
	var errOut bytes.Buffer
	app.SetOutput(&out, &errOut)

	target := "http://127.0.0.1:8081"
	err := app.Execute(context.Background(), []string{"drain", target, "-s", sockPath})
	if err != nil {
		t.Fatalf("expected drain to succeed, got: %v (errOut: %s)", err, errOut.String())
	}

	if !strings.Contains(out.String(), "is now DRAINING") {
		t.Fatalf("expected drain confirmation message, got: %s", out.String())
	}

	// Test undo
	out.Reset()
	err = app.Execute(context.Background(), []string{"drain", target, "--undo", "-s", sockPath})
	if err != nil {
		t.Fatalf("expected undrain to succeed, got: %v", err)
	}
	if !strings.Contains(out.String(), "ACTIVE (undrained)") {
		t.Fatalf("expected undrain confirmation message, got: %s", out.String())
	}
}
