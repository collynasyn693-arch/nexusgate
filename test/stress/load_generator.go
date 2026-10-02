package stress

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// LoadConfig configures the load generator harness parameters.
type LoadConfig struct {
	TargetURL      string
	Concurrency    int
	TotalRequests  int64
	Duration       time.Duration
	Headers        map[string]string
	DisableKeepAlive bool
}

// LoadResult captures performance and reliability metrics of a load run.
type LoadResult struct {
	TotalRequests  int64
	SuccessCount   int64
	ErrorCount     int64
	StatusCodes    map[int]int64
	Duration       time.Duration
	RequestsPerSec float64
	TotalBytes     int64
}

// RunLoad executes a concurrent load test against the designated target URL.
func RunLoad(ctx context.Context, cfg LoadConfig) (*LoadResult, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.Concurrency * 2,
		MaxIdleConnsPerHost: cfg.Concurrency * 2,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   cfg.DisableKeepAlive,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	var (
		reqCounter   atomic.Int64
		successCount atomic.Int64
		errorCount   atomic.Int64
		totalBytes   atomic.Int64
		statusMu     sync.Mutex
		statusCodes  = make(map[int]int64)
	)

	recordStatus := func(code int) {
		statusMu.Lock()
		statusCodes[code]++
		statusMu.Unlock()
	}

	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if cfg.Duration > 0 {
		time.AfterFunc(cfg.Duration, cancel)
	}

	startTime := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 8192)

			for {
				select {
				case <-loadCtx.Done():
					return
				default:
				}

				if cfg.TotalRequests > 0 {
					current := reqCounter.Add(1)
					if current > cfg.TotalRequests {
						return
					}
				} else {
					reqCounter.Add(1)
				}

				req, err := http.NewRequestWithContext(loadCtx, http.MethodGet, cfg.TargetURL, nil)
				if err != nil {
					errorCount.Add(1)
					continue
				}

				for k, v := range cfg.Headers {
					req.Header.Set(k, v)
				}

				resp, err := client.Do(req)
				if err != nil {
					errorCount.Add(1)
					continue
				}

				recordStatus(resp.StatusCode)

				// Discard response body with buffer recycling
				n, _ := io.CopyBuffer(io.Discard, resp.Body, buf)
				_ = resp.Body.Close()

				totalBytes.Add(n)
				if resp.StatusCode < 500 {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(startTime)

	rps := float64(0)
	if duration.Seconds() > 0 {
		rps = float64(reqCounter.Load()) / duration.Seconds()
	}

	statusMu.Lock()
	copiedCodes := make(map[int]int64, len(statusCodes))
	for k, v := range statusCodes {
		copiedCodes[k] = v
	}
	statusMu.Unlock()

	return &LoadResult{
		TotalRequests:  reqCounter.Load(),
		SuccessCount:   successCount.Load(),
		ErrorCount:     errorCount.Load(),
		StatusCodes:    copiedCodes,
		Duration:       duration,
		RequestsPerSec: rps,
		TotalBytes:     totalBytes.Load(),
	}, nil
}
