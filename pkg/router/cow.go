package router

import (
	"net/http"
	"sync"
	"sync/atomic"
)

// COWRouter is a thread-safe, lock-free routing engine utilizing Copy-On-Write (COW)
// state management via atomic.Pointer. Readers execute concurrently with zero locks,
// while mutations clone the trie hierarchy, validate new routes, and atomically swap
// the active routing table.
type COWRouter struct {
	active atomic.Pointer[Mux]
	mu     sync.Mutex

	// Configuration settings propagated to cloned multiplexers
	handleMethodNotAllowed bool
	handleOPTIONS          bool
	notFound               http.Handler
	methodNotAllowed       http.Handler
}

// RouterOption configures a COWRouter.
type RouterOption func(*COWRouter)

// WithMethodNotAllowed enables or disables RFC 7231 405 Method Not Allowed handling.
func WithMethodNotAllowed(enabled bool) RouterOption {
	return func(r *COWRouter) {
		r.handleMethodNotAllowed = enabled
	}
}

// WithAutomaticOPTIONS enables or disables automatic 204 No Content responses for OPTIONS requests.
func WithAutomaticOPTIONS(enabled bool) RouterOption {
	return func(r *COWRouter) {
		r.handleOPTIONS = enabled
	}
}

// WithNotFound sets a custom 404 handler.
func WithNotFound(h http.Handler) RouterOption {
	return func(r *COWRouter) {
		r.notFound = h
	}
}

// WithCustomMethodNotAllowed sets a custom 405 handler.
func WithCustomMethodNotAllowed(h http.Handler) RouterOption {
	return func(r *COWRouter) {
		r.methodNotAllowed = h
	}
}

// New creates and initializes a new Copy-On-Write COWRouter.
func New(opts ...RouterOption) *COWRouter {
	r := &COWRouter{
		handleMethodNotAllowed: true,
		handleOPTIONS:          true,
	}
	for _, opt := range opts {
		opt(r)
	}

	initialMux := NewMux()
	initialMux.HandleMethodNotAllowed = r.handleMethodNotAllowed
	initialMux.HandleOPTIONS = r.handleOPTIONS
	initialMux.NotFound = r.notFound
	initialMux.MethodNotAllowed = r.methodNotAllowed

	r.active.Store(initialMux)
	return r
}

// Tx represents an isolated transaction for batch route mutations during dynamic reloads.
type Tx struct {
	mux *Mux
}

// Handle registers a route inside the transaction.
func (tx *Tx) Handle(method, path string, handler Handler) error {
	return tx.mux.Handle(method, path, handler)
}

// HandleFunc registers a handler function inside the transaction.
func (tx *Tx) HandleFunc(method, path string, handler HandlerFunc) error {
	return tx.mux.HandleFunc(method, path, handler)
}

// Update executes a transactional batch update against a cloned routing table.
// If fn returns an error, the cloned table is discarded and the active table remains unmodified.
// If fn succeeds, the cloned table is atomically swapped into active service.
func (r *COWRouter) Update(fn func(tx *Tx) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldMux := r.active.Load()
	newMux := oldMux.clone()

	tx := &Tx{mux: newMux}
	if err := fn(tx); err != nil {
		return err
	}

	r.active.Store(newMux)
	return nil
}

// Handle registers a new request handler for the specified method and path,
// safely updating the routing table via Copy-On-Write.
func (r *COWRouter) Handle(method, path string, handler Handler) error {
	return r.Update(func(tx *Tx) error {
		return tx.Handle(method, path, handler)
	})
}

// HandleFunc registers a handler function for the specified method and path.
func (r *COWRouter) HandleFunc(method, path string, handler HandlerFunc) error {
	return r.Handle(method, path, handler)
}

// Lookup queries the active routing table without acquiring any mutexes.
// It executes with strictly 0 B/op and 0 allocs/op on the hot path.
func (r *COWRouter) Lookup(method, path string, params *Params) (Handler, bool) {
	mux := r.active.Load()
	return mux.Lookup(method, path, params)
}

// ServeHTTP dispatches requests to the active routing table with zero lock contention.
func (r *COWRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	mux := r.active.Load()
	mux.ServeHTTP(w, req)
}
