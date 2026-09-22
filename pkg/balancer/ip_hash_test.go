package balancer

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestIPHash_SessionAffinity(t *testing.T) {
	b1 := NewBackend(mustURL("http://upstream-1"), 10, 0)
	b2 := NewBackend(mustURL("http://upstream-2"), 10, 0)
	b3 := NewBackend(mustURL("http://upstream-3"), 10, 0)
	b4 := NewBackend(mustURL("http://upstream-4"), 10, 0)

	balancer := NewIPHash([]*Backend{b1, b2, b3, b4})
	ctx := context.Background()

	testIPs := []string{
		"192.168.1.50:12345",
		"192.168.1.50",
		"10.0.0.1:80",
		"172.16.0.99:443",
		"[2001:db8::1]:8080",
		"[2001:db8::1]",
		"[fe80::1ff:fe23:4567:890a]:54321",
	}

	for _, ip := range testIPs {
		req, _ := http.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip

		firstChoice, err := balancer.Select(ctx, req)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}

		// Subsequent 20 requests from the same IP must map to the exact same backend
		for iter := 0; iter < 20; iter++ {
			choice, err := balancer.Select(ctx, req)
			if err != nil {
				t.Fatalf("unexpected select error at iter %d: %v", iter, err)
			}
			if choice.RawURL != firstChoice.RawURL {
				t.Fatalf("affinity broken for IP %s at iter %d: got %s, expected %s",
					ip, iter, choice.RawURL, firstChoice.RawURL)
			}
		}
	}
}

func TestIPHash_SubnetDispersion(t *testing.T) {
	backends := make([]*Backend, 4)
	for i := range backends {
		backends[i] = NewBackend(mustURL(fmt.Sprintf("http://backend-%d", i+1)), 10, 0)
	}

	balancer := NewIPHash(backends)
	ctx := context.Background()

	counts := make(map[string]int)
	const totalIPs = 1000

	// Generate 1000 sequential IPs within subnets
	for i := 0; i < totalIPs; i++ {
		subnet := (i / 250) + 1
		host := (i % 250) + 1
		ip := fmt.Sprintf("192.168.%d.%d:%d", subnet, host, 10000+i)

		req, _ := http.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip

		sel, err := balancer.Select(ctx, req)
		if err != nil {
			t.Fatalf("select error: %v", err)
		}
		counts[sel.RawURL]++
	}

	t.Logf("IP-Hash Subnet Dispersion counts: %v", counts)

	// Across 4 backends, each should receive roughly 250 requests (25%).
	// Assert each receives at least 150 (15%) and at most 350 (35%), proving zero bit-parity clustering.
	for _, b := range backends {
		c := counts[b.RawURL]
		if c < 150 || c > 350 {
			t.Errorf("backend %s received %d requests (%.1f%%); expected between 150 and 350",
				b.RawURL, c, float64(c)/float64(totalIPs)*100)
		}
	}
}

func TestIPHash_ForwardedHeaders(t *testing.T) {
	b1 := NewBackend(mustURL("http://b1"), 10, 0)
	b2 := NewBackend(mustURL("http://b2"), 10, 0)

	balancer := NewIPHash([]*Backend{b1, b2})
	ctx := context.Background()

	// Test X-Forwarded-For precedence over RemoteAddr
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")

	sel1, err := balancer.Select(ctx, req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Another request with same X-Forwarded-For client IP but different proxy RemoteAddr
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "10.0.0.1:5555"
	req2.Header.Set("X-Forwarded-For", "203.0.113.195")

	sel2, err := balancer.Select(ctx, req2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if sel1.RawURL != sel2.RawURL {
		t.Errorf("expected same backend for identical X-Forwarded-For client IP, got %s vs %s",
			sel1.RawURL, sel2.RawURL)
	}
}

func TestIPHash_EmptyOrSingle(t *testing.T) {
	bEmpty := NewIPHash(nil)
	if _, err := bEmpty.Select(context.Background(), nil); err != ErrNoHealthyBackends {
		t.Errorf("expected ErrNoHealthyBackends, got %v", err)
	}

	bSingle := NewBackend(mustURL("http://single"), 10, 0)
	balancer := NewIPHash([]*Backend{bSingle})

	req, _ := http.NewRequest("GET", "/", nil)
	sel, err := balancer.Select(context.Background(), req)
	if err != nil || sel != bSingle {
		t.Fatalf("expected bSingle, got %v, %v", sel, err)
	}

	// Nil request fallback
	selNil, err := balancer.Select(context.Background(), nil)
	if err != nil || selNil != bSingle {
		t.Fatalf("expected bSingle on nil request, got %v, %v", selNil, err)
	}
}
