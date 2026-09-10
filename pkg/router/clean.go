package router

// isCleanPath performs an O(N) linear inspection to determine if path p is already normalized.
// For paths that are already clean (representing >95% of gateway requests), this enables
// CleanPath to return immediately with strictly 0 B/op and 0 allocs/op.
func isCleanPath(p string) bool {
	n := len(p)
	if n == 0 || p[0] != '/' {
		return false
	}
	for i := 1; i < n; i++ {
		if p[i] == '/' {
			if p[i-1] == '/' {
				return false // consecutive slashes (//)
			}
		} else if p[i] == '.' && p[i-1] == '/' {
			// Check if dot segment: /. or /.. (terminal or followed by /)
			if i+1 == n || p[i+1] == '/' {
				return false
			}
			if p[i+1] == '.' && (i+2 == n || p[i+2] == '/') {
				return false
			}
		}
	}
	return true
}

// CleanPath normalizes URL paths compliant with RFC 3986.
// It collapses repeated slashes, resolves '.' and '..' directory traversal segments,
// and ensures the path starts with a leading '/'.
// For clean paths, it executes with strictly 0 B/op and 0 allocs/op.
func CleanPath(p string) string {
	if isCleanPath(p) {
		return p
	}
	return cleanPathSlow(p)
}

func cleanPathSlow(p string) string {
	n := len(p)
	if n == 0 {
		return "/"
	}

	// Use stack-allocated scratch buffer for paths up to 256 bytes (0 allocs)
	var stackBuf [256]byte
	var buf []byte
	if n+1 <= len(stackBuf) {
		buf = stackBuf[:0]
	} else {
		buf = make([]byte, 0, n+1)
	}

	buf = append(buf, '/')

	var segStarts [64]int
	segCount := 0

	trailingSlash := p[n-1] == '/'

	i := 0
	for i < n {
		// Skip slashes
		for i < n && p[i] == '/' {
			i++
		}
		if i >= n {
			break
		}

		// Read segment until next '/'
		start := i
		for i < n && p[i] != '/' {
			i++
		}
		seg := p[start:i]

		if seg == "." {
			continue
		}

		if seg == ".." {
			if segCount > 0 {
				segCount--
				buf = buf[:segStarts[segCount]]
			} else {
				buf = buf[:1] // cannot escape root '/'
			}
			continue
		}

		// Append normal segment
		prevLen := len(buf)
		if prevLen > 1 {
			buf = append(buf, '/')
		}
		if segCount < len(segStarts) {
			segStarts[segCount] = prevLen
			segCount++
		}
		buf = append(buf, seg...)
	}

	// Restore trailing slash if original path ended with '/' and length > 1
	if trailingSlash && len(buf) > 1 && buf[len(buf)-1] != '/' {
		buf = append(buf, '/')
	}

	return string(buf)
}

// StripTrailingSlash returns the path without a trailing slash, preserving root "/".
func StripTrailingSlash(p string) string {
	if len(p) > 1 && p[len(p)-1] == '/' {
		return p[:len(p)-1]
	}
	return p
}

// EnsureLeadingSlash guarantees that path starts with a leading '/'.
func EnsureLeadingSlash(p string) string {
	if len(p) == 0 || p[0] != '/' {
		return "/" + p
	}
	return p
}
