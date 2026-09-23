package chaos

import (
	"sync"
	"sync/atomic"
	"time"
)

const maxRecentEvents = 256

// AuditRecorder provides lock-free atomic counters and a ring buffer for chaos audit events.
type AuditRecorder struct {
	// Atomic counters (64-bit aligned for ARM64 multi-core cache lines)
	delaysInjected       atomic.Uint64
	statusesInjected     atomic.Uint64
	dropsInjected        atomic.Uint64
	mocksServed          atomic.Uint64
	randomFaultsInjected atomic.Uint64
	unauthorizedAttempts atomic.Uint64

	mu           sync.RWMutex
	recentEvents [maxRecentEvents]AuditEvent
	eventCursor  uint32
	eventCount   uint32
	logger       AuditLogger
}

// NewAuditRecorder constructs an initialized AuditRecorder.
func NewAuditRecorder(logger AuditLogger) *AuditRecorder {
	return &AuditRecorder{
		logger: logger,
	}
}

// Counters returns an atomic snapshot of all chaos telemetry metrics.
func (ar *AuditRecorder) Counters() AuditCountersSnapshot {
	return AuditCountersSnapshot{
		DelaysInjected:       ar.delaysInjected.Load(),
		StatusesInjected:     ar.statusesInjected.Load(),
		DropsInjected:        ar.dropsInjected.Load(),
		MocksServed:          ar.mocksServed.Load(),
		RandomFaultsInjected: ar.randomFaultsInjected.Load(),
		UnauthorizedAttempts: ar.unauthorizedAttempts.Load(),
	}
}

// SetLogger attaches an external AuditLogger (e.g. Stage 07 telemetry engine).
func (ar *AuditRecorder) SetLogger(logger AuditLogger) {
	ar.mu.Lock()
	ar.logger = logger
	ar.mu.Unlock()
}

// emitEvent appends an event to the internal ring buffer and forwards to any external logger.
func (ar *AuditRecorder) emitEvent(evt AuditEvent) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	ar.mu.Lock()
	idx := ar.eventCursor % maxRecentEvents
	ar.recentEvents[idx] = evt
	ar.eventCursor++
	if ar.eventCount < maxRecentEvents {
		ar.eventCount++
	}
	logger := ar.logger
	ar.mu.Unlock()

	if logger != nil {
		logger.LogChaosEvent(evt)
	}
}

// RecordDelay increments the delay counter and records a delay audit event.
func (ar *AuditRecorder) RecordDelay(routeID, clientIP string, d time.Duration) {
	ar.delaysInjected.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:  routeID,
		ClientIP: clientIP,
		Action:   "delay",
		Duration: d,
	})
}

// RecordStatus increments the status counter and records a forced-status audit event.
func (ar *AuditRecorder) RecordStatus(routeID, clientIP string, status int, reason string) {
	ar.statusesInjected.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:    routeID,
		ClientIP:   clientIP,
		Action:     "status",
		StatusCode: status,
		Error:      reason,
	})
}

// RecordDrop increments the drop counter and records a connection drop audit event.
func (ar *AuditRecorder) RecordDrop(routeID, clientIP string) {
	ar.dropsInjected.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:  routeID,
		ClientIP: clientIP,
		Action:   "drop",
	})
}

// RecordMock increments the mock counter and records a mock served audit event.
func (ar *AuditRecorder) RecordMock(routeID, clientIP string, status int) {
	ar.mocksServed.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:    routeID,
		ClientIP:   clientIP,
		Action:     "mock",
		StatusCode: status,
	})
}

// RecordRandomFault increments the random fault counter and records an audit event.
func (ar *AuditRecorder) RecordRandomFault(routeID, clientIP string) {
	ar.randomFaultsInjected.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:  routeID,
		ClientIP: clientIP,
		Action:   "random_fault",
	})
}

// RecordUnauthorized increments the unauthorized counter and records an audit event.
func (ar *AuditRecorder) RecordUnauthorized(routeID, clientIP string, reason string) {
	ar.unauthorizedAttempts.Add(1)
	ar.emitEvent(AuditEvent{
		RouteID:  routeID,
		ClientIP: clientIP,
		Action:   "unauthorized",
		Error:    reason,
	})
}

// RecentEvents returns the most recent audit events, ordered oldest to newest.
func (ar *AuditRecorder) RecentEvents(limit int) []AuditEvent {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	count := int(ar.eventCount)
	if count == 0 {
		return nil
	}
	if limit <= 0 || limit > count {
		limit = count
	}

	result := make([]AuditEvent, limit)
	start := int(ar.eventCursor) - limit
	if start < 0 {
		start = 0
	}

	for i := 0; i < limit; i++ {
		slot := (start + i) % maxRecentEvents
		result[i] = ar.recentEvents[slot]
	}
	return result
}
