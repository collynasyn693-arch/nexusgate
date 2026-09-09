package router

import (
	"strings"
)

// longestCommonPrefix returns the length of the common prefix between strings a and b.
func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	i := 0
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

// insertStaticPrefix inserts a static path prefix into the subtree rooted at n,
// performing edge splitting if necessary, and returns the destination node for that prefix.
func (n *node) insertStaticPrefix(path string) (*node, error) {
	if len(path) == 0 {
		return n, nil
	}

	curr := n
	curr.priority++

	// Initialize empty root node
	if curr.path == "" && len(curr.children) == 0 && curr.paramChild == nil && curr.catchChild == nil && curr.handler == nil {
		curr.path = path
		curr.nType = NodeStatic
		return curr, nil
	}

walk:
	for {
		lcp := longestCommonPrefix(path, curr.path)

		// Case 1: Split existing edge
		if lcp < len(curr.path) {
			splitChild := &node{
				path:       curr.path[lcp:],
				indices:    curr.indices,
				children:   curr.children,
				paramChild: curr.paramChild,
				catchChild: curr.catchChild,
				handler:    curr.handler,
				paramKey:   curr.paramKey,
				nType:      curr.nType,
				priority:   curr.priority - 1,
			}

			curr.path = curr.path[:lcp]
			curr.indices = string(splitChild.path[0])
			curr.children = []*node{splitChild}
			curr.paramChild = nil
			curr.catchChild = nil
			curr.handler = nil
			curr.paramKey = ""
			curr.nType = NodeStatic

			if lcp == len(path) {
				return curr, nil
			}

			// Add new sibling branch
			remainder := path[lcp:]
			newChild := newNode(remainder, NodeStatic)
			newChild.priority = 1

			curr.indices += string(remainder[0])
			curr.children = append(curr.children, newChild)
			curr.incrementChildPriority(len(curr.children) - 1)
			return newChild, nil
		}

		// Case 2: Common prefix covers curr.path completely
		path = path[lcp:]
		if len(path) == 0 {
			return curr, nil
		}

		c := path[0]
		idx := curr.findStaticChildIndex(c)
		if idx >= 0 {
			idx = curr.incrementChildPriority(idx)
			curr = curr.children[idx]
			continue walk
		}

		// Allocate new static child
		child := newNode(path, NodeStatic)
		child.priority = 1
		curr.indices += string(c)
		curr.children = append(curr.children, child)
		curr.incrementChildPriority(len(curr.children) - 1)
		return child, nil
	}
}

// AddRoute parses and inserts a route (static or parameterized) into the radix trie.
func (n *node) AddRoute(path string, handler Handler) error {
	if len(path) == 0 || path[0] != '/' {
		return ErrInvalidPath
	}
	if handler == nil {
		return ErrNilHandler
	}

	curr := n
	remaining := path

	for len(remaining) > 0 {
		// Look for next dynamic token
		colonIdx := strings.IndexByte(remaining, ':')
		starIdx := strings.IndexByte(remaining, '*')

		tokenIdx := -1
		isParam := false
		isCatchAll := false

		if colonIdx >= 0 && (starIdx < 0 || colonIdx < starIdx) {
			tokenIdx = colonIdx
			isParam = true
		} else if starIdx >= 0 && (colonIdx < 0 || starIdx < colonIdx) {
			tokenIdx = starIdx
			isCatchAll = true
		}

		if tokenIdx < 0 {
			// Pure static path segment to the end
			dest, err := curr.insertStaticPrefix(remaining)
			if err != nil {
				return err
			}
			if dest.handler != nil {
				return ErrDuplicateRoute
			}
			dest.handler = handler
			return nil
		}

		// Insert static prefix before token if present
		if tokenIdx > 0 {
			staticPart := remaining[:tokenIdx]
			dest, err := curr.insertStaticPrefix(staticPart)
			if err != nil {
				return err
			}
			curr = dest
			remaining = remaining[tokenIdx:]
		}

		if isParam {
			// Parameter segment: parse up to next '/' or end of path
			end := strings.IndexByte(remaining, '/')
			var paramSegment string
			if end < 0 {
				paramSegment = remaining
				remaining = ""
			} else {
				paramSegment = remaining[:end]
				remaining = remaining[end:]
			}

			paramName := paramSegment[1:] // strip ':'
			if len(paramName) == 0 || strings.ContainsAny(paramName, ":*") {
				return ErrEmptyWildcardName
			}

			if curr.paramChild != nil {
				if curr.paramChild.paramKey != paramName {
					return ErrParamConflict
				}
			} else {
				curr.paramChild = newNode("", NodeParam)
				curr.paramChild.paramKey = paramName
			}

			curr = curr.paramChild
			curr.priority++

			if len(remaining) == 0 {
				if curr.handler != nil {
					return ErrDuplicateRoute
				}
				curr.handler = handler
				return nil
			}
		} else if isCatchAll {
			// Catch-all segment: must be terminal
			if slashIdx := strings.IndexByte(remaining, '/'); slashIdx >= 0 {
				return ErrInvalidCatchAll
			}
			catchAllName := remaining[1:] // strip '*'
			if len(catchAllName) == 0 || strings.ContainsAny(catchAllName, "/*:") {
				return ErrEmptyWildcardName
			}

			if curr.catchChild != nil {
				if curr.catchChild.paramKey != catchAllName {
					return ErrParamConflict
				}
			} else {
				curr.catchChild = newNode("", NodeCatchAll)
				curr.catchChild.paramKey = catchAllName
			}

			curr = curr.catchChild
			curr.priority++

			if curr.handler != nil {
				return ErrDuplicateRoute
			}
			curr.handler = handler
			return nil
		}
	}

	return nil
}
