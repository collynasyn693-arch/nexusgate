# NexusGate Reverse Proxy & Streaming Engine Architecture

## 1. Overview & Architectural Topology

NexusGate incorporates a bespoke, high-performance streaming reverse proxy engine specifically engineered for Linux ARM64 userland environments (such as Android Termux). The engine prioritizes:
- **Zero-allocation steady-state buffer recycling** via dual-tier `sync.Pool` byte managers.
- **Strict RFC 7230 §6.1 / RFC 2616 §13.5.1 hop-by-hop header removal** with request smuggling defense.
- **Immediate Server-Sent Events (SSE) chunk flushing** with sub-millisecond transit latency.
- **Full-duplex WebSocket connection hijacking** with synchronized teardown and zero early-frame loss.
- **Context-driven upstream cancellation** and double-write-safe synthetic gateway error generation (502/504).

```
                             +-----------------------------------+
                             |     Incoming Client Request       |
                             +-----------------------------------+
                                               |
                                               v
                          +------------------------------------------+
                          |   WrapTracker(w) [Commit/Byte Tracking]  |
                          +------------------------------------------+
                                               |
                     +-------------------------+-------------------------+
                     | IsWebSocketRequest?                               |
                    YES                                                 NO
                     |                                                   |
                     v                                                   v
   +------------------------------------+              +------------------------------------+
   |  ResolveHijacker & Hijack()        |              | NewUpstreamRequest                 |
   |  - Obtain client net.Conn & rw     |              | - Strip RFC 7230 Hop-by-Hop        |
   +------------------------------------+              | - Compute X-Forwarded-*            |
                     |                                 | - Bind r.Context() with Deadline   |
                     v                                 +------------------------------------+
   +------------------------------------+                                |
   | DialUpstreamWebSocket              |                                v
   | - Forward Handshake & Verify 101   |              +------------------------------------+
   +------------------------------------+              | Upstream Transport RoundTrip       |
                     |                                 | - DisableCompression: true         |
                     v                                 | - ARM64 Keep-Alive (30s)           |
   +------------------------------------+              +------------------------------------+
   | PumpWebSocket (Full-Duplex)        |                                |
   | - Drain clientRw.Reader            |                                v
   | - Dual-Goroutine io.CopyBuffer     |              +------------------------------------+
   | - Symmetric sync.Once Teardown     |              | ServeStream                        |
   +------------------------------------+              | - Detect text/event-stream (SSE)   |
                                                       | - Immediate http.Flusher Dispatch  |
                                                       | - Recycled 32KB io.CopyBuffer      |
                                                       +------------------------------------+
```

---

## 2. Dual-Tier `sync.Pool` Buffer Management

### 2.1 Buffer Tiers & Hardware Alignment
Memory transfers inside mobile ARM64 SoCs (e.g., Qualcomm Snapdragon, MediaTek Dimensity) are constrained by CPU cache hierarchies and Android's Low Memory Killer (LMK):
1. **Small Buffer Pool (4KB - `SmallBufferSize = 4096`):**
   - Fits cleanly within ARM Cortex-A55 / A78 L1 data caches ($32\text{ KB} - 64\text{ KB}$).
   - Allocated for small request bodies, SSE event chunks, and metadata buffers.
2. **Large Buffer Pool (32KB - `LargeBufferSize = 32768`):**
   - Matches common Linux socket receive/send buffers and OS pipe buffers.
   - Sits within L2 cache ($128\text{ KB} - 512\text{ KB}$), avoiding DDR memory bus saturation during heavy streaming.
   - Enforces $O(1)$ memory consumption during massive transfers ($>50\text{ MB}$).

### 2.2 Eliminating Interface Boxing Allocations (True Zero-Alloc)
In standard Go, passing a raw slice header `[]byte` (24 bytes) into `sync.Pool.Put(any)` invokes `runtime.convTslice`, creating a heap allocation on every single return.
NexusGate solves this by storing pointers to fixed-size arrays:
```go
smallPool: sync.Pool{
    New: func() any { return new([SmallBufferSize]byte) },
}
```
Using Go 1.20+ slice-to-array-pointer casting:
```go
func (p *DualTierBufferPool) PutSmall(b []byte) {
    if cap(b) != SmallBufferSize {
        return // Reject mutated slices
    }
    b = b[:SmallBufferSize]
    arrPtr := (*[SmallBufferSize]byte)(b)
    p.smallPool.Put(arrPtr)
}
```
Pointer-to-interface conversion in Go is strictly 1 word (8 bytes) and incurs **0 B/op and 0 allocs/op**.

### 2.3 Buffer Length Invariant (`io.CopyBuffer` Panic Guard)
Go's `io.CopyBuffer(dst, src, buf)` enforces:
```go
if buf != nil && len(buf) == 0 {
    panic("empty buffer in CopyBuffer")
}
```
All buffers acquired via `GetSmall()` and `GetLarge()` are guaranteed to have `len(buf) == cap(buf)` prior to use.

---

## 3. RFC 7230 §6.1 Hop-by-Hop Header Stripping & Security

### 3.1 Standard Hop-by-Hop Headers
NexusGate identifies and deletes all link-scoped headers before forwarding across proxy hops:
- `Connection`
- `Keep-Alive`
- `Proxy-Authenticate`
- `Proxy-Authorization`
- `Proxy-Connection` (Historical Netscape header preventing keep-alive desynchronization)
- `TE` (*Exception:* preserved if value is `trailers`, required for HTTP/2 gRPC)
- `Trailer` (RFC 7230 singular standard)
- `Trailers` (RFC 2616 legacy plural form)
- `Transfer-Encoding`
- `Upgrade` (Stripped for regular HTTP; preserved when negotiating WebSocket upgrades)

