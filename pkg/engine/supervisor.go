package engine

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"nexusgate/pkg/balancer"
	"nexusgate/pkg/chaos"
	"nexusgate/pkg/config"
	"nexusgate/pkg/proxy"
	"nexusgate/pkg/resilience"
	"nexusgate/pkg/router"
	"nexusgate/pkg/telemetry"
)

// Supervisor coordinates the lifecycle of all NexusGate subsystems.
type Supervisor struct {
	ConfigHolder *config.ConfigHolder
	Router       *router.COWRouter
	Pools        map[string]*balancer.Pool
	PoolsMu      sync.RWMutex
	Breakers     *resilience.BreakerRegistry
	Chaos        *chaos.Engine
	Proxy        *proxy.ReverseProxy
	RingBuffer   *telemetry.RingBuffer
	Throughput   *telemetry.ThroughputAggregator
	Histogram    *telemetry.LatencyHistogram
	IdleDetector *telemetry.IdleDetector
	Dispatcher   *telemetry.Dispatcher
	UDSServer    *telemetry.UDSServer
	SocketPath   string
	HTTPServer   *http.Server
	Listener     net.Listener

	ActiveConns atomic.Int64
	TotalReqs   atomic.Uint64
	StartTime   time.Time
	running     atomic.Bool
	stopChan    chan struct{}
}

// NewSupervisor builds and initializes a new Gateway Supervisor with all subsystems wired.
func NewSupervisor(cfg *config.GatewayConfig, socketPath string) (*Supervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil gateway configuration")
	}

	holder := config.NewConfigHolder(cfg)

	cowRouter := router.New(
		router.WithMethodNotAllowed(true),
		router.WithAutomaticOPTIONS(true),
	)

	// Build upstream target pools
	pools := make(map[string]*balancer.Pool)
	for _, u := range cfg.Upstreams {
		targets := make([]*balancer.Backend, 0, len(u.Targets))
		for _, t := range u.Targets {
			parsedURL, err := url.Parse(t.URL)
			if err != nil {
				return nil, fmt.Errorf("invalid target URL %q in upstream %q: %w", t.URL, u.ID, err)
			}
			weight := int64(t.Weight)
			if weight <= 0 {
				weight = 1
			}
			b := balancer.NewBackend(parsedURL, weight, int64(t.MaxConns))
			if t.Drain {
				b.SetDraining(true)
			}
			targets = append(targets, b)
		}

		bStrategy := balancer.NewRoundRobin(targets)
		pools[u.ID] = balancer.NewPool(u.ID, targets, bStrategy)
	}

	// Circuit breaker registry
	breakers := resilience.NewRegistry()

	// Chaos engine
	chaosCfg := chaos.Config{
		Enabled:             cfg.Chaos.Enabled,
		AdminKey:            cfg.Chaos.AdminKey,
		HeaderKey:           cfg.Chaos.HeaderKey,
		AllowedSubnets:      cfg.Chaos.AllowedSubnets,
		DefaultDelay:        cfg.Chaos.Delay,
		DefaultFailureRate:  cfg.Chaos.FailureRate,
		MaxDelay:            cfg.Chaos.MaxDelay,
		MaxBodyBytes:        cfg.Chaos.MaxBodyBytes,
		MaxConcurrentDelays: cfg.Chaos.MaxConcurrentDelays,
		StrictMode:          cfg.Chaos.StrictMode,
	}
	chaosEng, err := chaos.NewEngine(chaosCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create chaos engine: %w", err)
	}

	// Reverse proxy engine
	revProxy := proxy.NewReverseProxy(proxy.Config{
		FlushInterval: 10 * time.Millisecond,
	})

	// Telemetry subsystems
	ringBuf := telemetry.NewRingBuffer(65536)
	throughput := telemetry.NewThroughputAggregator()
	histogram := telemetry.NewLatencyHistogram()
	idleDetector := telemetry.NewIdleDetector(3 * time.Second)
	dispatcher := telemetry.NewDispatcher(16, 250*time.Millisecond)

	s := &Supervisor{
		ConfigHolder: holder,
		Router:       cowRouter,
		Pools:        pools,
		Breakers:     breakers,
		Chaos:        chaosEng,
		Proxy:        revProxy,
		RingBuffer:   ringBuf,
		Throughput:   throughput,
		Histogram:    histogram,
		IdleDetector: idleDetector,
		Dispatcher:   dispatcher,
		SocketPath:   socketPath,
		StartTime:    time.Now(),
		stopChan:     make(chan struct{}),
	}

	// Initialize UDS server
	udsServer, err := telemetry.NewUDSServer(socketPath, func(conn net.Conn) {
		dispatcher.Subscribe(conn)
	}, func(conn net.Conn) {
		dispatcher.Unsubscribe(conn)
	})
	if err == nil {
		s.UDSServer = udsServer
	}

	return s, nil
}

// DrainTarget sets the drain status on any backend matching targetURL across all pools.
func (s *Supervisor) DrainTarget(targetURL string, drain bool) int {
	s.PoolsMu.RLock()
	defer s.PoolsMu.RUnlock()

	matched := 0
	for _, p := range s.Pools {
		targets := p.Targets()
		for _, b := range targets {
			if b.RawURL == targetURL {
				b.SetDraining(drain)
				matched++
			}
		}
	}
	return matched
}

// Snapshot returns an atomic snapshot of live gateway telemetry.
func (s *Supervisor) Snapshot() telemetry.BinaryFrame {
	now := time.Now()
	nowSec := now.Unix()
	rps1s, _, _ := s.Throughput.Rates()
	pcts := s.Histogram.Percentiles()
	c2xx, c3xx, c4xx, c5xx := s.Throughput.RollingStatusCounts(nowSec)

	frame := telemetry.BinaryFrame{
		Version:           telemetry.CurrentVersion,
		FrameType:         telemetry.FrameTypeSnapshot,
		TimestampUnixNano: now.UnixNano(),
		TotalRequests:     s.TotalReqs.Load(),
		ActiveConns:       uint32(s.ActiveConns.Load()),
		RPS1s:             uint32(rps1s * 1000.0),
		P50LatencyUs:      uint32(pcts.P50),
		P90LatencyUs:      uint32(pcts.P90),
		P99LatencyUs:      uint32(pcts.P99),
		Status2xxCount:    c2xx,
		Status3xxCount:    c3xx,
		Status4xxCount:    c4xx,
		Status5xxCount:    c5xx,
	}
	return frame
}
