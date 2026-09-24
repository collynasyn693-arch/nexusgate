package router

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCOWRouterBasic(t *testing.T) {
	r := New()
	h := dummyHandler("basic")

	if err := r.Handle(http.MethodGet, "/hello", h); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	var p Params
	matched, ok := r.Lookup(http.MethodGet, "/hello", &p)
	if !ok || matched == nil {
		t.Fatal("expected /hello to match")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	r.ServeHTTP(w, req)
}

func TestCOWTransactionalRollback(t *testing.T) {
	r := New()
	_ = r.Handle(http.MethodGet, "/stable", dummyHandler("stable"))

	// Attempt an invalid registration batch that fails mid-way
	err := r.Handle(http.MethodGet, "/users/:id/posts", dummyHandler("posts"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Register conflicting parameter route that fails
	err = r.Handle(http.MethodGet, "/users/:user_id/comments", dummyHandler("comments"))
	if !errors.Is(err, ErrParamConflict) {
		t.Fatalf("expected ErrParamConflict, got: %v", err)
	}

	var p Params
	_, ok := r.Lookup(http.MethodGet, "/users/123/comments", &p)
	if ok {
		t.Error("candidate route should not exist after aborted transaction")
	}

	// Stable route must remain intact
	_, ok = r.Lookup(http.MethodGet, "/stable", &p)
	if !ok {
		t.Error("stable route should remain after aborted transaction")
	}
}

func TestCOWConcurrentStress(t *testing.T) {
	router := New()

	// Seed base routes
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/static/base/%d", i)
		_ = router.Handle(http.MethodGet, path, dummyHandler(path))
	}
	_ = router.Handle(http.MethodGet, "/users/:id/profile", dummyHandler("profile"))
	_ = router.Handle(http.MethodGet, "/files/*filepath", dummyHandler("files"))

	const (
		numReaders = 16
		numWriters = 4
		numOps     = 25
	)

	var (
		wg      sync.WaitGroup
		running atomic.Bool
	)
	running.Store(true)

	// Launch parallel reader goroutines
	var successfulReads atomic.Uint64
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var p Params
			for running.Load() {
				// Query static route
				baseID := id % 20
				p = p[:0]
				h, ok := router.Lookup(http.MethodGet, fmt.Sprintf("/static/base/%d", baseID), &p)
				if ok && h != nil {
					successfulReads.Add(1)
				}

				// Query param route
				p = p[:0]
				h, ok = router.Lookup(http.MethodGet, fmt.Sprintf("/users/u-%d/profile", id), &p)
				if ok && h != nil && p.ByName("id") == fmt.Sprintf("u-%d", id) {
					successfulReads.Add(1)
				}

				// Query wildcard route
				p = p[:0]
				h, ok = router.Lookup(http.MethodGet, fmt.Sprintf("/files/docs/%d/spec.pdf", id), &p)
				if ok && h != nil && p.ByName("filepath") == fmt.Sprintf("docs/%d/spec.pdf", id) {
					successfulReads.Add(1)
				}
				runtime.Gosched()
			}
		}(i)
	}

	// Launch parallel writer goroutines doing Copy-On-Write mutations
	var writerWg sync.WaitGroup
	var successfulWrites atomic.Uint64
	for w := 0; w < numWriters; w++ {
		writerWg.Add(1)
		go func(workerID int) {
			defer writerWg.Done()
			for op := 0; op < numOps; op++ {
				path := fmt.Sprintf("/dynamic/worker-%d/route-%d", workerID, op)
				err := router.Handle(http.MethodGet, path, dummyHandler(path))
				if err == nil {
					successfulWrites.Add(1)
				}
			}
		}(w)
	}

	// Wait for writers to complete all mutations
	writerWg.Wait()
	running.Store(false)
	wg.Wait()

	t.Logf("Stress test completed: %d successful reads, %d successful writes",
		successfulReads.Load(), successfulWrites.Load())
}

func TestCOWConcurrentFullStressRace(t *testing.T) {
	router := New()

	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/api/v1/item/%d", i)
		_ = router.Handle(http.MethodGet, path, dummyHandler(path))
	}

	const (
		readers = 16
		writers = 4
		ops     = 25
	)

	var wg sync.WaitGroup
	var writerWg sync.WaitGroup
	var stop atomic.Bool

	// Spawn readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var p Params
			for !stop.Load() {
				idx := id % 10
				h, ok := router.Lookup(http.MethodGet, fmt.Sprintf("/api/v1/item/%d", idx), &p)
				if ok && h == nil {
					panic("nil handler returned on valid match")
				}
				runtime.Gosched()
			}
		}(r)
	}

	// Spawn writers executing atomic tree swaps
	for w := 0; w < writers; w++ {
		writerWg.Add(1)
		go func(wid int) {
			defer writerWg.Done()
			for i := 0; i < ops; i++ {
				route := fmt.Sprintf("/api/v2/worker-%d/sub-%d", wid, i)
				_ = router.Handle(http.MethodGet, route, dummyHandler(route))
			}
		}(w)
	}

	// Wait for writers to finish
	writerWg.Wait()
	stop.Store(true)
	wg.Wait()
}