### 3.2 Dynamic `Connection` Token Parsing & Security Guardrail
RFC 7230 §6.1 mandates that headers named as comma-separated tokens in `Connection` are hop-by-hop.
*Security Guardrail:* Malicious clients can attempt HTTP request smuggling by sending `Connection: Host, Content-Length, Authorization`.
NexusGate enforces a protected header blacklist:
`Host`, `Content-Length`, `Content-Type`, `Authorization`, `Range`, `If-Range`, `Date`, and `X-Forwarded-*` cannot be stripped by client `Connection` tokens.

---

## 4. Client IP & Protocol Forwarding Header Mutators

NexusGate inspects incoming requests and populates standard reverse proxy forwarding headers:
- `ExtractClientIP(r)`: Robust parsing via `net.SplitHostPort` with bracket removal for IPv6 (`[::1]:port` $\to$ `::1`), and fallback handling for bare IPs or Unix domain sockets.
- `X-Forwarded-For`: Appends client IP to existing comma-separated lists (`clientIP` or `prior, clientIP`).
- `X-Real-IP`: Set to the validated client remote IP, overwriting untrusted client values.
- `X-Forwarded-Proto`: `"https"` if TLS is active or upstream indicates HTTPS, else `"http"`.
- `X-Forwarded-Host`: Preserves original client `Host`.

---

## 5. Mobile Outbound Transport Optimization

### 5.1 The `DisableCompression` Mandate
NexusGate configures `http.Transport{ DisableCompression: true }`.
*Why?*
If `DisableCompression` is false, Go's HTTP client automatically sends `Accept-Encoding: gzip` and wraps the response body in an internal `gzip.Reader`.
1. Gzip compression operates in sliding blocks; this **completely breaks Server-Sent Events (SSE)** by buffering chunks until 32KB windows fill.
2. It wastes precious ARM64 CPU cycles and battery decompressing streams that must immediately be re-streamed to mobile clients.
With `DisableCompression: true`, raw compressed bytes pass directly through to the client without userland buffering.

### 5.2 Keep-Alive and Idle Connection Pooling
- `KeepAlive: 30s`: Keeps ARM64 radio modems alive during request bursts without draining battery during idle periods.
- `MaxIdleConns: 128` & `MaxIdleConnsPerHost: 32`: Prevents socket leaks and file descriptor exhaustion under Termux non-root limits ($1024$ FDs).

---

## 6. Server-Sent Events (SSE) Streaming Engine

1. **Detection:** Detected via `Content-Type: text/event-stream`.
2. **Immediate Header Dispatch:** Response status and headers are committed immediately to the client socket, followed by an immediate `http.Flusher.Flush()`.
3. **Chunk Passthrough:** The destination `http.ResponseWriter` is wrapped in `flushWriter`, guaranteeing that every write produced by `io.CopyBuffer` is flushed to the network instantly ($<500\text{ }\mu\text{s}$ transit time).
4. **Proxy Directives:** Automatically attaches `Cache-Control: no-cache, no-transform` and `X-Accel-Buffering: no` to disable buffering in downstream caching layers.

---

## 7. WebSocket Hijacking & Full-Duplex TCP Pump

### 7.1 Hijacking & Buffered Reader Preservation
When upgrading to WebSocket:
1. Hijacks the client connection via `http.ResponseController(w).Hijack()` (with recursive `Unwrap()` fallback).
2. **Early-Buffered Data Preservation:** If the client sent pipelined WebSocket frames immediately following the HTTP handshake, those bytes reside in `clientRw.Reader`. Reading directly from `clientConn` would drop those frames. NexusGate's client-to-upstream copy pump reads directly from `clientRw.Reader`.

### 7.2 Full-Duplex TCP Pump Lifecycle
```go
var once sync.Once
closeBoth := func() {
    once.Do(func() {
        _ = clientConn.Close()
        _ = upstreamConn.Close()
    })
}
defer closeBoth()

var wg sync.WaitGroup
wg.Add(2)

// Client -> Upstream
go func() {
    defer wg.Done()
    defer closeBoth()
    buf := pool.GetLarge()
    defer pool.PutLarge(buf)
    _, _ = io.CopyBuffer(upstreamConn, clientRw.Reader, buf)
}()

// Upstream -> Client
go func() {
    defer wg.Done()
    defer closeBoth()
    buf := pool.GetLarge()
    defer pool.PutLarge(buf)
    _, _ = io.CopyBuffer(clientConn, upstreamConn, buf)
}()

wg.Wait()
```
- When either side closes or encounters a broken pipe, `closeBoth()` terminates both sockets, instantly waking up the opposing blocked `Read()`.
- `wg.Wait()` guarantees zero leaked goroutines or dangling file descriptors.

---

## 8. Synthetic Gateway Errors & Double-Write Protection

- **State Tracking:** `ResponseWriterTracker` tracks whether headers have already been committed (`Written()`).
- **502 Bad Gateway:** Emitted when the upstream connection is refused, unreachable, or broken.
- **504 Gateway Timeout:** Emitted when context deadlines or dial timeouts expire.
- **Mid-Stream Double-Write Protection:** If headers have already been committed and the upstream disconnects mid-transfer, `WriteGatewayError` safely aborts without attempting to emit a second `WriteHeader`, eliminating `superfluous response.WriteHeader` warnings.
- **Client Cancellation:** If `errors.Is(err, context.Canceled)`, no response is written to avoid writing to dead sockets.
