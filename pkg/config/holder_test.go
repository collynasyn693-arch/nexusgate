package config

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigHolderBasic(t *testing.T) {
	initial := NewDefaultConfig()
	initial.Version = "1.0"
	holder := NewConfigHolder(initial)

	got := holder.Get()
	if got.Version != "1.0" {
		t.Errorf("expected version 1.0, got %q", got.Version)
	}

	// Swap
	updated := initial.Clone()
	updated.Version = "2.0"
	old, err := holder.Swap(updated)
	if err != nil {
		t.Fatalf("Swap failed: %v", err)
	}
	if old.Version != "1.0" {
		t.Errorf("expected old version 1.0, got %q", old.Version)
	}
	if holder.Get().Version != "2.0" {
		t.Errorf("expected new version 2.0, got %q", holder.Get().Version)
	}

	// Nil swap rejected
	if _, err := holder.Swap(nil); err == nil {
		t.Errorf("expected error on nil swap")
	}
}

func TestConfigHolderConcurrentReadSwap(t *testing.T) {
	initial := NewDefaultConfig()
	holder := NewConfigHolder(initial)

	const numReaders = 100
	const numSwappers = 10
	const iterations = 500

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var readCount atomic.Uint64
	var swapCount atomic.Uint64

	// Launch 100 readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					cfg := holder.Get()
					if cfg == nil || cfg.Listener.Port == 0 {
						t.Errorf("reader %d read corrupt or nil config", readerID)
						return
					}
					readCount.Add(1)
				}
			}
		}(i)
	}

	// Launch 10 swappers
	for i := 0; i < numSwappers; i++ {
		wg.Add(1)
		go func(swapperID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				newCfg := initial.Clone()
				newCfg.Version = fmt.Sprintf("v-%d-%d", swapperID, j)
				newCfg.Listener.Port = 8080 + (j % 1000)
				_, err := holder.Swap(newCfg)
				if err != nil {
					t.Errorf("swapper %d failed swap at iter %d: %v", swapperID, j, err)
					return
				}
				swapCount.Add(1)
			}
		}(i)
	}

	// Wait for all swappers to finish
	var swapperWg sync.WaitGroup
	swapperWg.Add(numSwappers)
	// Track when swappers finish
	go func() {
		// Wait brief moment for iterations
		for swapCount.Load() < uint64(numSwappers*iterations) {
			time.Sleep(2 * time.Millisecond)
		}
		close(stop)
	}()

	wg.Wait()

	if readCount.Load() == 0 {
		t.Errorf("expected concurrent reads to execute, got 0")
	}
	if swapCount.Load() != uint64(numSwappers*iterations) {
		t.Errorf("expected %d swaps, got %d", numSwappers*iterations, swapCount.Load())
	}
}

func TestConfigHolderListenersAndUnsubscribe(t *testing.T) {
	initial := NewDefaultConfig()
	initial.Version = "v1"
	holder := NewConfigHolder(initial)

	var callbackCount atomic.Int32
	var lastOldVersion atomic.Value
	var lastNewVersion atomic.Value

	unsubscribe := holder.Subscribe(func(oldCfg, newCfg *GatewayConfig) {
		callbackCount.Add(1)
		lastOldVersion.Store(oldCfg.Version)
		lastNewVersion.Store(newCfg.Version)
	})

	// Perform first swap
	cfg2 := initial.Clone()
	cfg2.Version = "v2"
	_, _ = holder.Swap(cfg2)

	if callbackCount.Load() != 1 {
		t.Fatalf("expected callback count 1, got %d", callbackCount.Load())
	}
	if lastOldVersion.Load() != "v1" || lastNewVersion.Load() != "v2" {
		t.Errorf("expected v1 -> v2, got %v -> %v", lastOldVersion.Load(), lastNewVersion.Load())
	}

	// Unsubscribe and perform second swap
	unsubscribe()

	cfg3 := initial.Clone()
	cfg3.Version = "v3"
	_, _ = holder.Swap(cfg3)

	if callbackCount.Load() != 1 {
		t.Errorf("expected callback count to remain 1 after unsubscribe, got %d", callbackCount.Load())
	}
}

func TestConfigHolderUpdateRCU(t *testing.T) {
	initial := NewDefaultConfig()
	initial.Listener.Port = 8080
	holder := NewConfigHolder(initial)

	// Successful update
	updated, err := holder.Update(func(candidate *GatewayConfig) error {
		candidate.Listener.Port = 9000
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Listener.Port != 9000 {
		t.Errorf("expected port 9000, got %d", updated.Listener.Port)
	}
	if holder.Get().Listener.Port != 9000 {
		t.Errorf("expected holder.Get() port 9000, got %d", holder.Get().Listener.Port)
	}

	// Failing update (validation error: privileged port for non-root)
	_, err = holder.Update(func(candidate *GatewayConfig) error {
		candidate.Listener.Port = -5
		return nil
	})
	if err == nil {
		t.Fatalf("expected error on invalid update mutation")
	}
	// Active config must remain untouched (9000)
	if holder.Get().Listener.Port != 9000 {
		t.Errorf("expected active config to remain 9000 after failed update, got %d", holder.Get().Listener.Port)
	}
}
