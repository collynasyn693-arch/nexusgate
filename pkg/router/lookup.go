package router

// backtrackFrame stores traversal state for zero-allocation iterative backtracking.
type backtrackFrame struct {
	n        *node
	path     string
	paramLen int
}

// Lookup searches the radix trie rooted at n for a handler matching the given path.
// It executes with zero heap allocations on the hot path by traversing nodes iteratively
// and extracting parameters in-place into the provided params slice.
func (n *node) Lookup(path string, params *Params) (Handler, bool) {
	if n == nil || len(path) == 0 {
		return nil, false
	}

	var (
		searchPath = path
		curr       = n
		stack      [8]backtrackFrame
		stackIdx   = 0
	)

walk:
	for {
		prefixLen := len(curr.path)

		// Prefix mismatch check
		if len(searchPath) < prefixLen || searchPath[:prefixLen] != curr.path {
			break
		}
		searchPath = searchPath[prefixLen:]

		// Entire searchPath matched at this node
		if len(searchPath) == 0 {
			if curr.handler != nil {
				return curr.handler, true
			}
			break
		}

		c := searchPath[0]

		// If current node has alternative dynamic branches, push a backtrack checkpoint
		if (curr.paramChild != nil || curr.catchChild != nil) && stackIdx < len(stack) {
			paramLen := 0
			if params != nil {
				paramLen = len(*params)
			}
			stack[stackIdx] = backtrackFrame{
				n:        curr,
				path:     searchPath,
				paramLen: paramLen,
			}
			stackIdx++
		}

		// Search static children via byte lookup table
		for i := 0; i < len(curr.indices); i++ {
			if curr.indices[i] == c {
				curr = curr.children[i]
				continue walk
			}
		}

		// Dynamic child dispatch (param or catch-all)
		if curr.paramChild != nil {
			end := 0
			for end < len(searchPath) && searchPath[end] != '/' {
				end++
			}
			if end > 0 {
				if params != nil {
					*params = append(*params, Param{
						Key:   curr.paramChild.paramKey,
						Value: searchPath[:end],
					})
				}
				searchPath = searchPath[end:]
				curr = curr.paramChild
				continue walk
			}
		}

		if curr.catchChild != nil {
			if params != nil {
				*params = append(*params, Param{
					Key:   curr.catchChild.paramKey,
					Value: searchPath,
				})
			}
			if curr.catchChild.handler != nil {
				return curr.catchChild.handler, true
			}
		}

		break
	}

	// Backtrack unwinding across stack frames
	for stackIdx > 0 {
		stackIdx--
		frame := stack[stackIdx]
		searchPath = frame.path
		if params != nil && frame.paramLen < len(*params) {
			*params = (*params)[:frame.paramLen]
		}

		// Attempt paramChild first, then catchChild
		if frame.n.paramChild != nil {
			end := 0
			for end < len(searchPath) && searchPath[end] != '/' {
				end++
			}
			if end > 0 {
				if params != nil {
					*params = append(*params, Param{
						Key:   frame.n.paramChild.paramKey,
						Value: searchPath[:end],
					})
				}
				searchPath = searchPath[end:]
				curr = frame.n.paramChild
				goto walk
			}
		}

		if frame.n.catchChild != nil {
			if params != nil {
				*params = append(*params, Param{
					Key:   frame.n.catchChild.paramKey,
					Value: searchPath,
				})
			}
			if frame.n.catchChild.handler != nil {
				return frame.n.catchChild.handler, true
			}
		}
	}

	return nil, false
}
