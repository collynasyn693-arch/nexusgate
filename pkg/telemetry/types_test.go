package telemetry

import (
	"testing"
	"unsafe"
)

func TestMetricEvent_SizeAndAlignment(t *testing.T) {
	var e MetricEvent
	if sz := unsafe.Sizeof(e); sz != 32 {
		t.Fatalf("expected MetricEvent size 32, got %d", sz)
	}
	if al := unsafe.Alignof(e); al != 8 {
		t.Fatalf("expected MetricEvent alignment 8, got %d", al)
	}

	// Verify field offsets
	if off := unsafe.Offsetof(e.Timestamp); off != 0 {
		t.Errorf("Timestamp offset expected 0, got %d", off)
	}
	if off := unsafe.Offsetof(e.LatencyNs); off != 8 {
		t.Errorf("LatencyNs offset expected 8, got %d", off)
	}
	if off := unsafe.Offsetof(e.BytesIn); off != 16 {
		t.Errorf("BytesIn offset expected 16, got %d", off)
	}
	if off := unsafe.Offsetof(e.BytesOut); off != 20 {
		t.Errorf("BytesOut offset expected 20, got %d", off)
	}
	if off := unsafe.Offsetof(e.RouteID); off != 24 {
		t.Errorf("RouteID offset expected 24, got %d", off)
	}
	if off := unsafe.Offsetof(e.StatusCode); off != 28 {
		t.Errorf("StatusCode offset expected 28, got %d", off)
	}
	if off := unsafe.Offsetof(e.Flags); off != 30 {
		t.Errorf("Flags offset expected 30, got %d", off)
	}
}

func TestHashRouteID(t *testing.T) {
	if h := HashRouteID(""); h != 0 {
		t.Errorf("expected 0 for empty route ID, got %d", h)
	}
	h1 := HashRouteID("api_v1_users")
	h2 := HashRouteID("api_v1_users")
	h3 := HashRouteID("api_v1_orders")
	if h1 == 0 {
		t.Errorf("expected non-zero hash")
	}
	if h1 != h2 {
		t.Errorf("expected deterministic hash: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("expected distinct hash for different routes: %d == %d", h1, h3)
	}
}
