# Changelog

All notable changes to NexusGate are documented in this file in adherence with [Keep a Changelog](https://keepachangelog.com/) and [Conventional Commits](https://www.conventionalcommits.org/).

---

## [1.0.0] - 2026-09-05

NexusGate v1.0.0 is the foundational production release: a high-performance, battery-conscious, zero-daemon edge reverse proxy and real-time ANSI terminal observatory written in pure Go (1.22+) specifically engineered for unprivileged Linux ARM64 userland environments (Android Termux).

### Summary of Stages (200 Atomic Commits)

#### Stage 01: Core Architecture, Repository Foundation & Non-Root Userland Guard (Commits 001–020)
- Initialized Go 1.22+ module with zero third-party dependencies.
- Implemented Termux environment detector, POSIX path resolver, and unprivileged userland validation.
- Implemented secure directory isolation (0700) and UDS socket permission enforcement (0600).
- Configured conventional commit CI and commit lint automation.

#### Stage 02: Configuration Engine & Dynamic Schema Validation (Commits 021–040)
- Implemented dual-format YAML and JSON configuration parser with line/column diagnostic tracking.
- Implemented duration normalization ("5s", "100ms") and unprivileged port constraints ($\ge 1024$).
- Implemented atomic lock-free configuration holder (`COW` pointer swapping).
- Added inotify/stat file watcher with debouncing and semantic diff computation.

#### Stage 03: Zero-Allocation Radix Trie Routing Multiplexer (Commits 041–060)
- Implemented 9-method array-indexed routing multiplexer.
- Implemented radix tree with iterative stack-allocated backtracking (`[8]backtrackFrame`).
- Added parameter and wildcard matching with recycled `Params` slice pools (strictly 0 allocs/op).
- Added automatic HTTP OPTIONS, 405 Method Not Allowed, and RFC 3986 path normalization.

#### Stage 04: Zero-Daemon Streaming Reverse Proxy Engine (Commits 061–080)
- Implemented dual-tier buffer recycling engine (4KB header, 32KB streaming chunks).
- Implemented RFC 7230 hop-by-hop header removal and security guards.
- Implemented real-time Server-Sent Events (SSE) flushing with `TimedFlusher`.
- Implemented full-duplex RFC 6455 WebSocket proxying with connection hijacking.

#### Stage 05: Resilient Dynamic Upstream Balancer & Canary Pools (Commits 081–100)
- Implemented lock-free Smooth Weighted Round-Robin (SWWR) balancer.
- Implemented Power-of-Two-Choices (P2C) with Peak-EWMA latency weighting.
- Implemented consistent IP hashing with murmur3 hashing.
- Implemented graceful backend connection draining with timeout cancellation.

#### Stage 06: Distributed Circuit Breaker & Developer Chaos Engine (Commits 101–120)
- Implemented three-state circuit breaker (Closed, Open, Half-Open) with canary concurrency gating.
- Implemented sliding error window and consecutive failure tracking.
- Implemented developer Chaos Engine with sub-microsecond latency injection and fault simulation.
- Protected chaos endpoints with constant-time admin token and subnet authentication.

#### Stage 07: High-Throughput In-Memory Telemetry Subsystem (Commits 121–140)
- Designed cache-aligned 32-byte metric event structures (`MetricEvent`).
- Implemented lock-free power-of-two ring buffer.
- Implemented HDR-style latency histogram and rolling throughput rate counters.
- Implemented tickless mobile battery conservation (`IdleDetector`) with $<1\text{ ms}$ idle CPU utilization.
- Implemented IPC broadcast server streaming binary delta telemetry over Unix Domain Sockets.

#### Stage 08: Live ANSI Terminal Observatory (TUI Dashboard) (Commits 141–160)
- Implemented differential double-buffered screen rendering engine emitting minimal ANSI escape sequences.
- Implemented terminal dimension auto-detection and SIGWINCH resize handling.
- Built gateway header widget with battery, memory, and RPS gauges.
- Built backend matrix widget, log viewer widget, and horizontal Unicode sparklines.
- Implemented raw mode keyboard dispatcher with adaptive 30 FPS rendering loop.

#### Stage 09: CLI, Lifecycle Management & End-to-End Integration (Commits 161–180)
- Implemented unified CLI command tree: `start`, `run`, `attach`, `validate`, `reload`, `drain`, `version`, `help`.
- Implemented zero-loss graceful server shutdown sequence.
- Implemented end-to-end integration test suite verifying balancing, circuit breaking, chaos injection, and WebSockets.
- Documented full CLI reference manual.

#### Stage 10: Production Hardening, Performance Optimization & Release Engineering (Commits 181–200)
- Implemented mock upstream cluster fixture with variable latency and load generator harness.
- Verified 0 allocs/op across hot routing, balancer selection, and header filtering decision paths.
- Verified absence of memory leaks and goroutine leaks over high-concurrency workloads.
- Verified tickless mobile battery conservation with near-zero idle CPU wakeups.
- Implemented native Termux ARM64 static compilation scripts and one-line installer.
- Published architectural blueprint, Termux tuning guide, production examples, and release ledger.
