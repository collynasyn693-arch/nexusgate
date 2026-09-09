package router

// backtrackFrame stores traversal state for zero-allocation iterative backtracking.
type backtrackFrame struct {
	n        *node
	path     string
	paramLen int
	altType  NodeType // NodeParam or NodeCatchAll
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
		paramLen := 0
		if params != nil {
			paramLen = len(*params)
		}

		// Search static children via byte lookup table
		staticIdx := curr.findStaticChildIndex(c)
		if staticIdx >= 0 {
			// Push alternative dynamic frames before descending into static child
			// Push CatchAll first (lower priority), then Param (higher priority)
			if curr.catchChild != nil && stackIdx < len(stack) {
				stack[stackIdx] = backtrackFrame{
					n:        curr,
					path:     searchPath,
					paramLen: paramLen,
					altType:  NodeCatchAll,
				}
				stackIdx++
			}
			if curr.paramChild != nil && stackIdx < len(stack) {
				stack[stackIdx] = backtrackFrame{
					n:        curr,
					path:     searchPath,
					paramLen: paramLen,
					altType:  NodeParam,
				}
				stackIdx++
			}

			curr = curr.children[staticIdx]
			continue walk
		}

		// If no static match, try paramChild
		if curr.paramChild != nil {
			end := 0
			for end < len(searchPath) && searchPath[end] != '/' {
				end++
			}
			if end > 0 {
				// Push CatchAll as alternative if param branch fails later
				if curr.catchChild != nil && stackIdx < len(stack) {
					stack[stackIdx] = backtrackFrame{
						n:        curr,
						path:     searchPath,
						paramLen: paramLen,
						altType:  NodeCatchAll,
					}
					stackIdx++
				}

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

		// Try catchChild as lowest priority
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

		switch frame.altType {
		case NodeParam:
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
		case NodeCatchAll:
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
	}

	return nil, false
}
