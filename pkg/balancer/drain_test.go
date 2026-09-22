package balancer

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestBackendDrain_IdleImmediateReturn(t *testing.T) {
	b := NewBackend(mustURL("http://idle-backend"), 10, 0)

	start := time.Now()
	// Drain with 5-second timeout on an idle backend (0 active conns)
	err := b.Drain(context.Background(), 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil error on idle drain, got: %v", err)
	}

	// Must return immediately (< 50ms), not wait for 5s timeout
	if elapsed > 50*time.Millisecond {
		t.Errorf("idle drain took %v; expected immediate return (< 50ms)", elapsed)
	}

	if !b.IsDraining() {
		t.Errorf("expected b.IsDraining() to be true")
	}
}

func TestBackendDrain_ActiveConnsBleed(t *testing.T) {
	b := NewBackend(mustURL("http://active-backend"), 10, 0)

	// Acquire 3 active connections
	for i := 0; i < 3; i++ {
		if err := b.AcquireConn(); err != nil {
			t.Fatalf("failed acquiring connection %d: %v", i, err)
		}
	}
	if b.Inflight() != 3 {
		t.Fatalf("expected 3 inflight, got %d", b.Inflight())
	}

	drainDone := make(chan error, 1)
	go func() {
		drainDone <- b.Drain(context.Background(), 2*time.Second)
	}()

	// Wait briefly for drain to initiate
	time.Sleep(10 * time.Millisecond)

	// Attempting new acquire must be rejected
	if err := b.AcquireConn(); err != ErrBackendDraining {
		t.Errorf("expected ErrBackendDraining on new acquire, got: %v", err)
	}

	// Release 2 connections: drain should still be waiting
	b.ReleaseConn()
	b.ReleaseConn()
	if b.Inflight() != 1 {
		t.Fatalf("expected 1 inflight, got %d", b.Inflight())
	}

	select {
	case err := <-drainDone:
		t.Fatalf("drain completed prematurely while 1 connection still inflight: %v", err)
	default:
		// Expected to still be waiting
	}

	// Release the last connection
	b.ReleaseConn()
	if b.Inflight() != 0 {
		t.Fatalf("expected 0 inflight, got %d", b.Inflight())
	}

	select {
	case err := <-drainDone:
		if err != nil {
			t.Errorf("expected clean drain completion, got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("drain failed to complete within 500ms after last connection release")
	}
}

func TestBackendDrain_Timeout(t *testing.T) {
	b := NewBackend(mustURL("http://stuck-backend"), 10, 0)
	if err := b.AcquireConn(); err != nil {
		t.Fatalf("acquire error: %v", err)
	}

	// Drain with 50ms timeout; do NOT release connection
	err := b.Drain(context.Background(), 50*time.Millisecond)
	if err != ErrDrainTimeout {
		t.Errorf("expected ErrDrainTimeout, got: %v", err)
	}

	// Clean up connection
	b.ReleaseConn()
}

func TestTargetPool_DrainIntegration(t *testing.T) {
	b1 := NewBackend(mustURL("http://b1:8080"), 10, 0)
	b2 := NewBackend(mustURL("http://b2:8080"), 10, 0)

	pool := NewPool("pool-drain", []*Backend{b1, b2}, NewSWWR([]*Backend{b1, b2}))
	ctx := context.Background()

	// Acquire 2 connections on b1
	_ = b1.AcquireConn()
	_ = b1.AcquireConn()

	var wg sync.WaitGroup
	wg.Add(1)

	var drainErr error
	go func() {
		defer wg.Done()
		drainErr = pool.Drain(ctx, "http://b1:8080", 2*time.Second)
	}()

	time.Sleep(10 * time.Millisecond)

	// Healthy targets in pool must now contain only b2
	healthy := pool.HealthyTargets()
	if len(healthy) != 1 || healthy[0].RawURL != "http://b2:8080" {
		t.Fatalf("expected healthy targets to only contain b2, got %+v", healthy)
	}

	// All new pool selections must route strictly to b2
	for i := 0; i < 50; i++ {
		sel, err := pool.Select(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		if sel.RawURL != "http://b2:8080" {
			t.Fatalf("request routed to draining backend %s!", sel.RawURL)
		}
	}

	// Release in-flight connections on b1
	b1.ReleaseConn()
	b1.ReleaseConn()

	wg.Wait()
	if drainErr != nil {
		t.Errorf("pool drain returned error: %v", drainErr)
	}
}

func TestBackendDrain_Reset(t *testing.T) {
	b := NewBackend(mustURL("http://b-reset"), 10, 0)
	_ = b.Drain(context.Background(), 10*time.Millisecond)

	if !b.IsDraining() {
		t.Fatalf("expected draining to be true")
	}

	b.ResetDrain()
	if b.IsDraining() {
		t.Fatalf("expected draining to be false after ResetDrain()")
	}

	// Should be able to acquire again
	if err := b.AcquireConn(); err != nil {
		t.Fatalf("acquire after reset failed: %v", err)
	}
	b.ReleaseConn()
}
