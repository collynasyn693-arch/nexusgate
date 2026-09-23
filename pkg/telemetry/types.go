package telemetry

import (
	"hash/fnv"
	"unsafe"
)

// Invariant: MetricEvent must be strictly 32 bytes to pack two events per 64-byte
// CPU cache line on ARM64 processors without cache line straddling or split-access penalties.
const MetricEventExpectedSize = 32

func init() {
	if sz := unsafe.Sizeof(MetricEvent{}); sz != MetricEventExpectedSize {
		panic("MetricEvent size mismatch: expected 32 bytes")
	}
}

// Event flag bitmasks for MetricEvent.Flags.
const (
	FlagSuccess       uint16 = 0x0001
	FlagError         uint16 = 0x0002
	FlagChaosInjected uint16 = 0x0004
	FlagMockServed    uint16 = 0x0008
	FlagRateLimited   uint16 = 0x0010
	FlagCircuitBroken uint16 = 0x0020
)

// MetricEvent represents a single request lifecycle telemetry event.
// Packed strictly to 32 bytes with natural memory alignment:
// - int64 (8B, offset 0)
// - int64 (8B, offset 8)
// - uint32 (4B, offset 16)
// - uint32 (4B, offset 20)
// - uint32 (4B, offset 24)
// - uint16 (2B, offset 28)
// - uint16 (2B, offset 30)
// Total: 32 bytes, 0 compiler padding.
type MetricEvent struct {
	Timestamp  int64  // Unix timestamp in nanoseconds (8 bytes)
	LatencyNs  int64  // Request latency duration in nanoseconds (8 bytes)
	BytesIn    uint32 // Request payload bytes read (4 bytes)
	BytesOut   uint32 // Response payload bytes written (4 bytes)
	RouteID    uint32 // Numeric route identifier or FNV-1a hash (4 bytes)
	StatusCode uint16 // HTTP response status code (2 bytes)
	Flags      uint16 // Event flags / bitmask indicators (2 bytes)
}

// HashRouteID computes a deterministic 32-bit FNV-1a hash of a string route ID.
// This enables zero-allocation mapping from string route identifiers to MetricEvent.RouteID.
func HashRouteID(routeID string) uint32 {
	if routeID == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(routeID))
	return h.Sum32()
}

// TelemetryRecorder defines the contract for emitting metric events in the gateway hot path.
type TelemetryRecorder interface {
	Record(event MetricEvent) bool
	RecordRequest(timestamp int64, latencyNs int64, bytesIn, bytesOut uint32, routeID string, statusCode int, flags uint16) bool
}
