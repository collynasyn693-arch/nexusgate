package tui

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexusgate/pkg/telemetry"
)

func TestIPCClient_FrameConsumption(t *testing.T) {
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "test_nexusgate.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewIPCClient(sockPath)
	client.DialTimeout = 500 * time.Millisecond
	client.Start(ctx)

	// Mock server accept loop
	frameToSend := telemetry.BinaryFrame{
		Version:           telemetry.CurrentVersion,
		FrameType:         telemetry.FrameTypeSnapshot,
		TimestampUnixNano: time.Now().UnixNano(),
		TotalRequests:     1337,
		ActiveConns:       42,
		RPS1s:             12500, // 12.5 RPS
		P50LatencyUs:      850,
		P90LatencyUs:      1800,
		P99LatencyUs:      4200,
		Status2xxCount:    1300,
		Status4xxCount:    35,
		Status5xxCount:    2,
	}

	buf := make([]byte, telemetry.BinaryFrameSize)
	if err := frameToSend.Encode(buf); err != nil {
		t.Fatalf("failed to encode frame: %v", err)
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Write frame to client
		_, _ = conn.Write(buf)
	}()

	// Await frame in client
	select {
	case frame := <-client.Frames():
		if frame.TotalRequests != 1337 {
			t.Fatalf("expected TotalRequests=1337, got %d", frame.TotalRequests)
		}
		if frame.ActiveConns != 42 {
			t.Fatalf("expected ActiveConns=42, got %d", frame.ActiveConns)
		}
		if frame.RPS1s != 12500 {
			t.Fatalf("expected RPS1s=12500, got %d", frame.RPS1s)
		}
		if frame.P50LatencyUs != 850 {
			t.Fatalf("expected P50LatencyUs=850, got %d", frame.P50LatencyUs)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for frame from mock server")
	}

	client.Close()
	<-serverDone
}

func TestDefaultSocketPath(t *testing.T) {
	origPrefix := os.Getenv("PREFIX")
	defer os.Setenv("PREFIX", origPrefix)

	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	p := DefaultSocketPath()
	if p != "/data/data/com.termux/files/usr/tmp/nexusgate.sock" {
		t.Fatalf("unexpected socket path with PREFIX: %s", p)
	}
}
