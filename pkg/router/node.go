package router

// node represents a single compressed prefix node in the Radix Trie.
// Its fields are organized to maximize ARM64 L1 cache-line locality (64 bytes).
type node struct {
	// Hot fields accessed during prefix traversal
	path     string   // prefix segment matched at this node
	indices  string   // 1-byte lookup table mapping 1:1 to static children
	children []*node  // static child nodes sorted by priority
	priority uint32   // number of registered endpoints in this subtree
	nType    NodeType // NodeStatic, NodeParam, NodeCatchAll

	// Dynamic child edges segregated from static children
	paramChild *node // dynamic :param child edge (if any)
	catchChild *node // wildcard *catchAll child edge (if any)

	// Endpoint payload and parameter identification
	handler  Handler // registered request handler, nil for intermediate nodes
	paramKey string  // parameter name (e.g. "id" for :id, "filepath" for *filepath)
}

// newNode allocates an empty radix trie node with the specified prefix path and node type.
func newNode(path string, nType NodeType) *node {
	return &node{
		path:  path,
		nType: nType,
	}
}

// findStaticChild performs an O(1) byte lookup in indices to find the matching static child node.
func (n *node) findStaticChild(c byte) *node {
	for i := 0; i < len(n.indices); i++ {
		if n.indices[i] == c {
			return n.children[i]
		}
	}
	return nil
}

// findStaticChildIndex returns the index of the matching static child, or -1 if not found.
func (n *node) findStaticChildIndex(c byte) int {
	for i := 0; i < len(n.indices); i++ {
		if n.indices[i] == c {
			return i
		}
	}
	return -1
}

// incrementChildPriority increments the priority of children[idx] and bubbles it up
// to maintain descending priority order within children and indices.
func (n *node) incrementChildPriority(idx int) int {
	n.children[idx].priority++
	p := n.children[idx].priority

	newIdx := idx
	for newIdx > 0 && n.children[newIdx-1].priority < p {
		// Swap with predecessor in children slice
		n.children[newIdx-1], n.children[newIdx] = n.children[newIdx], n.children[newIdx-1]

		// Swap corresponding characters in indices
		b := []byte(n.indices)
		b[newIdx-1], b[newIdx] = b[newIdx], b[newIdx-1]
		n.indices = string(b)

		newIdx--
	}
	return newIdx
}

// clone recursively creates an exhaustive, deep structural copy of the radix subtree.
// This guarantees that concurrent readers and writers never observe shared child slices
// during Copy-On-Write (COW) router updates.
func (n *node) clone() *node {
	if n == nil {
		return nil
	}

	cp := &node{
		path:       n.path,
		indices:    n.indices,
		priority:   n.priority,
		nType:      n.nType,
		handler:    n.handler,
		paramKey:   n.paramKey,
		paramChild: n.paramChild.clone(),
		catchChild: n.catchChild.clone(),
	}

	if len(n.children) > 0 {
		cp.children = make([]*node, len(n.children))
		for i, child := range n.children {
			cp.children[i] = child.clone()
		}
	}

	return cp
}
