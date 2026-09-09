package router

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

// insertStatic inserts a static route into the radix subtree rooted at n.
func (n *node) insertStatic(path string, handler Handler) error {
	if len(path) == 0 || path[0] != '/' {
		return ErrInvalidPath
	}
	if handler == nil {
		return ErrNilHandler
	}

	curr := n
	curr.priority++

	// Empty root tree initialization
	if curr.path == "" && len(curr.children) == 0 && curr.paramChild == nil && curr.catchChild == nil && curr.handler == nil {
		curr.path = path
		curr.handler = handler
		curr.nType = NodeStatic
		return nil
	}

walk:
	for {
		lcp := longestCommonPrefix(path, curr.path)

		// Case 1: Split existing node edge (lcp < len(curr.path))
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

			// Reset current node to the common prefix
			curr.path = curr.path[:lcp]
			curr.indices = string(splitChild.path[0])
			curr.children = []*node{splitChild}
			curr.paramChild = nil
			curr.catchChild = nil
			curr.handler = nil
			curr.paramKey = ""
			curr.nType = NodeStatic

			if lcp == len(path) {
				// The inserted path matches the split prefix exactly
				curr.handler = handler
				return nil
			}

			// Insert remainder of new path as a sibling child
			remainder := path[lcp:]
			newChild := newNode(remainder, NodeStatic)
			newChild.handler = handler
			newChild.priority = 1

			curr.indices += string(remainder[0])
			curr.children = append(curr.children, newChild)
			curr.incrementChildPriority(len(curr.children) - 1)
			return nil
		}

		// Case 2: Common prefix covers curr.path completely
		path = path[lcp:]

		if len(path) == 0 {
			// Path terminates at this node
			if curr.handler != nil {
				return ErrDuplicateRoute
			}
			curr.handler = handler
			return nil
		}

		// Search for existing static child matching next byte
		c := path[0]
		idx := curr.findStaticChildIndex(c)
		if idx >= 0 {
			idx = curr.incrementChildPriority(idx)
			curr = curr.children[idx]
			continue walk
		}

		// No static child matches next byte: allocate a new child
		child := newNode(path, NodeStatic)
		child.handler = handler
		child.priority = 1

		curr.indices += string(c)
		curr.children = append(curr.children, child)
		curr.incrementChildPriority(len(curr.children) - 1)
		return nil
	}
}

// AddRoute adds a route to the radix tree.
func (n *node) AddRoute(path string, handler Handler) error {
	return n.insertStatic(path, handler)
}
