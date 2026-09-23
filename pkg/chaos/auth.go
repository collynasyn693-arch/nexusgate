package chaos

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
)

// Sentinel security errors.
var (
	ErrChaosDisabled       = errors.New("nexusgate: chaos injection engine is disabled")
	ErrInvalidSubnetCIDR   = errors.New("nexusgate: invalid allowed subnet CIDR")
	ErrNoSecurityGuardsSet = errors.New("nexusgate: chaos is enabled but neither admin_key nor allowed_subnets is configured")
)

// SecurityGuard validates whether inbound requests are authorized to trigger chaos injection.
type SecurityGuard struct {
	enabled        bool
	adminKey       string
	headerKey      string
	canonicalKey   string
	allowedSubnets []*net.IPNet
	strictMode     bool
}

// NewSecurityGuard initializes and compiles CIDR blocks for rapid subnet evaluation.
func NewSecurityGuard(cfg Config) (*SecurityGuard, error) {
	headerKey := cfg.HeaderKey
	if headerKey == "" {
		headerKey = HeaderChaosKey
	}

	sg := &SecurityGuard{
		enabled:      cfg.Enabled,
		adminKey:     cfg.AdminKey,
		headerKey:    headerKey,
		canonicalKey: http.CanonicalHeaderKey(headerKey),
		strictMode:   cfg.StrictMode,
	}

	if len(cfg.AllowedSubnets) > 0 {
		sg.allowedSubnets = make([]*net.IPNet, 0, len(cfg.AllowedSubnets))
		for _, s := range cfg.AllowedSubnets {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !strings.Contains(s, "/") {
				ip := net.ParseIP(s)
				if ip == nil {
					return nil, ErrInvalidSubnetCIDR
				}
				if ip.To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
			_, ipNet, err := net.ParseCIDR(s)
			if err != nil {
				return nil, ErrInvalidSubnetCIDR
			}
			sg.allowedSubnets = append(sg.allowedSubnets, ipNet)
		}
	}

	return sg, nil
}

// Authorize evaluates security tokens and IP subnets to determine if chaos can be invoked.
func (sg *SecurityGuard) Authorize(r *http.Request) bool {
	if !sg.enabled {
		return false
	}

	// Fail closed if no security guard is configured
	if sg.adminKey == "" && len(sg.allowedSubnets) == 0 {
		return false
	}

	hasKeyCheck := sg.adminKey != ""
	hasSubnetCheck := len(sg.allowedSubnets) > 0

	keyValid := false
	if hasKeyCheck {
		var provided string
		if r.Header != nil {
			if vals := r.Header[sg.canonicalKey]; len(vals) > 0 {
				provided = vals[0]
			} else if vals := r.Header[sg.headerKey]; len(vals) > 0 {
				provided = vals[0]
			}
		}

		if provided != "" && len(provided) == len(sg.adminKey) {
			if constantTimeCompareString(provided, sg.adminKey) == 1 {
				keyValid = true
			}
		}
	}

	subnetValid := false
	if hasSubnetCheck {
		clientIP := ExtractRemoteIP(r)
		subnetValid = sg.IsIPAllowed(clientIP)
	}

	if hasKeyCheck && hasSubnetCheck {
		return keyValid && subnetValid
	}
	if hasKeyCheck {
		return keyValid
	}
	return subnetValid
}

// constantTimeCompareString performs constant-time string comparison without heap allocations.
func constantTimeCompareString(x, y string) int {
	if len(x) != len(y) {
		return 0
	}
	var v byte
	for i := 0; i < len(x); i++ {
		v |= x[i] ^ y[i]
	}
	return subtle.ConstantTimeByteEq(v, 0)
}

// IsIPAllowed tests if the provided IP string falls within any allowed CIDR subnets.
func (sg *SecurityGuard) IsIPAllowed(ipStr string) bool {
	if len(sg.allowedSubnets) == 0 {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, subnet := range sg.allowedSubnets {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

// ExtractRemoteIP cleanly extracts client IP from r.RemoteAddr, stripping ports and IPv6 brackets.
func ExtractRemoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "127.0.0.1"
	}

	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return cleanIP(host)
	}

	return cleanIP(addr)
}

func cleanIP(ip string) string {
	ip = strings.TrimPrefix(ip, "[")
	ip = strings.TrimSuffix(ip, "]")
	return ip
}
