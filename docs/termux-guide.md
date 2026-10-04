# NexusGate Android Termux Operations & Tuning Manual

This guide provides exhaustive deployment, operational troubleshooting, and battery optimization procedures for running NexusGate inside an unprivileged Android Termux userland environment.

---

## 1. System Requirements & Environment Preparation

- **Operating System:** Android 7.0+ (Nougat, API 24) through Android 15+ (Vanilla Ice Cream).
- **Architecture:** `aarch64` (ARM64) or `armv7l` (32-bit ARM).
- **Terminal Host:** Termux installed via F-Droid or official GitHub releases. (Note: Google Play builds are unmaintained).

### Initial Package Setup
```bash
pkg update && pkg upgrade -y
pkg install -y golang git termux-tools
```

---

## 2. Installation & Compilation

### Option A: One-Line Installer Script
```bash
./scripts/install.sh
```
This automatically compiles `bin/nexusgate`, provisions `$PREFIX/etc/nexusgate/nexusgate.yaml`, and configures binary symlinks in `$PREFIX/bin`.

### Option B: Native Compilation via Make
```bash
make build-termux
cp bin/nexusgate $PREFIX/bin/
```

---

## 3. Unprivileged Userland Execution Modes

Because Termux operates strictly as a regular unprivileged Android app UID (e.g., `u0_a123`), certain standard Linux conventions differ:

### A. Non-Root Port Restrictions ($\ge 1024$)
Unprivileged processes cannot bind to privileged ports below 1024 (e.g., `80`, `443`).
- Always configure listener ports $\ge 1024$ (e.g., `8080`, `8443`, `9000`).
- If port forwarding from port 80 is required without root, use userland redirectors or Android VPN-based port forwarders.

### B. Path Standards
- Configs: `$PREFIX/etc/nexusgate/nexusgate.yaml` or `~/.nexusgate/nexusgate.yaml`.
- Unix Domain Sockets: `$PREFIX/var/run/nexusgate/nexusgate.sock` or `~/.nexusgate/run/nexusgate.sock`.

---

## 4. Background Daemonization & Android Battery Optimization

### A. Termux Wake-Lock
When the Android screen turns off, the Linux kernel aggressively enters deep suspend. To maintain continuous reverse proxy connectivity:
```bash
termux-wake-lock
```
*(Release with `termux-wake-unlock` when finished).*

### B. Disable Phantom Process Killer (Android 12+)
Android 12 introduced a phantom process killer that terminates background processes exceeding 32 child tasks or high CPU spikes. Run via ADB:
```bash
adb shell "/system/bin/device_config put activity_manager max_phantom_processes 2147483647"
```

### C. Native `termux-services` (runit) Integration
If `termux-services` is installed:
```bash
pkg install termux-services
# Enable and start nexusgate daemon
sv-enable nexusgate
sv up nexusgate

# Check status
sv status nexusgate

# Stop daemon
sv down nexusgate
```

---

## 5. Mobile Battery Conservation & CPU Tuning

NexusGate includes dedicated mobile battery conservation features:
1. **Tickless Idle:** When 0 RPS is sustained for $>3.0\text{ s}$, all background telemetry loops enter sleeping state, dropping CPU usage below 0.3%.
2. **Dynamic TUI Refresh Throttling:** If running `nexusgate run` on battery, cap the refresh rate:
   ```bash
   nexusgate run -c ./nexusgate.yaml --fps 15
   ```
3. **Transparent Decompression Disabled:** `DisableCompression: true` prevents mobile CPU cycles from being wasted decompressing and recompressing streaming chunks in memory.

---

## 6. Troubleshooting Common Issues

### Issue 1: `bind: permission denied` on port
- **Cause:** Attempting to bind a port $< 1024$ as a non-root user.
- **Fix:** Update `listener.port` in your configuration to `8080` or higher.

### Issue 2: `bind: address already in use` on socket
- **Cause:** Previous unclean exit left a stale Unix Domain Socket.
- **Fix:** NexusGate auto-detects and removes stale sockets on startup. You can also manually remove it:
  ```bash
  rm -f $PREFIX/var/run/nexusgate/*.sock /tmp/*.sock
  ```

### Issue 3: Ingress connection refused from other LAN devices
- **Cause:** `listener.host` set to `127.0.0.1` (loopback only).
- **Fix:** Set `listener.host: "0.0.0.0"` in `nexusgate.yaml` to accept traffic across local Wi-Fi or USB tethering.
