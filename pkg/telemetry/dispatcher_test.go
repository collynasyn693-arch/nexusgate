package telemetry

import (
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func createUDSConnPair(t *testing.T, name string) (serverConn net.Conn, clientConn net.Conn, cleanup func()) {
	t.Helper()
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("disp_%d_%s.sock", rand.Uint32(), name))
	_ = os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on UDS: %v", err)
	}

	var (
		sConn     net.Conn
		acceptErr error
		wg        sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sConn, acceptErr = l.Accept()
	}()

	cConn, err := net.Dial("unix", sockPath)
	if err != nil {
		_ = l.Close()
		t.Fatalf("failed to dial UDS: %v", err)
	}
	wg.Wait()

	if acceptErr != nil {
		_ = l.Close()
		_ = cConn.Close()
		t.Fatalf("failed to accept UDS conn: %v", acceptErr)
	}

	cleanup = func() {
		_ = sConn.Close()
		_ = cConn.Close()
		_ = l.Close()
		_ = os.Remove(sockPath)
	}
	return sConn, cConn, cleanup
}

func TestDispatcher_FastSubscriberReceivesAll(t *testing.T) {
	d := NewDispatcher(16, 200*time.Millisecond)
	defer d.Close()

	serverConn, clientConn, cleanup := createUDSConnPair(t, "fast")
	defer cleanup()

	d.Subscribe(serverConn)
	if count := d.SubscriberCount(); count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}

	const totalFrames = 25
	var (
		receivedCount int
		wg            sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		var decoded BinaryFrame
		buf := make([]byte, BinaryFrameSize)
		for i := 0; i < totalFrames; i++ {
			if err := ReadFrame(clientConn, &decoded, buf); err != nil {
				return
			}
			receivedCount++
		}
	}()

	for i := 0; i < totalFrames; i++ {
		d.Broadcast(BinaryFrame{
			Version:       CurrentVersion,
			FrameType:     FrameTypeSnapshot,
			TotalRequests: uint64(i + 1),
		})
		time.Sleep(1 * time.Millisecond)
	}

	wg.Wait()

	if receivedCount != totalFrames {
		t.Errorf("expected %d received frames, got %d", totalFrames, receivedCount)
	}
	if dropped := d.SubscriberDropped(serverConn); dropped != 0 {
		t.Errorf("expected 0 dropped frames, got %d", dropped)
	}
}

func TestDispatcher_SlowSubscriberNonBlockingDrops(t *testing.T) {
	// Small channel buffer = 2
	d := NewDispatcher(2, 50*time.Millisecond)
	defer d.Close()

	// Fast client
	serverA, clientA, cleanupA := createUDSConnPair(t, "slow_a")
	defer cleanupA()
	d.Subscribe(serverA)

	// Stalled client (does not read)
	serverB, clientB, cleanupB := createUDSConnPair(t, "slow_b")
	defer cleanupB()
	_ = clientB
	d.Subscribe(serverB)

	// Broadcast enough frames to saturate the kernel socket buffer for client B
	const numFrames = 3000

	var (
		fastReceived int
		fastDone     = make(chan struct{})
	)

	go func() {
		defer close(fastDone)
		var decoded BinaryFrame
		buf := make([]byte, BinaryFrameSize)
		for {
			if err := ReadFrame(clientA, &decoded, buf); err != nil {
				return
			}
			fastReceived++
		}
	}()

	t0 := time.Now()
	for i := 0; i < numFrames; i++ {
		d.Broadcast(BinaryFrame{
			Version:       CurrentVersion,
			FrameType:     FrameTypeSnapshot,
			TotalRequests: uint64(i + 1),
		})
	}
	broadcastDuration := time.Since(t0)

	// Broadcast of 3,000 frames must finish in <500ms without blocking on client B
	if broadcastDuration > 500*time.Millisecond {
		t.Errorf("broadcast took too long (%v), expected non-blocking execution", broadcastDuration)
	}

	// Give fast client time to drain
	time.Sleep(50 * time.Millisecond)
	_ = clientA.Close()
	<-fastDone

	bDropped := d.SubscriberDropped(serverB)
	if bDropped == 0 {
		t.Errorf("expected stalled client B to experience dropped frames, got 0")
	}
	if d.TotalDropped() == 0 {
		t.Errorf("expected TotalDropped > 0")
	}
	t.Logf("Broadcast %d frames in %v; client B dropped %d frames without stalling fast client (received %d)",
		numFrames, broadcastDuration, bDropped, fastReceived)
}

func TestDispatcher_WriteTimeoutEviction(t *testing.T) {
	d := NewDispatcher(2, 20*time.Millisecond)
	defer d.Close()

	serverConn, clientConn, cleanup := createUDSConnPair(t, "evict")
	defer cleanup()
	_ = clientConn

	d.Subscribe(serverConn)

	for i := 0; i < 5000; i++ {
		d.Broadcast(BinaryFrame{
			Version:   CurrentVersion,
			FrameType: FrameTypeSnapshot,
		})
	}

	time.Sleep(100 * time.Millisecond)
	t.Logf("Subscriber count after write timeout test: %d", d.SubscriberCount())
}
