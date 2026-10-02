# NexusGate CLI Reference Manual

`nexusgate` is the unified command-line entrypoint for the NexusGate high-performance edge reverse proxy and terminal observatory designed specifically for unprivileged Linux ARM64 (Android Termux) environments.

---

## Global Usage

```bash
nexusgate <command> [arguments] [flags]
```

### Global Flags & Aliases
- `-h`, `--help`: Display available commands or contextual help for a subcommand.
- `-v`, `--version`: Display version, git commit, build date, and target platform.

---

## Command Hierarchy

| Command | Description | Common Flags |
| :--- | :--- | :--- |
| `start` | Launch gateway engine in background headless daemon mode | `-c, --config`, `-s, --socket` |
| `run` | Launch all-in-one foreground gateway and TUI observatory | `-c, --config`, `-s, --socket`, `--fps` |
| `attach` | Connect live TUI dashboard to a running gateway via UDS socket | `-s, --socket`, `--fps` |
| `validate` | Perform offline syntax, schema, and security checks on config | `-c, --config` |
| `reload` | Send atomic zero-downtime hot config reload signal via IPC | `-s, --socket`, `-c, --config` |
| `drain` | Gracefully drain backends or initiate soft gateway termination | `-s, --socket`, `-t, --timeout` |
| `version` | Print binary build metadata, Go compiler version, and platform | `--json` |
| `help` | Output help summaries for commands | `[command]` |

---

## Command Details

### 1. `nexusgate start`
Starts the HTTP reverse proxy engine in headless mode. Binds the ingress TCP socket and initializes the Unix Domain Socket (UDS) IPC listener.

```bash
nexusgate start -c /data/data/com.termux/files/home/nexusgate/nexusgate.yaml
```

**Options:**
- `-c, --config <path>`: Path to configuration YAML/JSON file (default: `./nexusgate.yaml` or `$PREFIX/etc/nexusgate/nexusgate.yaml`).
- `-s, --socket <path>`: Path to Unix domain socket for IPC (default: `/data/data/com.termux/files/usr/tmp/nexusgate.sock` or `/tmp/nexusgate.sock`).

---

### 2. `nexusgate run`
Launches the full gateway reverse proxy engine while simultaneously running the differential ANSI Terminal Observatory (TUI) on stdout/stdin. Ideal for interactive development and live mobile monitoring.

```bash
nexusgate run -c ./nexusgate.yaml --fps 30
```

**Options:**
- `-c, --config <path>`: Path to configuration file.
- `-s, --socket <path>`: Path to IPC socket.
- `--fps <rate>`: Screen refresh rate cap (default: `30`, automatically throttled when battery is low).

---

### 3. `nexusgate attach`
Attaches a standalone TUI dashboard process to a pre-existing background `nexusgate start` instance by connecting to its IPC Unix Domain Socket. Consumes binary delta telemetry frames at negligible CPU overhead.

```bash
nexusgate attach -s /tmp/nexusgate.sock
```

**Options:**
- `-s, --socket <path>`: Path to the running instance's UDS socket.
- `--fps <rate>`: Refresh cap (default: `30`).

---

### 4. `nexusgate validate`
Performs deep static verification on a configuration file without binding any network interfaces.
Validates:
- Syntax validity (YAML / JSON).
- Unprivileged port restrictions ($\ge 1024$ for Termux userland).
- Upstream target URI structure and routing integrity.
- Circuit breaker, balancer, and rate limiter thresholds.

```bash
nexusgate validate -c ./nexusgate.yaml
```

**Exit Codes:**
- `0`: Configuration is valid.
- `1`: Syntax error, unprivileged port violation, or missing upstream reference.

---

### 5. `nexusgate reload`
Sends a hot-reload IPC packet over the local UDS socket. The running supervisor parses the new configuration file, initializes new pools, atomically swaps the active pointer, and retires old workers with zero dropped requests.

```bash
nexusgate reload -s /tmp/nexusgate.sock -c ./nexusgate.new.yaml
```

---

### 6. `nexusgate drain`
Commands the running gateway to place all or specified upstreams into draining state. Active inflight requests are permitted to complete within the configured timeout, after which idle connections are closed.

```bash
nexusgate drain -s /tmp/nexusgate.sock -t 30s
```

---

### 7. `nexusgate version`
Prints version and build provenance.

```bash
nexusgate version
nexusgate version --json
```

---

## Signal Handling

| Signal | Mode | Action |
| :--- | :--- | :--- |
| `SIGINT` / `SIGTERM` | `start`, `run` | Graceful shutdown; drains in-flight requests and unlinks UDS socket. |
| `SIGHUP` | `start` | Reloads active configuration from disk. |
| `SIGWINCH` | `run`, `attach` | Terminal resize detection; triggers differential screen recalculation. |
