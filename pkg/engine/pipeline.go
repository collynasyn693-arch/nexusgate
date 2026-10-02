package engine

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"nexusgate/pkg/proxy"
	"nexusgate/pkg/resilience"
	"nexusgate/pkg/router"
	"nexusgate/pkg/telemetry"
)

// statusTrackingResponseWriter wraps http.ResponseWriter to track response status and bytes written.
type statusTrackingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten uint32
	headerWrote  bool
}

func (w *statusTrackingResponseWriter) WriteHeader(code int) {
	if !w.headerWrote {
		w.statusCode = code
		w.headerWrote = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *statusTrackingResponseWriter) Write(p []byte) (int, error) {
	if !w.headerWrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += uint32(n)
	return n, err
}

func (w *statusTrackingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusTrackingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusTrackingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return proxy.HijackConnection(w.ResponseWriter)
}

// BuildPipeline assembles the full middleware and proxy pipeline into an http.Handler.
func (s *Supervisor) BuildPipeline() http.Handler {
	cfg := s.ConfigHolder.Get()

	// Register all configured routes into the Router
	for _, r := range cfg.Routes {
		routeCopy := r
		upstreamPool, hasPool := s.Pools[routeCopy.UpstreamID]

		methods := routeCopy.Methods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
		}

		httpHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Mock response check
			if routeCopy.Mock != nil && routeCopy.Mock.Enabled {
				for k, v := range routeCopy.Mock.Headers {
					w.Header().Set(k, v)
				}
				statusCode := routeCopy.Mock.StatusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(routeCopy.Mock.Body))
				return
			}

			if !hasPool || upstreamPool == nil {
				http.Error(w, `{"error":"upstream pool not configured"}`, http.StatusBadGateway)
				return
			}

			// 1. Balancer selection
			backend, err := upstreamPool.Select(req.Context(), req)
			if err != nil || backend == nil {
				http.Error(w, `{"error":"no healthy upstream targets available"}`, http.StatusBadGateway)
				return
			}

			// 2. Circuit Breaker evaluation
			cbConfig := resilience.Config{
				FailureRateThreshold: 0.5,
				ConsecutiveFailures:  5,
				MinRequests:          5,
				ResetTimeout:         10 * time.Second,
				HalfOpenMaxRequests:  3,
			}
			breaker := s.Breakers.GetOrCreate(backend.RawURL, cbConfig)
			if !breaker.Allow() {
				w.Header().Set("Retry-After", "10")
				http.Error(w, `{"error":"upstream circuit breaker open"}`, http.StatusServiceUnavailable)
				return
			}
			defer breaker.ReleaseInflight()

			// 3. Reverse proxy execution
			proxyCtx := proxy.WithTargetURL(req.Context(), backend.URL)
			s.Proxy.ServeHTTP(w, req.WithContext(proxyCtx))

			// Feedback circuit breaker on response
			if sw, ok := w.(*statusTrackingResponseWriter); ok {
				if sw.statusCode >= 500 {
					breaker.RecordFailure()
				} else {
					breaker.RecordSuccess()
				}
			}
		})

		routerHandler := router.WrapHTTPHandler(httpHandler)
		for _, m := range methods {
			_ = s.Router.Handle(m, routeCopy.Path, routerHandler)
		}
	}

	// Wrap Router with Chaos Engine middleware
	var coreHandler http.Handler = s.Router
	if s.Chaos != nil {
		coreHandler = s.Chaos.Wrap(s.Router)
	}

	// Top-level Request Pipeline Handler
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		s.ActiveConns.Add(1)
		defer s.ActiveConns.Add(-1)

		if s.IdleDetector != nil {
			s.IdleDetector.OnActivity()
		}

		tw := &statusTrackingResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		coreHandler.ServeHTTP(tw, req)

		// Record Telemetry
		s.recordTelemetry(req, tw.statusCode, start, tw.bytesWritten)
	})
}

// recordTelemetry updates metric ring buffers, histograms, and rolling throughput counters.
func (s *Supervisor) recordTelemetry(req *http.Request, statusCode int, start time.Time, bytesOut uint32) {
	durationNs := time.Since(start).Nanoseconds()
	s.TotalReqs.Add(1)

	if s.Histogram != nil {
		s.Histogram.Record(durationNs)
	}

	if s.Throughput != nil {
		s.Throughput.Record(start.UnixNano(), statusCode, uint32(req.ContentLength), bytesOut)
	}

	if s.RingBuffer != nil {
		routeID := ""
		if req.URL != nil {
			routeID = req.URL.Path
		}
		event := telemetry.MetricEvent{
			Timestamp:  start.UnixNano(),
			LatencyNs:  durationNs,
			BytesIn:    uint32(req.ContentLength),
			BytesOut:   bytesOut,
			RouteID:    telemetry.HashRouteID(routeID),
			StatusCode: uint16(statusCode),
		}
		s.RingBuffer.Push(event)
	}

	// Dispatch frame to connected UDS clients if any are active
	if s.Dispatcher != nil && s.Dispatcher.SubscriberCount() > 0 {
		frame := s.Snapshot()
		s.Dispatcher.Broadcast(frame)
	}
}

// Serve starts the HTTP listener and serves traffic until context is canceled.
func (s *Supervisor) Serve(ctx context.Context) error {
	cfg := s.ConfigHolder.Get()
	addr := fmt.Sprintf("%s:%d", cfg.Listener.Host, cfg.Listener.Port)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind ingress listener on %s: %w", addr, err)
	}
	s.Listener = ln

	pipeline := s.BuildPipeline()
	server := &http.Server{
		Addr:              addr,
		Handler:           pipeline,
		ReadTimeout:       cfg.Listener.ReadTimeout,
		WriteTimeout:      cfg.Listener.WriteTimeout,
		IdleTimeout:       cfg.Listener.IdleTimeout,
		ReadHeaderTimeout: cfg.Listener.ReadHeaderTimeout,
	}
	s.HTTPServer = server
	s.running.Store(true)

	// Start UDS socket server if configured
	if s.UDSServer != nil {
		if err := s.UDSServer.Start(); err != nil {
			// Non-fatal if UDS cannot start
			fmt.Printf("Warning: failed to start UDS IPC server: %v\n", err)
		}
	}

	errChan := make(chan error, 1)
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}
