package balancer

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBalancerFactory_Instantiation(t *testing.T) {
	b1 := NewBackend(mustURL("http://10.0.0.1:8080"), 10, 0)
	targets := []*Backend{b1}

	validAlgos := []BalancingAlgorithm{
		AlgorithmSWWR,
		AlgorithmSWRR,
		AlgorithmRoundRobin,
		AlgorithmP2C,
		AlgorithmPeakEWMA,
		AlgorithmIPHash,
	}

	for _, algo := range validAlgos {
		bal, err := NewBalancer(algo, targets)
		if err != nil {
			t.Errorf("NewBalancer(%q) returned unexpected error: %v", algo, err)
		}
		if bal == nil {
			t.Errorf("NewBalancer(%q) returned nil balancer", algo)
		}
	}

	// Invalid algorithm must return error
	_, err := NewBalancer("unsupported_algo", targets)
	if err != ErrInvalidAlgorithm {
		t.Errorf("expected ErrInvalidAlgorithm, got: %v", err)
	}
}

func TestBalancerFactory_DynamicSwitchingUnderTraffic(t *testing.T) {
	backends := make([]*Backend, 4)
	for i := range backends {
		backends[i] = NewBackend(mustURL(fmt.Sprintf("http://backend-%d", i+1)), int64(10*(i+1)), 0)
	}

	initialBal, err := NewBalancer(AlgorithmSWWR, backends)
	if err != nil {
		t.Fatalf("failed creating initial SWWR balancer: %v", err)
	}

	pool := NewPool("hot-reload-pool", backends, initialBal)

	const numWorkers = 20
	const runDuration = 100 * time.Millisecond

	var totalSuccess atomic.Uint64
	var totalErrors atomic.Uint64
	stopCh := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	// Launch worker goroutines continuously calling pool.Select
	for w := 0; w < numWorkers; w++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					sel, err := pool.Select(context.Background(), req)
					if err != nil || sel == nil {
						totalErrors.Add(1)
					} else {
						totalSuccess.Add(1)
					}
				}
			}
		}()
	}

	// Cycle through balancing algorithms dynamically while traffic is live
	algos := []BalancingAlgorithm{
		AlgorithmP2C,
		AlgorithmPeakEWMA,
		AlgorithmIPHash,
		AlgorithmRoundRobin,
		AlgorithmSWWR,
	}

	for _, nextAlgo := range algos {
		time.Sleep(15 * time.Millisecond)
		if err := ReloadPoolAlgorithm(pool, nextAlgo); err != nil {
			t.Errorf("ReloadPoolAlgorithm(%q) failed: %v", nextAlgo, err)
		}
	}

	time.Sleep(runDuration)
	close(stopCh)
	wg.Wait()

	t.Logf("Hot-reload traffic stats: %d successful selections, %d errors",
		totalSuccess.Load(), totalErrors.Load())

	if totalErrors.Load() > 0 {
		t.Errorf("encountered %d errors during dynamic algorithm switching under traffic", totalErrors.Load())
	}
	if totalSuccess.Load() < 1000 {
		t.Errorf("expected >= 1000 successful selections, got %d", totalSuccess.Load())
	}
}
