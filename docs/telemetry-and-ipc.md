# NexusGate Telemetry & IPC Architecture

## 1. Overview & Core Philosophy

NexusGate's telemetry subsystem is engineered specifically for high-throughput edge proxies running on battery-constrained Android ARM64 hardware inside Termux.

Traditional observability systems incur significant performance penalties through:
1. Heap allocations on every request logging event.
2. Heavy mutex lock contention across multi-core architectures.
3. Continuous CPU wakeups from periodic timer ticks draining mobile battery.
4. Unbounded memory growth from high-volume logging queues.
5. Inefficient JSON-over-TCP sockets for local inter-process communication.

NexusGate eliminates all five bottlenecks via:
- **Lock-Free MPSC Ring Buffer:** 64-byte cache-line padded slots preventing false sharing and split-access penalties on ARM64 processors.
- **Fixed-Memory Sliding HDR Histogram:** 4-tier logarithmic binning (4,071 pre-allocated bins across 3 generational windows) computing microsecond-accurate percentiles (P50, P90, P99, P99.9) with strictly **0 B/op** and **0 allocs/op**.
- **Tickless Passive Decay & Idle Detector:** Mathematical time-slice decay computing moving RPS without background timers. Traffic lapses (>3s at 0 RPS) place telemetry workers into a low-power dormant state (`sync.Cond`), waking up in <100μs upon traffic arrival.
- **Compact 64-Byte Binary IPC Frame:** A fixed-length LittleEndian wire format with magic bytes `0x4E, 0x47` and IEEE CRC32 checksums streamed over restrictive Unix Domain Sockets (`0600` permissions) to observatory clients.
- **Non-Blocking Fan-Out Dispatcher:** Dedicated per-subscriber channels with automatic drop mechanisms preventing slow or stalled TUI clients from creating backpressure on the reverse proxy.

---

## 2. 32-Byte MetricEvent & 64-Byte Ring Slot Wire Layout

To achieve zero memory allocation and prevent CPU cache thrashing on ARM64 big.LITTLE architectures, `MetricEvent` is strictly packed to 32 bytes with natural memory alignment:

```
+-----------------------------------------------------------------------+
| Offset (Bytes) | Field Name  | Type    | Description                  |
+-----------------------------------------------------------------------+
| 00 - 07 (8B)   | Timestamp   | int64   | Unix epoch nanoseconds       |
| 08 - 15 (8B)   | LatencyNs   | int64   | Request latency nanoseconds  |
| 16 - 19 (4B)   | BytesIn     | uint32  | Payload bytes received       |
| 20 - 23 (4B)   | BytesOut    | uint32  | Payload bytes transmitted    |
| 24 - 27 (4B)   | RouteID     | uint32  | FNV-1a route hash            |
| 28 - 29 (2B)   | StatusCode  | uint16  | HTTP response status code    |
| 30 - 31 (2B)   | Flags       | uint16  | Event bitmask flags          |
+-----------------------------------------------------------------------+
Total: Strictly 32 bytes (0 compiler padding bytes).
```

Each ring buffer slot (`ringSlot`) embeds one `MetricEvent` and is explicitly padded to exactly 64 bytes (the standard L1/L2 cache line size on ARM64 Cortex-A cores):
- `seq: atomic.Uint64` (8 bytes, offset 0..7)
- `event: MetricEvent` (32 bytes, offset 8..39)
- `_pad: [24]byte` (24 bytes, offset 40..63)

This layout completely eliminates false sharing between concurrent producer goroutines accessing adjacent array indices.

---

## 3. Fixed-Memory Sliding HDR Latency Histogram

Rather than storing individual latency measurements, NexusGate bins request durations into a 4-tier fixed-size histogram totaling 4,071 bins:
- **Tier 1 (0 to 999 μs):** 1,000 bins, 1 μs resolution.
- **Tier 2 (1 ms to 99.9 ms):** 990 bins, 100 μs resolution.
- **Tier 3 (100 ms to 999 ms):** 900 bins, 1 ms resolution.
- **Tier 4 (1 s to 60 s):** 1,180 bins, 50 ms resolution.
- **Overflow (> 60 s):** 1 bin.

Three generational windows (10 seconds each) provide a rolling 30-second sliding window. Percentiles (P50, P90, P99, P99.9, Min, Max, Mean) are computed via single-pass cumulative rank scanning in <20μs with zero heap allocations.

---

