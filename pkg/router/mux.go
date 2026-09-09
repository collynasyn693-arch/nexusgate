package router

import (
	"net/http"
	"strings"
	"sync"
)

const (
	methodGET = iota
	methodPOST
	methodPUT
	methodDELETE
	methodPATCH
	methodHEAD
	methodOPTIONS
	methodCONNECT
	methodTRACE
	methodCount = 9
)

var methodNames = [methodCount]string{
	"GET",
	"POST",
	"PUT",
	"DELETE",
	"PATCH",
	"HEAD",
	"OPTIONS",
	"CONNECT",
	"TRACE",
}

// methodIndex maps HTTP method strings to contiguous array indices via fast-switch.
func methodIndex(method string) int {
	switch method {
	case http.MethodGet:
		return methodGET
	case http.MethodPost:
		return methodPOST
	case http.MethodPut:
		return methodPUT
	case http.MethodDelete:
		return methodDELETE
	case http.MethodPatch:
		return methodPATCH
	case http.MethodHead:
		return methodHEAD
	case http.MethodOptions:
		return methodOPTIONS
	case http.MethodConnect:
		return methodCONNECT
	case http.MethodTrace:
		return methodTRACE
	default:
		return -1
	}
}

// paramHolder encapsulates a pre-allocated Params slice to prevent interface boundary heap escapes.
type paramHolder struct {
	params Params
}

var paramsPool = sync.Pool{
	New: func() any {
		return &paramHolder{
			params: make(Params, 0, 8),
		}
	},
}

// Mux is the HTTP method-based routing multiplexer maintaining independent radix trees per method.
type Mux struct {
	trees                  [methodCount]*node
	HandleMethodNotAllowed bool
	HandleOPTIONS          bool
	NotFound               http.Handler
	MethodNotAllowed       http.Handler
}

// NewMux allocates and initializes an empty Mux with standard default settings.
func NewMux() *Mux {
	return &Mux{
		HandleMethodNotAllowed: true,
		HandleOPTIONS:          true,
	}
}

// Handle registers a request handler for the given method and path.
func (m *Mux) Handle(method, path string, handler Handler) error {
	idx := methodIndex(method)
	if idx < 0 {
		return ErrInvalidMethod
	}
	if handler == nil {
		return ErrNilHandler
	}

	if m.trees[idx] == nil {
		m.trees[idx] = newNode("", NodeStatic)
	}

	return m.trees[idx].AddRoute(path, handler)
}

// HandleFunc registers a handler function for the given method and path.
func (m *Mux) HandleFunc(method, path string, handler HandlerFunc) error {
	return m.Handle(method, path, handler)
}

// Lookup queries the trie for the specified method and path, extracting parameters in-place.
func (m *Mux) Lookup(method, path string, params *Params) (Handler, bool) {
	idx := methodIndex(method)
	if idx < 0 || m.trees[idx] == nil {
		return nil, false
	}
	return m.trees[idx].Lookup(path, params)
}

// allowed traverses all other method trees to discover which methods are allowed for the given path.
func (m *Mux) allowed(path string, reqMethod string) string {
	var allowedMethods []string
	var p Params

	for i := 0; i < methodCount; i++ {
		methodName := methodNames[i]
		if methodName == reqMethod || m.trees[i] == nil {
			continue
		}

		p = p[:0]
		if _, ok := m.trees[i].Lookup(path, &p); ok {
			allowedMethods = append(allowedMethods, methodName)
		}
	}

	if len(allowedMethods) > 0 {
		// Include OPTIONS in Allow header if HandleOPTIONS is enabled
		if m.HandleOPTIONS {
			hasOptions := false
			for _, mName := range allowedMethods {
				if mName == http.MethodOptions {
					hasOptions = true
					break
				}
			}
			if !hasOptions {
				allowedMethods = append(allowedMethods, http.MethodOptions)
			}
		}
		return strings.Join(allowedMethods, ", ")
	}

	return ""
}

// ServeHTTP dispatches inbound HTTP requests to matched handlers with zero heap allocations on the hot path.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	idx := methodIndex(r.Method)

	if idx >= 0 && m.trees[idx] != nil {
		holder := paramsPool.Get().(*paramHolder)
		holder.params = holder.params[:0]

		h, found := m.trees[idx].Lookup(path, &holder.params)
		if found {
			h.ServeHTTP(w, r, holder.params)
			if cap(holder.params) <= 32 {
				paramsPool.Put(holder)
			}
			return
		}
		paramsPool.Put(holder)
	}

	// Automatic OPTIONS handling
	if r.Method == http.MethodOptions && m.HandleOPTIONS {
		if allow := m.allowed(path, http.MethodOptions); len(allow) > 0 {
			w.Header().Set("Allow", allow)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// 405 Method Not Allowed handling
	if m.HandleMethodNotAllowed {
		if allow := m.allowed(path, r.Method); len(allow) > 0 {
			w.Header().Set("Allow", allow)
			if m.MethodNotAllowed != nil {
				m.MethodNotAllowed.ServeHTTP(w, r)
			} else {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			}
			return
		}
	}

	// 404 Not Found handling
	if m.NotFound != nil {
		m.NotFound.ServeHTTP(w, r)
	} else {
		http.NotFound(w, r)
	}
}

// clone creates an exhaustive, deep structural copy of the entire multiplexer.
func (m *Mux) clone() *Mux {
	cp := &Mux{
		HandleMethodNotAllowed: m.HandleMethodNotAllowed,
		HandleOPTIONS:          m.HandleOPTIONS,
		NotFound:               m.NotFound,
		MethodNotAllowed:       m.MethodNotAllowed,
	}
	for i := 0; i < methodCount; i++ {
		if m.trees[i] != nil {
			cp.trees[i] = m.trees[i].clone()
		}
	}
	return cp
}
