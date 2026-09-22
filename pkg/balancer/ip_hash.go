package balancer

import (
	"context"
	"net/http"
	"sync/atomic"
)

const (
	fnvOffsetBasis = 14695981039346656037
	fnvPrime       = 1099511628211
)

// ipHashBalancer implements deterministic, sticky upstream routing based on client IP addresses.
// It applies a 64-bit FNV-1a hash with a SplitMix64 avalanche mixer to eliminate bit-parity
// clustering across local subnets, guaranteeing consistent session affinity.
type ipHashBalancer struct {
	targets atomic.Pointer[[]*Backend]
}

// NewIPHash constructs a new IP-Hash consistent session balancer.
func NewIPHash(targets []*Backend) Balancer {
	b := &ipHashBalancer{}
	b.UpdateTargets(targets)
	return b
}

// Name returns the balancing algorithm identifier.
func (b *ipHashBalancer) Name() string {
	return string(AlgorithmIPHash)
}

// UpdateTargets updates the candidate set of healthy backends atomically.
func (b *ipHashBalancer) UpdateTargets(targets []*Backend) {
	filtered := make([]*Backend, 0, len(targets))
	for _, t := range targets {
		if t != nil && t.IsHealthy() && !t.IsDraining() {
			filtered = append(filtered, t)
		}
	}
	b.targets.Store(&filtered)
}

// hashClientIP performs an inlined, zero-allocation 64-bit FNV-1a hash with
// a SplitMix64 avalanche finalizer to ensure uniform dispersion across small target sets.
func hashClientIP(addr string) uint64 {
	if len(addr) == 0 {
		return 0
	}

	ipStr := addr
	// Handle IPv6 bracket enclosure: [2001:db8::1]:8080 or [2001:db8::1]
	if addr[0] == '[' {
		for i := 1; i < len(addr); i++ {
			if addr[i] == ']' {
				ipStr = addr[1:i]
				break
			}
		}
	} else {
		// IPv4: scan from right for port colon
		lastColon := -1
		for i := len(addr) - 1; i >= 0; i-- {
			if addr[i] == ':' {
				lastColon = i
				break
			}
		}
		if lastColon != -1 {
			ipStr = addr[:lastColon]
		}
	}

	// 64-bit FNV-1a hash
	var h uint64 = fnvOffsetBasis
	for i := 0; i < len(ipStr); i++ {
		h ^= uint64(ipStr[i])
		h *= fnvPrime
	}

	// SplitMix64 avalanche mixer to break low-bit subnet correlation
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31

	return h
}

// extractIP returns the client IP address string from HTTP headers or RemoteAddr without allocations.
func extractIP(req *http.Request) string {
	if req == nil {
		return ""
	}
	if len(req.Header) == 0 {
		return req.RemoteAddr
	}
	// Direct map access with pre-canonicalized keys avoids CanonicalMIMEHeaderKey heap escapes
	if xff := req.Header["X-Forwarded-For"]; len(xff) > 0 && len(xff[0]) > 0 {
		val := xff[0]
		for i := 0; i < len(val); i++ {
			if val[i] == ',' {
				return val[:i]
			}
		}
		return val
	}
	if xri := req.Header["X-Real-Ip"]; len(xri) > 0 && len(xri[0]) > 0 {
		return xri[0]
	}
	return req.RemoteAddr
}

// Select routes requests deterministically using the client IP hash modulo healthy backends.
func (b *ipHashBalancer) Select(ctx context.Context, req *http.Request) (*Backend, error) {
	ptr := b.targets.Load()
	if ptr == nil {
		return nil, ErrNoHealthyBackends
	}
	targets := *ptr
	n := len(targets)
	if n == 0 {
		return nil, ErrNoHealthyBackends
	}
	if n == 1 {
		return targets[0], nil
	}

	ip := extractIP(req)
	var hashVal uint64
	if len(ip) > 0 {
		hashVal = hashClientIP(ip)
	}

	idx := int(hashVal % uint64(n))
	return targets[idx], nil
}
