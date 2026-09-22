package balancer

import (
	"context"
	"testing"
)

func TestSWWR_MathematicalProportions_70000(t *testing.T) {
	bA := NewBackend(mustURL("http://backend-a:8080"), 5, 0)
	bB := NewBackend(mustURL("http://backend-b:8080"), 1, 0)
	bC := NewBackend(mustURL("http://backend-c:8080"), 1, 0)

	balancer := NewSWWR([]*Backend{bA, bB, bC})

	counts := make(map[string]int)
	ctx := context.Background()
	const total = 70000

	for i := 0; i < total; i++ {
		selected, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected select error at iteration %d: %v", i, err)
		}
		counts[selected.RawURL]++
	}

	expectedA := 50000
	expectedB := 10000
	expectedC := 10000

	if counts[bA.RawURL] != expectedA {
		t.Errorf("backend A: expected %d requests, got %d", expectedA, counts[bA.RawURL])
	}
	if counts[bB.RawURL] != expectedB {
		t.Errorf("backend B: expected %d requests, got %d", expectedB, counts[bB.RawURL])
	}
	if counts[bC.RawURL] != expectedC {
		t.Errorf("backend C: expected %d requests, got %d", expectedC, counts[bC.RawURL])
	}
}

func TestSWWR_InterleavingSmoothness(t *testing.T) {
	bA := NewBackend(mustURL("http://a"), 5, 0)
	bB := NewBackend(mustURL("http://b"), 1, 0)
	bC := NewBackend(mustURL("http://c"), 1, 0)

	balancer := NewSWWR([]*Backend{bA, bB, bC})

	// For weights 5, 1, 1:
	// Nginx SWWR sequence: A, A, B, A, C, A, A
	expectedPattern := []string{"http://a", "http://a", "http://b", "http://a", "http://c", "http://a", "http://a"}
	ctx := context.Background()

	for i, exp := range expectedPattern {
		selected, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("select error at %d: %v", i, err)
		}
		if selected.RawURL != exp {
			t.Fatalf("step %d: expected %s, got %s", i, exp, selected.RawURL)
		}
	}
}

func TestSWWR_DynamicTargetChange(t *testing.T) {
	bA := NewBackend(mustURL("http://a"), 5, 0)
	bB := NewBackend(mustURL("http://b"), 1, 0)
	bC := NewBackend(mustURL("http://c"), 1, 0)

	balancer := NewSWWR([]*Backend{bA, bB, bC})
	ctx := context.Background()

	// Run 7,000 iterations initially
	for i := 0; i < 7000; i++ {
		_, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
	}

	// Dynamically drop bB
	balancer.UpdateTargets([]*Backend{bA, bC})

	// Total weight is now 5 + 1 = 6. Over 6,000 requests, A gets 5,000 and C gets 1,000.
	counts := make(map[string]int)
	for i := 0; i < 6000; i++ {
		sel, err := balancer.Select(ctx, nil)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		counts[sel.RawURL]++
	}

	if counts["http://a"] != 5000 {
		t.Errorf("expected 5000 for http://a, got %d", counts["http://a"])
	}
	if counts["http://c"] != 1000 {
		t.Errorf("expected 1000 for http://c, got %d", counts["http://c"])
	}
}

func TestSWWR_EmptyOrSingle(t *testing.T) {
	b := NewSWWR(nil)
	if _, err := b.Select(context.Background(), nil); err != ErrNoHealthyBackends {
		t.Errorf("expected ErrNoHealthyBackends for empty pool, got %v", err)
	}

	b1 := NewBackend(mustURL("http://single"), 10, 0)
	bSingle := NewSWWR([]*Backend{b1})
	for i := 0; i < 10; i++ {
		sel, err := bSingle.Select(context.Background(), nil)
		if err != nil || sel != b1 {
			t.Fatalf("expected single backend returned, got %v, %v", sel, err)
		}
	}
}
