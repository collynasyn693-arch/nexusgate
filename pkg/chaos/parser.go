package chaos

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ParseQueryParams extracts and validates chaos parameters from a raw query string.
// If rawQuery does not contain the chaos marker "__", it returns immediately with 0 allocations.
func ParseQueryParams(rawQuery string, maxDelay time.Duration, maxBodyBytes int) (ChaosParams, bool) {
	if len(rawQuery) == 0 || !strings.Contains(rawQuery, "__") {
		return ChaosParams{}, false
	}

	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxBodyBytes
	}

	var (
		params   ChaosParams
		hasChaos bool
	)

	remaining := rawQuery
	for len(remaining) > 0 {
		var pair string
		idx := strings.IndexAny(remaining, "&;")
		if idx >= 0 {
			pair = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			pair = remaining
			remaining = ""
		}

		if len(pair) == 0 {
			continue
		}

		var key, val string
		eqIdx := strings.IndexByte(pair, '=')
		if eqIdx >= 0 {
			key = pair[:eqIdx]
			val = pair[eqIdx+1:]
		} else {
			key = pair
			val = ""
		}

		// Fast check: only process keys starting with "__"
		if !strings.HasPrefix(key, "__") {
			continue
		}

		if strings.Contains(key, "%") {
			if unescaped, err := url.QueryUnescape(key); err == nil {
				key = unescaped
			}
		}
		if strings.Contains(val, "%") {
			if unescaped, err := url.QueryUnescape(val); err == nil {
				val = unescaped
			}
		}

		switch key {
		case QueryParamDelay:
			if d, err := time.ParseDuration(val); err == nil {
				if d < 0 {
					d = 0
				} else if maxDelay > 0 && d > maxDelay {
					d = maxDelay
				}
				params.Delay = d
				hasChaos = true
			}

		case QueryParamStatus:
			if code, err := strconv.Atoi(val); err == nil {
				if code >= 100 && code <= 599 {
					params.Status = code
					hasChaos = true
				}
			}

		case QueryParamBody:
			if len(val) > maxBodyBytes {
				val = val[:maxBodyBytes]
			}
			params.Body = val
			hasChaos = true

		case QueryParamDrop:
			v := strings.ToLower(val)
			if v == "true" || v == "1" || v == "yes" || len(val) == 0 {
				params.Drop = true
				hasChaos = true
			}

		case QueryParamFaultRate:
			if rate, err := strconv.ParseFloat(val, 64); err == nil {
				if !math.IsNaN(rate) && !math.IsInf(rate, 0) {
					if rate > 1.0 && rate <= 100.0 {
						rate = rate / 100.0
					}
					if rate < 0.0 {
						rate = 0.0
					} else if rate > 1.0 {
						rate = 1.0
					}
					params.FaultRate = rate
					hasChaos = true
				}
			}
		}
	}

	params.HasChaos = hasChaos
	return params, hasChaos
}

// StripChaosParams strips all query parameters starting with "__" from rawQuery
// without allocating maps or using url.Query().
func StripChaosParams(rawQuery string) string {
	if len(rawQuery) == 0 || !strings.Contains(rawQuery, "__") {
		return rawQuery
	}

	var b strings.Builder
	b.Grow(len(rawQuery))

	remaining := rawQuery
	first := true

	for len(remaining) > 0 {
		var pair string
		idx := strings.IndexAny(remaining, "&;")
		if idx >= 0 {
			pair = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			pair = remaining
			remaining = ""
		}

		if len(pair) == 0 {
			continue
		}

		key := pair
		if eqIdx := strings.IndexByte(pair, '='); eqIdx >= 0 {
			key = pair[:eqIdx]
		}

		if strings.HasPrefix(key, "__") {
			continue
		}

		if !first {
			b.WriteByte('&')
		}
		b.WriteString(pair)
		first = false
	}

	return b.String()
}

// SanitizeRequestURL returns a sanitized copy of u with all chaos query parameters removed.
// If no chaos parameters are present, it returns u unchanged.
func SanitizeRequestURL(u *url.URL) *url.URL {
	if u == nil || len(u.RawQuery) == 0 || !strings.Contains(u.RawQuery, "__") {
		return u
	}
	cpy := *u
	cpy.RawQuery = StripChaosParams(u.RawQuery)
	return &cpy
}
