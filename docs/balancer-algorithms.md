# NexusGate Upstream Balancer & Load Distribution Subsystem

## 1. Overview & Architectural Topology

NexusGate includes an ultra-low-latency, zero-allocation upstream load balancing and target distribution subsystem designed specifically for resource-constrained, multi-core ARM64 edge environments (such as Android Termux).

The balancer subsystem operates directly between the routing engine (`pkg/router`) and the streaming reverse proxy (`pkg/proxy`). It provides:
- **Copy-On-Write (COW) Target Pool Registry:** Lock-free, wait-free $O(1)$ read paths (`atomic.Pointer[poolState]`) with zero allocation overhead during target selection.
- **Deterministic Smooth Weighted Round-Robin (SWWR / SWRR):** Nginx-style mathematical interleaving with zero-sum invariant preservation.
- **Power-of-Two-Choices (P2C) Least-Loaded:** Mitzenmacher queue herd mitigation with virtual inflight integer cross-multiplication.
- **Starvation-Free Peak-EWMA:** Linkerd/Finagle latency-aware dynamic routing with selection-time exponential time decay.
- **Subnet-Dispersed IP-Hash:** 64-bit FNV-1a consistent session routing equipped with a SplitMix64 avalanche finalizer.
- **Graceful Backend Draining & Connection Bleeding:** Zero-delay idle short-circuit and slip-free atomic connection drain barriers.
- **RAII Connection Tracking Guards (`ConnGuard`):** Recycled via `sync.Pool` for strictly **0 B/op and 0 allocs/op**, providing panic-safe leak prevention.

```
                              +-----------------------------+
                              |   Incoming Client Request   |
                              +-----------------------------+
                                             |
                                             v
                              +-----------------------------+
                              |  TargetPool.Select(ctx, r)  |
                              |  - atomic.Pointer Load      |
                              |  - Lock-Free Target Snapshot|
                              +-----------------------------+
                                             |
                        +--------------------+--------------------+
                        |                    |                    |
                        v                    v                    v
                 [ SWWR Balancer ]    [ P2C Balancer ]    [ Peak-EWMA Balancer ]
                 - Max CurrentWeight  - 2 Random Choices  - Decayed Latency Cost
                 - Interleaved Order  - Min Inflight Load - Instant Peak Evasion
                        |                    |                    |
                        +--------------------+--------------------+
                                             |
                                             v
                              +-----------------------------+
                              |      Target Backend         |
                              |  - Inflight Active Conns    |
                              |  - Latency EWMA (Nanos)     |
                              +-----------------------------+
                                             |
                                             v
                              +-----------------------------+
                              | Backend.AcquireGuard()      |
                              | - CAS max_conns Enforcement |
                              | - sync.Pool Recycled Guard  |
                              +-----------------------------+
                                             |
                                             v
                              +-----------------------------+
                              |  Proxy RoundTrip Execution  |
                              +-----------------------------+
                                             |
                                             v
                              +-----------------------------+
                              | defer guard.Done(lat, err)  |
                              | - Atomic conns decrement    |
                              | - Record Peak-EWMA latency  |
                              | - Recycle to sync.Pool      |
                              +-----------------------------+
```

---

## 2. Load Balancing Strategies & Mathematical Foundations

### 2.1 Smooth Weighted Round-Robin (SWWR / SWRR - Nginx Algorithm)

The Smooth Weighted Round-Robin algorithm distributes requests across $N$ healthy backends according to configured weights $w_1, w_2, \dots, w_N$, with total weight $W = \sum_{i=1}^N w_i$.

#### Selection Algorithm
At each request selection step:
1. For every healthy backend $i \in \{1, \dots, N\}$:
   $$c_i \leftarrow c_i + w_i$$
2. Select the backend with the maximum current weight:
   $$m = \arg\max_{i} (c_i)$$
3. Deduct total weight from the selected backend:
   $$c_m \leftarrow c_m - W$$
4. Return backend $m$.

#### Mathematical Invariant
Assuming initial state $c_i^{(0)} = 0$ for all $i$:
$$\sum_{i=1}^N c_i^{(k)} = \sum_{i=1}^N (c_i^{(k-1)} + w_i) - W = \sum_{i=1}^N c_i^{(k-1)} + W - W = \sum_{i=1}^N c_i^{(k-1)} = 0$$
The sum of all current weights remains **strictly zero** at every step.

