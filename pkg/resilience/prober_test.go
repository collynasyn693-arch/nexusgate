package resilience

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProber_JitterDistribution(t *testing.T) {
	baseInterval := 10 * time.Second
	cfg := ProberConfig{
		Interval:    baseInterval,
		JitterRatio: 0.20, // +/- 20% -> [8s, 12s]
	}
	p := NewProber(cfg, nil)

	minExpected := 8 * time.Second
	maxExpected := 12 * time.Second

	var total time.Duration
	samples := 1000
	minObserved := 24 * time.Hour
	maxObserved := time.Duration(0)

	for i := 0; i < samples; i++ {
		interval := p.NextJitteredInterval()
		if interval < minExpected || interval > maxExpected {
			t.Fatalf("interval %v outside expected jitter range [%v, %v]", interval, minExpected, maxExpected)
		}
		total += interval
		if interval < minObserved {
			minObserved = interval
		}
		if interval > maxObserved {
			maxObserved = interval
		}
	}

	avg := total / time.Duration(samples)
	// Average should be very close to 10s (+/- 250ms over 1000 samples)
	diff := avg - baseInterval
	if diff < -250*time.Millisecond || diff > 250*time.Millisecond {
		t.Fatalf("average jittered interval %v deviates too far from base %v", avg, baseInterval)
	}

	// Must have variance
	if minObserved == maxObserved {
		t.Fatalf("expected variance in jittered intervals, but all samples were %v", minObserved)
	}
}

func TestProber_HTTPCheck(t *testing.T) {
	var statusCode atomic.Int32
	statusCode.Store(http.StatusOK)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(int(statusCode.Load()))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	cfg := ProberConfig{
		Type:     ProberTypeHTTP,
		Target:   server.URL,
		Path:     "/healthz",
		Timeout:  1 * time.Second,
		Interval: 50 * time.Millisecond,
	}

	p := NewProber(cfg, nil)

	ctx := context.Background()
	// 1. Healthy check
	if err := p.CheckOnce(ctx); err != nil {
		t.Fatalf("expected healthy check to succeed, got: %v", err)
	}

	// 2. Unhealthy check (500)
	statusCode.Store(http.StatusInternalServerError)
	if err := p.CheckOnce(ctx); err == nil {
		t.Fatalf("expected unhealthy check to return error for HTTP 500")
	}

	// 3. Unhealthy check (404)
	statusCode.Store(http.StatusNotFound)
	if err := p.CheckOnce(ctx); err == nil {
		t.Fatalf("expected unhealthy check to return error for HTTP 404")
	}
}

func TestProber_HysteresisTransitions(t *testing.T) {
	var lastStatus atomic.Bool
	var callbackCount atomic.Int64

	onStatusChange := func(target string, healthy bool) {
		lastStatus.Store(healthy)
		callbackCount.Add(1)
	}

	cfg := ProberConfig{
		Type:               ProberTypeHTTP,
		Target:             "http://dummy.target",
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
	}
	p := NewProber(cfg, onStatusChange)

	// Starts healthy by default
	if !p.IsHealthy() {
		t.Fatalf("expected initial healthy state")
	}

	// Record 1 failure -> still healthy
	p.RecordProbeResult(context.DeadlineExceeded)
	if !p.IsHealthy() || p.ConsecutiveFailed() != 1 {
		t.Fatalf("expected healthy after 1 failure, failCount=%d", p.ConsecutiveFailed())
	}

	// Record 2nd failure -> still healthy
	p.RecordProbeResult(context.DeadlineExceeded)
	if !p.IsHealthy() || p.ConsecutiveFailed() != 2 {
		t.Fatalf("expected healthy after 2 failures")
	}

	// Record 3rd failure -> reaches UnhealthyThreshold (3), must transition to unhealthy!
	p.RecordProbeResult(context.DeadlineExceeded)
	if p.IsHealthy() {
		t.Fatalf("expected unhealthy after 3 consecutive failures")
	}
	if callbackCount.Load() != 1 || lastStatus.Load() != false {
		t.Fatalf("expected 1 status change callback with healthy=false")
	}

	// Record 1 success -> still unhealthy (HealthyThreshold = 2)
	p.RecordProbeResult(nil)
	if p.IsHealthy() || p.ConsecutiveHealthy() != 1 {
		t.Fatalf("expected unhealthy after 1 success")
	}

	// Record 2nd success -> reaches HealthyThreshold (2), must transition to healthy!
	p.RecordProbeResult(nil)
	if !p.IsHealthy() {
		t.Fatalf("expected healthy after 2 consecutive successes")
	}
	if callbackCount.Load() != 2 || lastStatus.Load() != true {
		t.Fatalf("expected 2 status change callbacks with healthy=true")
	}
}

func TestProber_TCPCheck(t *testing.T) {
	// Start local TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local TCP: %v", err)
	}
	addr := listener.Addr().String()

	// Handle accepted connection asynchronously
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	cfg := ProberConfig{
		Type:    ProberTypeTCP,
		Target:  addr,
		Timeout: 500 * time.Millisecond,
	}
	p := NewProber(cfg, nil)

	ctx := context.Background()
	if err := p.CheckOnce(ctx); err != nil {
		t.Fatalf("expected TCP probe to succeed on open port: %v", err)
	}

	// Close listener and verify probe fails
	_ = listener.Close()
	time.Sleep(10 * time.Millisecond)

	if err := p.CheckOnce(ctx); err == nil {
		t.Fatalf("expected TCP probe to fail on closed port")
	}
}

func TestProber_LifecycleStartStop(t *testing.T) {
	var probeCalls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := ProberConfig{
		Type:        ProberTypeHTTP,
		Target:      server.URL,
		Path:        "/healthz",
		Interval:    20 * time.Millisecond,
		JitterRatio: 0.10,
		Timeout:     100 * time.Millisecond,
	}
	p := NewProber(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("failed to start prober: %v", err)
	}

	// Wait for at least 2 probe calls
	time.Sleep(60 * time.Millisecond)
	if probeCalls.Load() < 1 {
		t.Fatalf("expected at least 1 probe call during background run")
	}

	// Stop prober
	p.Stop()
	callsAtStop := probeCalls.Load()

	// Wait and assert no further calls are made
	time.Sleep(50 * time.Millisecond)
	if probeCalls.Load() > callsAtStop+1 {
		t.Fatalf("expected prober to stop calling target after Stop(), calls before=%d, now=%d", callsAtStop, probeCalls.Load())
	}
}
