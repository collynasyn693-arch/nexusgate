package resilience

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProberType indicates the probing protocol: HTTP or raw TCP.
type ProberType string

const (
	// ProberTypeHTTP executes an HTTP GET request against a designated health path.
	ProberTypeHTTP ProberType = "http"
	// ProberTypeTCP executes a raw TCP connect dial against the host and port.
	ProberTypeTCP ProberType = "tcp"
)

// ProberConfig defines configuration options for active health checking.
type ProberConfig struct {
	Type               ProberType    `json:"type" yaml:"type"`
	Target             string        `json:"target" yaml:"target"`       // e.g. "http://127.0.0.1:8081/healthz" or "127.0.0.1:8081"
	Path               string        `json:"path" yaml:"path"`           // Health check path, defaults to "/healthz"
	Interval           time.Duration `json:"interval" yaml:"interval"`   // Base interval between checks
	Timeout            time.Duration `json:"timeout" yaml:"timeout"`     // Maximum duration for a single probe
	HealthyThreshold   int           `json:"healthy_threshold" yaml:"healthy_threshold"`     // Consecutive successes to mark healthy
	UnhealthyThreshold int           `json:"unhealthy_threshold" yaml:"unhealthy_threshold"` // Consecutive failures to mark unhealthy
}

// DefaultProberConfig returns sane defaults for active target health checks.
func DefaultProberConfig(target string) ProberConfig {
	return ProberConfig{
		Type:               ProberTypeHTTP,
		Target:             target,
		Path:               "/healthz",
		Interval:           10 * time.Second,
		Timeout:            2 * time.Second,
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
	}
}

// Prober runs background health checks against an upstream destination node.
// Enforces socket draining and tight connection limits to stay well below Android
// Termux file descriptor limits (RLIMIT_NOFILE = 1024).
type Prober struct {
	cfg        ProberConfig
	client     *http.Client
	httpClient *http.Transport

	// Atomic health and status counters
	healthy            atomic.Bool
	consecutiveHealthy atomic.Int64
	consecutiveFailed  atomic.Int64
	running            atomic.Bool

	// Status change callback
	onStatusChange func(target string, healthy bool)

	// Lifecycle management
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewProber constructs an initialized Prober for the target.
func NewProber(cfg ProberConfig, onStatusChange func(target string, healthy bool)) *Prober {
	if cfg.Type == "" {
		if strings.HasPrefix(cfg.Target, "http://") || strings.HasPrefix(cfg.Target, "https://") {
			cfg.Type = ProberTypeHTTP
		} else {
			cfg.Type = ProberTypeTCP
		}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.HealthyThreshold <= 0 {
		cfg.HealthyThreshold = 2
	}
	if cfg.UnhealthyThreshold <= 0 {
		cfg.UnhealthyThreshold = 3
	}
	if cfg.Path == "" {
		cfg.Path = "/healthz"
	}

	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   cfg.Timeout,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: cfg.Timeout,
		DisableKeepAlives:     true, // Do not hoard keep-alive sockets on mobile
		MaxIdleConns:          2,
		IdleConnTimeout:       5 * time.Second,
	}

	p := &Prober{
		cfg:            cfg,
		httpClient:     tr,
		client:         &http.Client{Transport: tr, Timeout: cfg.Timeout},
		onStatusChange: onStatusChange,
	}
	p.healthy.Store(true) // Initial presumption of health
	return p
}

// Target returns the configured probe target.
func (p *Prober) Target() string {
	return p.cfg.Target
}

// IsHealthy reports the current health status of the target backend.
func (p *Prober) IsHealthy() bool {
	return p.healthy.Load()
}

// ConsecutiveHealthy returns the consecutive successful checks count.
func (p *Prober) ConsecutiveHealthy() int64 {
	return p.consecutiveHealthy.Load()
}

// ConsecutiveFailed returns the consecutive failed checks count.
func (p *Prober) ConsecutiveFailed() int64 {
	return p.consecutiveFailed.Load()
}

// CheckOnce performs a single immediate synchronous probe against the target.
func (p *Prober) CheckOnce(ctx context.Context) error {
	switch p.cfg.Type {
	case ProberTypeHTTP:
		return p.checkHTTP(ctx)
	case ProberTypeTCP:
		return p.checkTCP(ctx)
	default:
		return p.checkTCP(ctx)
	}
}

// checkHTTP executes an HTTP GET probe with strict body draining.
func (p *Prober) checkHTTP(ctx context.Context) error {
	probeURL := p.cfg.Target
	if !strings.HasPrefix(probeURL, "http://") && !strings.HasPrefix(probeURL, "https://") {
		probeURL = "http://" + probeURL
	}
	u, err := url.Parse(probeURL)
	if err != nil {
		return err
	}
	if p.cfg.Path != "" {
		u.Path = p.cfg.Path
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "NexusGate-HealthProber/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Drain response body to prevent socket leakage under RLIMIT_NOFILE
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("nexusgate: health probe HTTP status %d", resp.StatusCode)
	}
	return nil
}

// checkTCP executes a raw TCP connect dial with immediate socket cleanup.
func (p *Prober) checkTCP(ctx context.Context) error {
	addr := p.cfg.Target
	// Strip scheme if present
	if strings.HasPrefix(addr, "tcp://") {
		addr = strings.TrimPrefix(addr, "tcp://")
	} else if strings.HasPrefix(addr, "http://") {
		u, err := url.Parse(addr)
		if err == nil {
			addr = u.Host
		}
	} else if strings.HasPrefix(addr, "https://") {
		u, err := url.Parse(addr)
		if err == nil {
			addr = u.Host
		}
	}

	dialer := &net.Dialer{Timeout: p.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// RecordProbeResult updates hysteresis state counters and notifies status changes.
func (p *Prober) RecordProbeResult(err error) {
	if err == nil {
		// Probe Succeeded
		p.consecutiveFailed.Store(0)
		h := p.consecutiveHealthy.Add(1)

		if int(h) >= p.cfg.HealthyThreshold {
			if p.healthy.CompareAndSwap(false, true) {
				if p.onStatusChange != nil {
					p.onStatusChange(p.cfg.Target, true)
				}
			}
		}
	} else {
		// Probe Failed
		p.consecutiveHealthy.Store(0)
		f := p.consecutiveFailed.Add(1)

		if int(f) >= p.cfg.UnhealthyThreshold {
			if p.healthy.CompareAndSwap(true, false) {
				if p.onStatusChange != nil {
					p.onStatusChange(p.cfg.Target, false)
				}
			}
		}
	}
}

// Start initiates the background probing loop under the provided parent context.
func (p *Prober) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running.Load() {
		return nil // Already running
	}

	probeCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})
	p.running.Store(true)

	go p.probeLoop(probeCtx)
	return nil
}

// Stop halts the background probing loop and cleans up timers.
func (p *Prober) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running.Load() {
		return
	}

	p.running.Store(false)
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	if p.httpClient != nil {
		p.httpClient.CloseIdleConnections()
	}
}

// probeLoop executes periodic health probes with context awareness.
func (p *Prober) probeLoop(ctx context.Context) {
	defer close(p.done)

	timer := time.NewTimer(p.cfg.Interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			probeCtx, probeCancel := context.WithTimeout(ctx, p.cfg.Timeout)
			err := p.CheckOnce(probeCtx)
			probeCancel()

			p.RecordProbeResult(err)
			timer.Reset(p.cfg.Interval)
		}
	}
}