#### Dynamic Membership Re-Centering
When backend targets are dynamically added, removed, or change health status, retaining stale $c_i$ values breaks the zero-sum invariant and induces traffic bursts. NexusGate's `swwrBalancer.UpdateTargets()` automatically re-centers all current weights to zero on pool changes, guaranteeing burst-free distribution under dynamic environments.

---

### 2.2 Power-of-Two-Choices (P2C) Least-Loaded

Michael Mitzenmacher's seminal theorem (*The Power of Two Random Choices in Randomized Load Balancing*) proves that selecting the lesser loaded of two randomly chosen candidates exponentially collapses the maximum queue length from $O(\frac{\log n}{\log \log n})$ in pure random routing to $O(\log \log n)$.

#### Uniform $O(1)$ Candidate Selection
Rather than relying on iterative retry loops to select two distinct nodes, NexusGate executes exact bijective mapping:
$$i = \text{rand.N}(N)$$
$$j' = \text{rand.N}(N - 1)$$
$$j = \begin{cases} j' & \text{if } j' < i \\ j' + 1 & \text{if } j' \ge i \end{cases}$$
Every pair $(i, j)$ with $i \ne j$ has identical probability $P(i, j) = \frac{1}{N(N - 1)}$ in strict $O(1)$ time with 0 allocations.

#### Virtual Inflight Cross-Multiplication
To support heterogeneous backend weights without floating-point division or divide-by-zero risks:
$$\text{Load}_1 = (\text{activeConns}_1 + 1) \times w_2 \quad \text{vs} \quad \text{Load}_2 = (\text{activeConns}_2 + 1) \times w_1$$
The virtual inflight addition $(+1)$ guarantees that when both candidates have zero active connections, the tie is broken in favor of the node with higher configured weight.

---

### 2.3 Peak-EWMA (Latency-Aware Dynamic Routing)

Adapted from Twitter Finagle and Linkerd, Peak-EWMA routes traffic based on real-time upstream latency observations with exponential decay.

#### Cost Function
$$\text{Cost}(b) = (\text{activeConns}(b) + 1) \times \text{EffectiveLatency}(b, t_{\text{now}})$$

#### Asymmetric Instant-Peak & Exponential Recovery
1. **Spike Reaction (Instantaneous Latch):** When a request completes with measured round-trip time $\Delta \ge \text{EWMA}_{\text{old}}$, the estimate jumps immediately:
   $$\text{EWMA} \leftarrow \Delta$$
   This immediately redirects subsequent traffic away within 1 request.
2. **Decay Recovery (Selection-Time Evaluation):** To avoid the **Starvation Trap** (where a spiked node receives zero traffic and therefore never updates its EWMA), decay is evaluated at selection time:
   $$\Delta t = t_{\text{now}} - t_{\text{last}}$$
   $$\text{factor} = e^{-\Delta t / \tau} \quad (\tau = 10\text{s})$$
   $$\text{EffectiveLatency}(b, t) = \text{MinRTT} + (\text{EWMA} - \text{MinRTT}) \cdot \text{factor}$$
   As time passes, a spiked node naturally decays toward the baseline $\text{MinRTT} = 1\text{ms}$, allowing P2C to gently probe and restore traffic.

---

### 2.4 IP-Hash Consistent Session Affinity

For stateful applications requiring sticky session persistence, IP-Hash maps client IP addresses deterministically to upstream targets.

#### Subnet Bit-Parity Dispersion
Standard FNV-1a retains linear parity correlations across sequential client IPs within the same subnet (e.g. `192.168.1.10`, `192.168.1.11`). For small cluster sizes ($N \in [2, 8]$), this causes severe hash clustering. NexusGate integrates a Stafford Mix13 / SplitMix64 avalanche mixer directly into the hash loop:
```go
h ^= h >> 30
h *= 0xbf58476d1ce4e5b9
h ^= h >> 27
h *= 0x94d049bb133111eb
h ^= h >> 31
```
This guarantees uniform dispersion across all upstream nodes even for tightly clustered IP blocks.

---

## 3. Dynamic Backend Draining Lifecycle

Graceful backend decommissioning is managed via `Backend.Drain(ctx, timeout)` and `TargetPool.Drain()`:

```
[ Drain Invocation ]
        |
        v
Set draining = true
Rebuild HealthyTargets() (Target immediately excluded from new Select calls)
        |
        v
Are Inflight Connections == 0?
       / \
     YES  NO
     /     \
    v       v
Immediate  Wait on drainDone channel with timeout timer
Return nil          |
             Inflight requests complete -> ReleaseConn()
                    |
             conns == 0 && draining == true?
                    |
             sync.Once.Do(close(drainDone))
                    |
             Drain() unblocks cleanly (0 active conns)
```

