package telemetry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBuffer_RaceMultiProducerSingleConsumer(t *testing.T) {
	const numProducers = 32
	const eventsPerProducer = 10000
	const totalEvents = numProducers * eventsPerProducer

	rb := NewRingBuffer(DefaultRingBufferSize)

	var (
		startCh           = make(chan struct{})
		producerWg        sync.WaitGroup
		consumerDone      = make(chan struct{})
		producersFinished atomic.Bool
		successPushed     atomic.Uint64
		totalPopped       atomic.Uint64
		dataCorruptions   atomic.Uint64
	)

	// Launch single consumer goroutine
	go func() {
		defer close(consumerDone)
		batch := make([]MetricEvent, 64)
		for {
			n := rb.BatchPop(batch)
			if n > 0 {
				for i := 0; i < n; i++ {
					ev := batch[i]
					// Verify integrity: StatusCode should match 200 + (Timestamp % 100)
					expectedStatus := uint16(200 + (ev.Timestamp % 100))
					if ev.StatusCode != expectedStatus {
						dataCorruptions.Add(1)
					}
					totalPopped.Add(1)
				}
			} else {
				// If all producers are done and buffer is empty, terminate cleanly
				if producersFinished.Load() && rb.IsEmpty() {
					return
				}
				time.Sleep(10 * time.Microsecond)
			}
		}
	}()

	// Launch 32 producer goroutines
	producerWg.Add(numProducers)
	for p := 0; p < numProducers; p++ {
		go func(producerID int) {
			defer producerWg.Done()
			<-startCh // Wait for simultaneous start signal

			for i := 0; i < eventsPerProducer; i++ {
				seq := int64(producerID*eventsPerProducer + i)
				ev := MetricEvent{
					Timestamp:  seq,
					LatencyNs:  seq * 100,
					RouteID:    uint32(producerID),
					StatusCode: uint16(200 + (seq % 100)),
					Flags:      FlagSuccess,
				}
				for {
					if rb.Push(ev) {
						successPushed.Add(1)
						break
					}
					// If buffer was full, yield briefly and retry to ensure all events arrive
					time.Sleep(5 * time.Microsecond)
				}
			}
		}(p)
	}

	// Trigger all producers simultaneously
	close(startCh)

	// Wait for all producers to finish
	producerWg.Wait()
	producersFinished.Store(true)

	// Wait for consumer to drain all pushed events
	select {
	case <-consumerDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for consumer: pushed=%d, popped=%d, dropped=%d",
			successPushed.Load(), totalPopped.Load(), rb.Dropped())
	}

	if corruptions := dataCorruptions.Load(); corruptions != 0 {
		t.Fatalf("detected %d corrupted events in race test", corruptions)
	}

	if pushed := successPushed.Load(); pushed != totalEvents {
		t.Fatalf("expected %d total pushed, got %d", totalEvents, pushed)
	}

	if popped := totalPopped.Load(); popped != totalEvents {
		t.Fatalf("expected %d total popped, got %d", totalEvents, popped)
	}

	if !rb.IsEmpty() {
		t.Fatalf("expected ring buffer to be empty at end of race test")
	}
}
