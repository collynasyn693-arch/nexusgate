package router

import (
	"context"
	"errors"
	"net/http"
)

// NodeType represents the structural classification of a Radix Trie node.
type NodeType uint8

const (
	// NodeStatic represents a static string prefix.
	NodeStatic NodeType = iota
	// NodeParam represents a dynamic named parameter segment (:param).
	NodeParam
	// NodeCatchAll represents a wildcard catch-all tail segment (*filepath).
	NodeCatchAll
)

// String returns the human-readable name of the node type.
func (t NodeType) String() string {
	switch t {
	case NodeStatic:
		return "static"
	case NodeParam:
		return "param"
	case NodeCatchAll:
		return "catchAll"
	default:
		return "unknown"
	}
}

// Param represents a single key-value URL parameter extracted from a path.
type Param struct {
	Key   string
	Value string
}

// Params is a slice of Param key-value pairs.
type Params []Param

// Get returns the parameter value for the specified key and a boolean indicating if it was found.
func (ps Params) Get(name string) (string, bool) {
	for i := range ps {
		if ps[i].Key == name {
			return ps[i].Value, true
		}
	}
	return "", false
}

// ByName returns the parameter value for the specified key, or an empty string if not found.
func (ps Params) ByName(name string) string {
	val, _ := ps.Get(name)
	return val
}

// Handler defines the request handler interface for NexusGate routing,
// passing pre-extracted zero-allocation path parameters directly.
type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, p Params)
}

// HandlerFunc is an adapter type allowing standard functions to satisfy Handler.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, p Params)

// ServeHTTP calls f(w, r, p).
func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request, p Params) {
	f(w, r, p)
}

// WrapHTTPHandler wraps a standard library http.Handler into a router.Handler.
func WrapHTTPHandler(h http.Handler) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		h.ServeHTTP(w, r)
	}
}

// Route represents the metadata of a registered route.
type Route struct {
	Method  string
	Path    string
	Handler Handler
}

// Router is the public interface for the NexusGate routing engine.
type Router interface {
	http.Handler

	// Handle registers a new request handler for the given method and path.
	Handle(method, path string, handler Handler) error

	// HandleFunc registers a handler function for the given method and path.
	HandleFunc(method, path string, handler HandlerFunc) error

	// Lookup searches the trie for a route matching the method and path,
	// populating the provided params slice without heap allocations.
	Lookup(method, path string, params *Params) (Handler, bool)
}

// Common routing errors.
var (
	ErrNotFound          = errors.New("route not found")
	ErrMethodNotAllowed  = errors.New("method not allowed")
	ErrDuplicateRoute    = errors.New("duplicate route registration")
	ErrParamConflict     = errors.New("parameter name conflicts with existing route at the same depth")
	ErrInvalidCatchAll   = errors.New("catch-all wildcard must be the terminal path segment")
	ErrEmptyWildcardName = errors.New("wildcard or parameter name cannot be empty")
	ErrNilHandler        = errors.New("handler cannot be nil")
	ErrInvalidMethod     = errors.New("unsupported or invalid HTTP method")
	ErrInvalidPath       = errors.New("path must begin with '/'")
)

type paramsKey struct{}

// WithParams attaches Params to the given request context.
func WithParams(ctx context.Context, p Params) context.Context {
	return context.WithValue(ctx, paramsKey{}, p)
}

// ParamsFromContext extracts Params from the request context.
func ParamsFromContext(ctx context.Context) Params {
	if p, ok := ctx.Value(paramsKey{}).(Params); ok {
		return p
	}
	return nil
}
