package proxy

import (
	"net"
	"net/http"
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

// isProtectedHeader reports whether a header cannot be stripped by dynamic Connection tokens.
func isProtectedHeader(token string) bool {
	if len(token) >= 12 && strings.EqualFold(token[:12], "x-forwarded-") {
		return true
	}
	for _, p := range [...]string{
		"host", "content-length", "content-type", "authorization",
		"range", "if-range", "date", "x-forwarded-for", "x-forwarded-proto",
		"x-forwarded-host", "x-real-ip",
	} {
		if strings.EqualFold(token, p) {
			return true
		}
	}
	return false
}

// IsHopByHopHeader reports whether the given header key is a standard hop-by-hop header.
func IsHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[strings.ToLower(key)]
	return ok
}

// standardHopByHopKeys contains pre-computed canonical MIME keys for RFC hop-by-hop headers,
// eliminating repeated string allocations during per-request header sanitization.
var standardHopByHopKeys = [...]string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// RemoveHopByHopHeaders inspects the given HTTP headers and strips:
//  1. All dynamic hop-by-hop headers declared as tokens in the 'Connection' header (RFC 7230 §6.1),
//     except for protected framing/auth headers.
//  2. Standard hop-by-hop headers (Connection, Keep-Alive, Proxy-Authenticate, etc.).
//  3. The 'TE' header is stripped unless its value is exactly 'trailers' (RFC 7230 §4.3 gRPC compatibility).
func RemoveHopByHopHeaders(h http.Header) {
	if h == nil {
		return
	}

	// 1. Parse and delete dynamic headers declared in the Connection header without heap allocations.
	if connVals, ok := h["Connection"]; ok {
		for _, val := range connVals {
			for len(val) > 0 {
				var token string
				comma := strings.IndexByte(val, ',')
				if comma >= 0 {
					token = strings.TrimSpace(val[:comma])
					val = val[comma+1:]
				} else {
					token = strings.TrimSpace(val)
					val = ""
				}
				if token == "" {
					continue
				}
				// Skip standard non-header connection tokens
				if strings.EqualFold(token, "close") || strings.EqualFold(token, "keep-alive") || strings.EqualFold(token, "upgrade") {
					continue
				}
				// Security guard: protect core framing and authentication headers
				if isProtectedHeader(token) {
					continue
				}
				h.Del(token)
			}
		}
	}

	// 2. Preserve 'TE: trailers' if requested; otherwise strip TE
	hasTrailers := false
	teVals, hasTe := h["Te"]
	if !hasTe {
		teVals, hasTe = h["TE"]
	}
	if hasTe {
		for _, val := range teVals {
			if strings.EqualFold(strings.TrimSpace(val), "trailers") {
				hasTrailers = true
				break
			}
		}
	}

	// 3. Strip all standard hop-by-hop headers using zero-alloc direct map deletion
	for _, key := range standardHopByHopKeys {
		delete(h, key)
	}

	// Restore TE: trailers if it was present
	if hasTrailers {
		h["Te"] = []string{"trailers"}
	}
}

// ExtractClientIP extracts the client IP address from r.RemoteAddr.
// It cleanly strips port numbers for both IPv4 ("1.2.3.4:5678") and IPv6 ("[::1]:5678"),
// strips bracket enclosures, and handles bare IP strings or Unix socket paths without ports.
func ExtractClientIP(r *http.Request) string {
	if r == nil || r.RemoteAddr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Fallback for bare IPs without ports, unix domain sockets, or test addresses
		host = r.RemoteAddr
	}

	// Strip IPv6 enclosing brackets if present
	host = strings.Trim(host, "[]")
	return host
}

// MutateForwardedHeaders computes and attaches standard reverse proxy forwarding headers:
// - X-Forwarded-For: appends client IP to any existing comma-separated list
// - X-Real-IP: canonical client IP (overwriting untrusted client values)
// - X-Forwarded-Proto: "https" if TLS is active or upstream indicates HTTPS, else "http"
// - X-Forwarded-Host: client's original Host header
func MutateForwardedHeaders(req *http.Request) {
	if req == nil {
		return
	}

	clientIP := ExtractClientIP(req)

	// 1. X-Forwarded-For
	if clientIP != "" {
		if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
			req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			req.Header.Set("X-Forwarded-For", clientIP)
		}
		// 2. X-Real-IP
		req.Header.Set("X-Real-IP", clientIP)
	}

	// 3. X-Forwarded-Proto
	proto := "http"
	if req.TLS != nil || strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
		proto = "https"
	}
	req.Header.Set("X-Forwarded-Proto", proto)

	// 4. X-Forwarded-Host
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	if host != "" {
		req.Header.Set("X-Forwarded-Host", host)
	}
}
