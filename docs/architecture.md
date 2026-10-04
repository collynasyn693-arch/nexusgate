# NexusGate Architectural Blueprint

NexusGate is a high-performance, battery-conscious, zero-daemon edge reverse proxy and real-time terminal observatory written in pure Go (1.22+) specifically engineered for unprivileged Linux ARM64 userland environments (Android Termux).

---

## 1. Core Architectural Tenets

```
                        +---------------------------------------------+
                        |           Client Network Requests           |
                        +---------------------------------------------+
                                               |
                                               v
+---------------------------------------------------------------------------------------+
| Ingress Engine: netpoll / HTTP Server (Unprivileged Port >= 1024, Non-Root POSIX)    |
+---------------------------------------------------------------------------------------+
                                               |
                                               v
+---------------------------------------------------------------------------------------+
| Supervisor Middleware Pipeline:                                                       |
|  - Active Connection Counter (Atomic)                                                 |
|  - Battery Idle Detector (Tickless park/unpark transitions)                           |
|  - Chaos Injection Engine (Admin Key / Subnet Guarded)                                |
|  - COW Radix Trie Router (Atomic Pointer Swap, 0 allocs/op)                           |
|  - Load Balancer Selector (SWWR, P2C, Peak-EWMA, IP-Hash, 0 allocs/op)                 |
|  - Circuit Breaker Evaluator (Canary Probing, Half-Open Auto-Recovery)                |
|  - Reverse Proxy (Dual-Tier Recycled Buffers, Zero-Decompress Streaming, SSE / WS)   |
|  - Metric Event Ring Buffer Push (Fixed 32-Byte Structs, 0 allocs/op)                 |
+---------------------------------------------------------------------------------------+
        |                                                              |
        v                                                              v
+-----------------------------+                               +-------------------------+
| Upstream Backend Fleet      |                               | IPC Telemetry Server    |
| (Microservices, Dev Servers)|                               | (Unix Domain Socket)    |
+-----------------------------+                               +-------------------------+
                                                                       |
                                                                       v
                                                              +-------------------------+
                                                              | ANSI Terminal Dashboard |
                                                              | (Double-Buffered TUI)   |
                                                              +-------------------------+
```

---

## 2. Low-Level Subsystem Specifications

### A. Zero-Allocation Hot Path Engineering
To operate efficiently on mobile ARM big.LITTLE architectures with constrained memory and battery thermal budgets, NexusGate enforces strict zero heap allocations on all steady-state request decisions:
- **Radix Trie Routing (`pkg/router`):** Iterative stack-allocated backtracking (`[8]backtrackFrame`) and recycled `Params` slice pools guarantee `0 B/op` and `0 allocs/op`.
- **Load Balancing (`pkg/balancer`):** Lock-free atomic round-robin counters and array indexing achieve `0 allocs/op` during backend target selection.
- **Header Sanitization (`pkg/proxy`):** RFC 7230 hop-by-hop header removal utilizes pre-computed canonical arrays (`standardHopByHopKeys`) and index-based comma scanning, completely eliminating dynamic string allocations.
- **Telemetry Buffering (`pkg/telemetry`):** Fixed-size 32-byte cache-aligned telemetry events (`MetricEvent`) are pushed into power-of-two lock-free ring buffers without heap growth.

### B. Dual-Tier Buffer Recycling Engine
Dynamic payload streaming bypasses Go's runtime heap garbage collector via dual-tier `sync.Pool` memory recycling:
- **Small Buffers (4 KB):** Used for handshake parsing, HTTP status lines, and short control frames.
- **Large Buffers (32 KB):** Used for bulk TCP/HTTP streaming, large uploads, Server-Sent Events, and RFC 6455 WebSocket proxying.
- **Buffer Invariant Guard:** Capacity validation strictly rejects under-sized or mismatched slices before returning them to pools, eliminating slice slicing panics.

### C. Tickless Mobile Battery Conservation
Continuous timer interrupts on mobile devices prevent ARM CPU cores from dropping into low-power $C$-states, severely depleting battery life.
- **`IdleDetector` State Machine:** Monitors active request arrivals. When zero traffic is detected for $>3.0\text{ s}$, telemetry collectors park into sleeping states (`IdleStateSleeping`).
- **Zero CPU Wakeups:** In quiet periods, CPU utilization drops to $<0.3\%$, consuming $<1\text{ ms}$ of CPU time over hundreds of milliseconds of quiet wall-clock time.
- **Instantaneous Wakeup:** Incoming network packets trip atomic CAS operations, unblocking sleeping worker goroutines in $<100\,\mu\text{s}$.

### D. Live ANSI Terminal Observatory
- **Differential Screen Rendering:** Double-buffered screen grids compare previous and current frames cell-by-cell, emitting ANSI escape sequences only for modified characters.
- **Adaptive Frame Throttling:** Baseline 30 FPS rendering automatically downscales refresh frequencies during idle conditions or battery saver mode.
- **Zero Third-Party Dependencies:** Pure Go standard library ANSI terminal writer eliminates heavyweight terminal UI packages.

### E. Unprivileged Userland Sandbox
- **Non-Root Port Enforcement:** Fails closed if configured with ports $<1024$ without root credentials.
- **Path Isolation:** Dynamically detects Termux `$PREFIX` and `$HOME` to bind Unix domain sockets with strict `0600` permissions.
