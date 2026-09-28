# NexusGate Terminal Observatory (TUI) Documentation

## 1. Overview & Mobile ARM64 Architecture

NexusGate includes an embedded, high-performance, real-time Terminal Observatory written in pure Go (zero external CGo or heavy ratatui/ncurses bindings). It is optimized specifically for Android Termux terminals running on ARM64 processors.

### Key Architectural Highlights
1. **Differential Double-Buffered Screen Rendering (`pkg/tui/screen.go`):**
   - Maintains two in-memory grids: `Front` (active terminal display) and `Back` (next frame).
   - Only emits cursor positioning and SGR color escape sequences for cells that have changed.
   - For identical frames (e.g. idle traffic), emits strictly 0 bytes (`0 B/op`, `0 allocs/op`).
   - Eliminates terminal flicker and drastically conserves mobile battery.

2. **Adaptive 30 FPS / Low-Power Sleep Transitions (`pkg/tui/loop.go`):**
   - Renders at 30 FPS (~33ms per frame) during active traffic.
   - Automatically detects idle periods (>3s at 0 RPS) and throttles refresh to 2 FPS or tickless event-driven wakes.

3. **Pure Go Terminal Raw Mode & Panic Restoration Guard (`pkg/tui/restore.go`):**
   - Uses native `syscall.SYS_IOCTL` with `TCGETS` / `TCSETS` and `TIOCGWINSZ`.
   - Traps unexpected panics and signals (`SIGINT`, `SIGTERM`, `SIGHUP`), guaranteeing cursor visibility (`\033[?25h`), alternate screen exit (`\033[?1049l`), and termios reset.

4. **Standalone UDS Client (`pkg/tui/ipc_client.go`):**
   - Connects to `/data/data/com.termux/files/usr/tmp/nexusgate.sock` (or `/tmp/nexusgate.sock`).
   - Consumes 64-byte LittleEndian binary telemetry frames produced by Stage 07.

---

## 2. Terminal Layout Anatomy

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ NEXUSGATE v1.0.0 [ARM64]  [ONLINE]                       UP: 0h 14m 22s     │
│  Conns: 3 | RPS: 45.2 | Req: 12430   Go: 18 | RSS: 14.2MB     BAT: 87%⚡     │
├─────────────────────────────────────────────────────────────────────────────┤
│  THROUGHPUT (RPS):  ▂▃▅▆▇█▇▆▅▃▂     LATENCY P50: ▂ ▃ ▅ ▂                    │
├─────────────────────────────────────────────────────────────────────────────┤
│─[ UPSTREAM BACKEND STATUS & TOPOLOGY ]──────────────────────────────────────│
│ ROUTE        TARGET                     WT   STATUS     INFLT    LATENCY   RPS  │
│ /api/v1      http://127.0.0.1:8081      5    HEALTHY    1        1.20ms    35.0 │
│ /api/v1      http://127.0.0.1:8082      3    HEALTHY    0        0.85ms    10.2 │
│ /auth        http://127.0.0.1:9000      1    DEGRADED   2        14.50ms   0.0  │
├─────────────────────────────────────────────────────────────────────────────┤
│─[ LIVE ACCESS & EVENT STREAM ]──────────────────────────────────────────────│
│ 14:22:01 GET    [200] 1.15ms   127.0.0.1       /api/v1/users                │
│ 14:22:02 POST   [201] 2.40ms   127.0.0.1       /api/v1/checkout             │
│ 14:22:03 GET    [304] 0.45ms   192.168.1.15    /static/bundle.js            │
│ 14:22:04 GET    [503] 15.20ms  10.0.0.5        /auth/login                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Interactive Keyboard Controls

| Keypress | Action | Description |
| :---: | :---: | :--- |
| `q` / `Q` | **Quit** | Exits the dashboard, restores cursor and original terminal screen. |
| `d` / `D` | **Drain Mode** | Toggles draining mode on the active gateway instance. |
| `m` / `M` | **Mute / Quiet** | Toggles muted mode to suppress live log stream rendering. |
| `Ctrl-C` | **Force Exit** | Triggers graceful emergency shutdown. |