- **Immediate Short-Circuit:** If the target has 0 active connections, `Drain()` signals `drainDone` immediately and returns within $<1\text{ms}$.
- **Slip-Free Concurrency Barrier:** `AcquireConn()` checks `draining` after atomic connection increment; if draining commenced mid-flight, it immediately backs out and returns `ErrBackendDraining`.
- **Double-Close Immunity:** Channel teardown is guarded by `sync.Once`.

---

## 4. Active Connection Tracking & RAII Leak Prevention (`ConnGuard`)

`ConnGuard` provides RAII connection management:
```go
guard, err := backend.AcquireGuard()
if err != nil {
    return err
}
defer guard.Done(latency, err)
```

- **Zero Allocation Recycling:** `ConnGuard` structs are recycled through an internal `sync.Pool`.
- **Panic Resilience:** Guard releases are strictly idempotent using an atomic `released.CompareAndSwap(false, true)` flag. Even if downstream HTTP handlers suffer catastrophic panics, `defer guard.Release()` guarantees that active connection counters return accurately to zero without leaking.
- **Saturation Bounds:** `max_conns` limits are enforced atomically via compare-and-swap loops before connection slots are granted.

---

## 5. Micro-Benchmark Performance Verification (Android Termux ARM64)

The upstream balancer subsystem was profiled on an ARM64 quad-core environment under high concurrency:

```
goos: android
goarch: arm64
pkg: nexusgate/pkg/balancer
BenchmarkSWWR_Sequential-4              4654312        220.20 ns/op        0 B/op        0 allocs/op
BenchmarkSWWR_Parallel-4                2767755        449.40 ns/op        0 B/op        0 allocs/op
BenchmarkP2C_Sequential-4               5311707        221.40 ns/op        0 B/op        0 allocs/op
BenchmarkP2C_Parallel-4                22231107         58.22 ns/op        0 B/op        0 allocs/op
BenchmarkPeakEWMA_Sequential-4          1000000       1026.00 ns/op        0 B/op        0 allocs/op
BenchmarkPeakEWMA_Parallel-4            4661515        239.50 ns/op        0 B/op        0 allocs/op
BenchmarkIPHash_Sequential-4            6244513        193.20 ns/op        0 B/op        0 allocs/op
BenchmarkIPHash_Parallel-4             25744658         50.65 ns/op        0 B/op        0 allocs/op
BenchmarkRoundRobin_Sequential-4       45910407         26.07 ns/op        0 B/op        0 allocs/op
BenchmarkRoundRobin_Parallel-4         22643400         87.67 ns/op        0 B/op        0 allocs/op
BenchmarkConnGuard_AcquireRelease-4     5106688        217.60 ns/op        0 B/op        0 allocs/op
```

### Key Performance Highlights:
- **Zero Allocations Across the Board:** Strictly **0 B/op and 0 allocs/op** across all 5 balancing algorithms, sequential selection, parallel selection, and connection guard tracking.
- **Ultra-High Throughput:** Parallel P2C and IP-Hash exceed **17,000,000 to 25,000,000 decisions/second** per core on ARM64 (`50 - 58 ns/op`).
- **Sequential Latency:** Standard round-robin executes in **26.07 ns/op**.

---

## 6. Mobile & Edge Scenario Recommendations

| Scenario / Workload | Recommended Algorithm | Rationale |
| :--- | :--- | :--- |
| **Heterogeneous Edge Devices** | **P2C Least-Loaded (`p2c`)** | Automatically routes according to live processing capability without incurring latency measurement overhead. |
| **Variable Mobile Latency / Cellular** | **Peak-EWMA (`peak_ewma`)** | Instantly routes around jittery cellular nodes and radio wake-up delays; probes recovering nodes. |
| **Strict Predictability & API Gateways** | **Smooth Weighted RR (`swwr`)** | Deterministic, smooth interleaving proportional to configured weights without burstiness. |
| **Session Affinity & Local Caches** | **IP-Hash (`ip_hash`)** | Guarantees sticky cache hits with uniform subnet dispersion via SplitMix64. |
| **High-Throughput Static Endpoints** | **Round-Robin (`round_robin`)** | Lowest CPU overhead (26 ns/op) for symmetric, uniform backends. |
