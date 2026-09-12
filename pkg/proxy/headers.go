package proxy

import (
	"net/http"
	"net/textproto"
	"strings"
)

// hopByHopHeaders defines standard RFC 7230 §6.1 / RFC 2616 §13.5.1 / RFC 9110 §7.6.1 hop-by-hop headers.
// These headers apply to a single transport link and must not be forwarded across reverse proxy hops.
var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"proxy-connection":    {}, // Non-standard de-facto header widely sent by older clients
	"te":                  {}, // Handled with RFC 7230 §4.3 exception for 'trailers'
	"trailer":             {}, // RFC 7230 standard singular form
	"trailers":            {}, // RFC 2616 legacy plural form
	"transfer-encoding":   {},
	"upgrade":             {},
}

// protectedHeaders defines essential end-to-end framing and routing headers that must NEVER
// be stripped by dynamic tokens in the Connection header to prevent HTTP request smuggling
// and routing bypass vulnerabilities.
var protectedHeaders = map[string]struct{}{
	"host":              {},
	"content-length":    {},
	"content-type":      {},
	"authorization":     {},
	"range":             {},
	"if-range":          {},
	"date":              {},
	"x-forwarded-for":   {},
	"x-forwarded-proto": {},
	"x-forwarded-host":  {},
	"x-real-ip":         {},
}

// IsHopByHopHeader reports whether the given header key is a standard hop-by-hop header.
func IsHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[strings.ToLower(key)]
	return ok
}

// RemoveHopByHopHeaders inspects the given HTTP headers and strips:
// 1. All dynamic hop-by-hop headers declared as tokens in the 'Connection' header (RFC 7230 §6.1),
//    except for protected framing/auth headers.
// 2. Standard hop-by-hop headers (Connection, Keep-Alive, Proxy-Authenticate, etc.).
// 3. The 'TE' header is stripped unless its value is exactly 'trailers' (RFC 7230 §4.3 gRPC compatibility).
func RemoveHopByHopHeaders(h http.Header) {
	if h == nil {
		return
	}

	// 1. Parse and delete dynamic headers declared in the Connection header.
	// A request may have multiple Connection headers or comma-separated tokens.
	if connVals, ok := h["Connection"]; ok {
		for _, val := range connVals {
			for _, token := range strings.Split(val, ",") {
				token = strings.TrimSpace(token)
				if token == "" {
					continue
				}
				lowerToken := strings.ToLower(token)
				// Security guard: protect core framing and authentication headers
				if _, protected := protectedHeaders[lowerToken]; protected {
					continue
				}
				// Use canonical header key for deletion to match Go's http.Header map
				canonical := textproto.CanonicalMIMEHeaderKey(token)
				h.Del(canonical)
			}
		}
	}

	// 2. Preserve 'TE: trailers' if requested; otherwise strip TE
	te := h.Get("TE")
	hasTrailers := strings.EqualFold(strings.TrimSpace(te), "trailers")

	// 3. Strip all standard hop-by-hop headers
	for hop := range hopByHopHeaders {
		canonical := textproto.CanonicalMIMEHeaderKey(hop)
		h.Del(canonical)
	}

	// Restore TE: trailers if it was present
	if hasTrailers {
		h.Set("TE", "trailers")
	}
}
