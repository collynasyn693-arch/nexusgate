# NexusGate Radix Trie Router Specification & Internals

## 1. Architectural Overview

The **NexusGate Radix Trie Router** (`pkg/router`) is an ultra-low-latency, zero-allocation HTTP request router engineered specifically for Android Termux ARM64. It utilizes a compressed prefix tree (Radix Tree) with dedicated trees per HTTP method, enabling sub-microsecond path lookups with **strictly 0 B/op and 0 allocs/op** on the steady-state hot path.

```
                                [ Method Multiplexer ]
                                          |
                +-------------------------+-------------------------+
                |                         |                         |
            GET Tree                  POST Tree                 PUT Tree
                |
             "/" (Root)
                |
         +------+------+
         |             |
      "api/v1/"     "static/"
         |             |
     +---+---+     "*filepath" (Catch-All)
     |       |
 "users"  "users/"
             |
          ":id" (Param)
             |
         "/profile" (Static Suffix)
```

---

## 2. Trie Node Taxonomy

Each `node` in the tree represents a compressed path fragment:

- **Static Node (`NodeStatic`):** Exact byte prefix match. Child nodes are indexed via an `indices` string for $O(1)$ child branch selection.
- **Param Node (`NodeParam`):** Single path segment wildcard indicated by `:name` (e.g., `:id`). Matches any non-slash byte sequence up to the next `/` or end of path.
- **Catch-All Node (`NodeCatchAll`):** Trailing multi-segment wildcard indicated by `*name` (e.g., `*filepath`). Captures all remaining path segments including slashes.

---

## 3. Precedence & Conflict Resolution

When multiple route patterns could match an incoming URI, the router enforces deterministic priority:

$$\text{Static Exact Match} > \text{Named Parameter } (:param) > \text{Catch-All Wildcard } (*filepath)$$

### Traversal Backtracking:
If a static child branch matches the current prefix but subsequent path segments fail, the traversal engine falls back to an alternative parameter or catch-all branch using a fixed stack (`[8]backtrackFrame`) allocated entirely on the goroutine stack, completely avoiding heap escapes.

---

## 4. Zero-Allocation Guarantees

On the request routing hot path, zero memory is allocated on the heap:
1. **Stack-Allocated Backtrack Frames:** Up to 8 levels of tree depth are tracked via an inline array on the goroutine stack.
2. **In-Place Param Extraction:** URL parameter keys and values are extracted as sub-slices of the original request URL string.
3. **Pooled Parameter Containers:** Parameter slice holders (`sync.Pool`) are recycled across requests, eliminating slice header allocations.
4. **Method Indexing:** Methods are mapped via a fast bitmask / integer switch avoiding string allocations.

---

## 5. Copy-On-Write (COW) Concurrency Model

NexusGate routes can be dynamically registered or hot-reloaded without stopping active network traffic:
- **`COWRouter`:** Maintains an `atomic.Pointer[Mux]`.
- **Lock-Free Reads:** Querying threads traverse the active `Mux` snapshot without acquiring read locks, eliminating thread contention on ARM big.LITTLE architectures.
- **Transactional Mutations (`Update`):** Route updates clone the current tree hierarchy, apply modifications to the clone, and execute a single atomic pointer swap (`Swap`). If an error occurs during route construction, the transaction rolls back with zero impact on active readers.

---

## 6. Benchmark Verification (Android ARM64)

```
goos: android
goarch: arm64
pkg: nexusgate/pkg/router
BenchmarkStaticExactLookup-4          6749725       195.2 ns/op        0 B/op        0 allocs/op
BenchmarkStaticExactParallel-4       19269675        82.59 ns/op       0 B/op        0 allocs/op
BenchmarkParamSingleLookup-4          6491864       233.4 ns/op        0 B/op        0 allocs/op
BenchmarkParamMultiLookup-4           3027212       382.3 ns/op        0 B/op        0 allocs/op
BenchmarkCatchAllLookup-4             7512213       169.7 ns/op        0 B/op        0 allocs/op
BenchmarkMuxServeHTTP_ZeroAlloc-4     3826130       328.7 ns/op        0 B/op        0 allocs/op
```

- **Parallel Static Lookup:** **82.59 ns/op**
- **Heap Allocation:** **0 B/op across all benchmarks**
- **Allocs:** **0 allocs/op across all benchmarks**
