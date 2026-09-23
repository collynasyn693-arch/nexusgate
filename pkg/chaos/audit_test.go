package chaos

import (
	"sync"
	"testing"
	"time"
)

type mockAuditLogger struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (m *mockAuditLogger) LogChaosEvent(event AuditEvent) {
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
}

func (m *mockAuditLogger) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func TestAuditRecorder_CountersAndEvents(t *testing.T) {
	mockLogger := &mockAuditLogger{}
	ar := NewAuditRecorder(mockLogger)

	ar.RecordDelay("route-1", "127.0.0.1", 100*time.Millisecond)
	ar.RecordStatus("route-1", "127.0.0.1", 503, "forced-status")
	ar.RecordDrop("route-2", "192.168.1.1")
	ar.RecordMock("route-3", "10.0.0.1", 200)
	ar.RecordRandomFault("route-4", "172.16.0.1")
	ar.RecordUnauthorized("route-5", "203.0.113.1", "bad-token")

	c := ar.Counters()
	if c.DelaysInjected != 1 {
		t.Errorf("expected DelaysInjected 1, got %d", c.DelaysInjected)
	}
	if c.StatusesInjected != 1 {
		t.Errorf("expected StatusesInjected 1, got %d", c.StatusesInjected)
	}
	if c.DropsInjected != 1 {
		t.Errorf("expected DropsInjected 1, got %d", c.DropsInjected)
	}
	if c.MocksServed != 1 {
		t.Errorf("expected MocksServed 1, got %d", c.MocksServed)
	}
	if c.RandomFaultsInjected != 1 {
		t.Errorf("expected RandomFaultsInjected 1, got %d", c.RandomFaultsInjected)
	}
	if c.UnauthorizedAttempts != 1 {
		t.Errorf("expected UnauthorizedAttempts 1, got %d", c.UnauthorizedAttempts)
	}

	if mockLogger.count() != 6 {
		t.Errorf("expected 6 logged events, got %d", mockLogger.count())
	}

	recent := ar.RecentEvents(10)
	if len(recent) != 6 {
		t.Fatalf("expected 6 recent events, got %d", len(recent))
	}
	if recent[0].Action != "delay" || recent[1].Action != "status" {
		t.Errorf("unexpected event ordering in ring buffer")
	}
}

func TestAuditRecorder_RingBufferRollover(t *testing.T) {
	ar := NewAuditRecorder(nil)

	// Record 300 events to force ring buffer wrap (>256)
	for i := 0; i < 300; i++ {
		ar.RecordDelay("route-x", "127.0.0.1", time.Duration(i)*time.Millisecond)
	}

	c := ar.Counters()
	if c.DelaysInjected != 300 {
		t.Errorf("expected 300 delays, got %d", c.DelaysInjected)
	}

	recent := ar.RecentEvents(10)
	if len(recent) != 10 {
		t.Fatalf("expected 10 recent events, got %d", len(recent))
	}

	// The most recent event should have duration = 299ms
	last := recent[len(recent)-1]
	if last.Duration != 299*time.Millisecond {
		t.Errorf("expected last event duration 299ms, got %v", last.Duration)
	}
}

func TestAuditRecorder_ConcurrentTracking(t *testing.T) {
	ar := NewAuditRecorder(nil)
	var wg sync.WaitGroup
	workers := 20
	iters := 100

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				ar.RecordDelay("r1", "127.0.0.1", 10*time.Millisecond)
				ar.RecordStatus("r1", "127.0.0.1", 500, "err")
				_ = ar.Counters()
				_ = ar.RecentEvents(5)
			}
		}()
	}

	wg.Wait()

	c := ar.Counters()
	expected := uint64(workers * iters)
	if c.DelaysInjected != expected {
		t.Errorf("expected %d delays, got %d", expected, c.DelaysInjected)
	}
	if c.StatusesInjected != expected {
		t.Errorf("expected %d statuses, got %d", expected, c.StatusesInjected)
	}
}