## 4. Tickless Passive Decay & Battery-Conscious Idle Detection

Android aggressively throttles applications that continuously wake up the CPU via background timers or tickers. NexusGate employs two complementary strategies:

1. **Passive Mathematical Rate Decay:** Moving 1s, 10s, and 60s RPS rates are computed relative to `time.Now().Unix()`. Buckets whose timestamps fall outside the moving window are excluded mathematically without requiring active ticker goroutines to purge them.
2. **Tickless Idle Detector:** When 0 requests have been recorded for >3 seconds, background telemetry dispatching enters a dormant state using `sync.Cond.Wait()`. Incoming requests immediately signal the condition variable, waking up workers in <100μs without polling loops.

---

## 5. Unix Domain Socket (UDS) Server & 64-Byte Binary IPC Frame

Gateway telemetry is streamed locally over a Unix Domain Socket located by default at:
`$PREFIX/tmp/nexusgate.sock` (or `/data/data/com.termux/files/usr/tmp/nexusgate.sock` in Termux).

### Security & Permission Enforcements
- Restrictive file permissions (`0600`) ensure only the owner user ID can read or write to the socket.
- Parent directory permissions are validated for `0700` compliance.
- Stale socket files from terminated processes are automatically detected and unlinked on startup.

### 64-Byte Binary Frame Layout (LittleEndian)
```
+-----------------------------------------------------------------------+
| Offset (Bytes) | Field Name          | Type    | Description          |
+-----------------------------------------------------------------------+
| 00 - 03 (4B)   | Magic Header        | uint32  | 0x4D54474E (0x4E,0x47)|
| 04 - 05 (2B)   | Protocol Version    | uint16  | Version = 1 (LE)     |
| 06 - 07 (2B)   | Frame Type          | uint16  | 1=Snapshot, 2=Ping   |
| 08 - 15 (8B)   | TimestampUnixNano   | int64   | Epoch nanoseconds    |
| 16 - 23 (8B)   | TotalRequests       | uint64  | Cumulative requests  |
| 24 - 27 (4B)   | ActiveConns         | uint32  | Active connections   |
| 28 - 31 (4B)   | RPS1s               | uint32  | Milli-RPS (RPS*1000) |
| 32 - 35 (4B)   | P50LatencyUs        | uint32  | P50 latency in μs    |
| 36 - 39 (4B)   | P90LatencyUs        | uint32  | P90 latency in μs    |
| 40 - 43 (4B)   | P99LatencyUs        | uint32  | P99 latency in μs    |
| 44 - 47 (4B)   | Status2xxCount      | uint32  | 2xx responses (60s)  |
| 48 - 51 (4B)   | Status3xxCount      | uint32  | 3xx responses (60s)  |
| 52 - 55 (4B)   | Status4xxCount      | uint32  | 4xx responses (60s)  |
| 56 - 59 (4B)   | Status5xxCount      | uint32  | 5xx responses (60s)  |
| 60 - 63 (4B)   | CRC32 Checksum      | uint32  | IEEE CRC32 of [0..59]|
+-----------------------------------------------------------------------+
Total: Exactly 64 bytes (LittleEndian for native ARM64 instruction efficiency).
```

---

## 6. Micro-Benchmark Performance Summary (ARM64)

Benchmarked on Android ARM64 running Linux userland:

| Operation | Throughput / Overhead | Allocations |
| :--- | :---: | :---: |
| `RingBuffer.Push` (Sequential) | **38.05 ns/op** | **0 B/op, 0 allocs/op** |
| `RingBuffer.Push` (8x Parallel) | **184.2 ns/op** | **0 B/op, 0 allocs/op** |
| `Histogram.Record` (Sequential) | **181.2 ns/op** | **0 B/op, 0 allocs/op** |
| `Histogram.Record` (8x Parallel) | **321.8 ns/op** | **0 B/op, 0 allocs/op** |
| `Histogram.Percentiles` (Scan 4k bins) | **18.07 μs/op** | **0 B/op, 0 allocs/op** |
| `BinaryFrame.Encode/Decode` | **38.88 ns/op** | **0 B/op, 0 allocs/op** |
| `ThroughputAggregator.Record` (8x Parallel) | **272.2 ns/op** | **0 B/op, 0 allocs/op** |
| `SnapshotProvider.RecordEvent` (8x Parallel) | **384.6 ns/op** | **0 B/op, 0 allocs/op** |
