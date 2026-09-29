# OurWay RMM Agent

The OurWay agent is a lightweight, cross-platform Go binary that collects system metrics and communicates with the OurWay server in real-time via WebSocket.

---

## Table of Contents

- [Agent Architecture](#agent-architecture)
- [Metric Collection](#metric-collection)
- [WebSocket Protocol](#websocket-protocol)
- [Streaming Mode](#streaming-mode)
- [Self-Updating](#self-updating)
- [Platform-Specific Notes](#platform-specific-notes)
- [Agent CLI Usage](#agent-cli-usage)

---

## Agent Architecture

```
┌─────────────────────────────────────────────────────┐
│                  OurWay Agent                        │
│                                                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │    CLI      │  │   Config    │  │   Logger    │  │
│  │  Interface  │  │    Loader   │  │             │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  │
│                                                       │
│  ┌───────────────────────────────────────────────┐   │
│  │           CollectorManager                    │   │
│  │  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────────┐ │   │
│  │  │  CPU  │ │ Memory│ │ Disk  │ │ Network   │ │   │
│  │  └───────┘ └───────┘ └───────┘ └───────────┘ │   │
│  │  ┌───────────┐ ┌───────────┐                  │   │
│  │  │ Processes │ │  System   │                  │   │
│  │  └───────────┘ └───────────┘                  │   │
│  └───────────────────────────────────────────────┘   │
│                                                       │
│  ┌───────────────────────────────────────────────┐   │
│  │           WebSocket Client                    │   │
│  │  • Connect/Reconnect with backoff             │   │
│  │  • Heartbeat timer (15s)                      │   │
│  │  • Metrics timer (60s default)                │   │
│  └───────────────────────────────────────────────┘   │
│                                                       │
│  ┌───────────────────────────────────────────────┐   │
│  │           Install Manager                     │   │
│  │  • systemd (Linux)                            │   │
│  │  • launchd (macOS)                            │   │
│  │  • sc.exe (Windows)                           │   │
│  └───────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
         │ WebSocket (ws:// or wss://)
         ▼
┌─────────────────┐
│  OurWay Server  │
│   /ws endpoint  │
└─────────────────┘
```

### Key Design Principles

- **Single binary**: No runtime dependencies, static linking where possible
- **Cross-platform**: Same codebase for Linux, macOS, and Windows via Go build tags
- **Low overhead**: Minimal CPU and memory footprint
- **Resilient**: Automatic reconnect with exponential backoff
- **Stateless**: No local database, all data sent to server

---

## Metric Collection

The agent collects system metrics using the [gopsutil](https://github.com/shirou/gopsutil) library. Each metric type has a dedicated collector that implements the `Collector` interface.

### Collector Interface

```go
type Collector interface {
    Name() string
    Collect() (map[string]interface{}, error)
}
```

### Metrics Collected

#### CPU

| Field | Type | Description |
|-------|------|-------------|
| `cpu` | float64 | Overall CPU usage percentage (0-100) |
| `cpu_per_core` | []float64 | Per-core CPU usage percentages |

Collected via: `gopsutil/cpu.Percent()`

#### Memory

| Field | Type | Description |
|-------|------|-------------|
| `ram` | float64 | RAM usage percentage (0-100) |
| `ram_used` | uint64 | RAM used in bytes |
| `ram_total` | uint64 | Total RAM in bytes |
| `swap` | float64 | Swap usage percentage (0-100) |
| `swap_used` | uint64 | Swap used in bytes |
| `swap_total` | uint64 | Total swap in bytes |

Collected via: `gopsutil/mem.VirtualMemory()`, `gopsutil/mem.SwapMemory()`

#### Disk

| Field | Type | Description |
|-------|------|-------------|
| `disks` | []DiskInfo | Array of disk partition info |

Each `DiskInfo` contains:

| Field | Type | Description |
|-------|------|-------------|
| `mount_point` | string | Mount point path |
| `device` | string | Device name |
| `type` | string | Filesystem type |
| `total` | uint64 | Total size in bytes |
| `used` | uint64 | Used space in bytes |
| `free` | uint64 | Free space in bytes |
| `usage_percent` | float64 | Usage percentage (0-100) |

Collected via: `gopsutil/disk.Partitions()`, `gopsutil/disk.Usage()`

#### Network

| Field | Type | Description |
|-------|------|-------------|
| `network` | map[string]NetworkInfo | Network interface statistics |

Each `NetworkInfo` contains:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Interface name |
| `bytes_sent` | uint64 | Bytes sent |
| `bytes_recv` | uint64 | Bytes received |
| `packets_sent` | uint64 | Packets sent |
| `packets_recv` | uint64 | Packets received |

Collected via: `gopsutil/net.IOCounters()`

#### Processes

| Field | Type | Description |
|-------|------|-------------|
| `top_processes` | []ProcessInfo | Top processes by CPU usage |

Each `ProcessInfo` contains:

| Field | Type | Description |
|-------|------|-------------|
| `pid` | int32 | Process ID |
| `name` | string | Process name |
| `cpu` | float64 | CPU usage percentage |
| `memory` | uint64 | Memory usage in bytes |

Collected via: `gopsutil/process.Processes()`

#### System

| Field | Type | Description |
|-------|------|-------------|
| `uptime` | uint64 | System uptime in seconds |
| `load_avg` | []float64 | Load averages (1, 5, 15 min) where available |

Collected via: `gopsutil/host.Uptime()`, `gopsutil/load.Avg()`

### Collection Intervals

| Mode | Interval | Trigger |
|------|----------|---------|
| Normal | 60s | Automatic, runs continuously |
| Heartbeat | 15s | Automatic liveness signal |

---

## WebSocket Protocol

The agent communicates with the server via a persistent WebSocket connection. All messages are JSON objects.

### Connection

The agent connects to the WebSocket endpoint with query parameters:

```
wss://ourway.example.com/ws?device_key=<device-key>
```

- `device_key`: UUID of the registered device

### Message Envelope

All messages use this envelope format:

```json
{
  "type": "<message-type>",
  "payload": { ... }
}
```

### Agent → Server Messages

#### heartbeat

Sent every 15 seconds to indicate the agent is alive.

```json
{
  "type": "heartbeat",
  "device_key": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": 1727232000
}
```

#### metrics

Sent every 60 seconds (configurable via `OURWAY_METRICS_INTERVAL`) with full system metrics.

```json
{
  "type": "metrics",
  "device_key": "550e8400-e29b-41d4-a716-446655440000",
  "hostname": "my-workstation",
  "timestamp": 1727232000,
  "data": {
    "cpu": 45.2,
    "cpu_per_core": [12.3, 34.5, 56.7, 78.9],
    "ram": 62.1,
    "ram_used": 25587584512,
    "ram_total": 41231686656,
    "swap": 0,
    "swap_used": 0,
    "swap_total": 0,
    "disks": [
      {
        "mount_point": "/",
        "device": "/dev/sda1",
        "type": "ext4",
        "total": 500107862016,
        "used": 150032358656,
        "free": 350075503360,
        "usage_percent": 30.0
      }
    ],
    "network": {
      "eth0": {
        "name": "eth0",
        "bytes_sent": 123456789,
        "bytes_recv": 987654321,
        "packets_sent": 123456,
        "packets_recv": 234567
      }
    },
    "top_processes": [
      {
        "pid": 1,
        "name": "systemd",
        "cpu": 0.5,
        "memory": 12345678
      }
    ],
    "uptime": 86400,
    "load_avg": [0.5, 0.4, 0.3]
  }
}
```

#### status

Sent when the agent status changes.

```json
{
  "type": "status",
  "device_key": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": 1727232000,
  "data": {
    "status": "online"
  }
}
```

### Server → Agent Messages

The server sends the following message types to the agent, each wrapped in
the standard envelope:

| Type | Description |
|------|-------------|
| `session_start` | Start a remote session (begin screen capture) |
| `session_end` | End the remote session |
| `session_quality` | Change screen-capture video quality (1–100) |
| `input` | Forward keyboard/mouse input from the session viewer |
| `file_push` | Download a file pushed by the server |
| `file_pull` | Upload a file from the device to the server |
| `scan_updates` | Run a software update scan |
| `deploy_updates` | Deploy approved software updates |
| `rollback_updates` | Roll back a software deployment |
| `reboot` | Reboot the device |

The agent also still handles `stream`, `stream_end`, and `command` messages,
but the server does not currently send them.

### Reconnection Behavior

The agent uses exponential backoff for reconnection:

1. Initial backoff: 1 second
2. Multiply by 2 after each failed attempt
3. Maximum backoff: 60 seconds
4. Reset to 1 second on successful connection

---

## Streaming Mode

The agent's configuration includes a stream interval (default 2 seconds) and
the agent retains handling for `stream`/`stream_end` messages that would
switch it to a higher-frequency metrics interval. The server does not
currently send any such messages, so streaming mode is never active and the
agent always reports metrics at the normal interval (default 60 seconds).

---

## Self-Updating

*Planned for v1.1*

The agent will support self-updating via the server:

1. Server maintains a registry of agent versions
2. Agent checks for updates on startup and periodically
3. If a new version is available, the agent downloads and installs it
4. Agent restarts itself to apply the update

---

## Platform-Specific Notes

### Linux

- **Service manager**: systemd
- **Binary location**: `/usr/local/bin/ourway-agent`
- **Unit file**: `/etc/systemd/system/ourway-agent.service`
- **Log location**: Systemd journal (`journalctl -u ourway-agent`)
- **Permissions**: Runs as root by default (for comprehensive metrics)

#### Requirements

- systemd (most modern distributions)
- No external dependencies (static binary)

### macOS

- **Service manager**: launchd
- **Binary location**: `/usr/local/bin/ourway-agent`
- **Plist file**: `~/Library/LaunchAgents/com.ourway.agent.plist`
- **Log location**: `~/Library/Logs/ourway-agent.log`
- **Permissions**: Runs as the current user

#### Requirements

- macOS 10.15+
- No external dependencies

### Windows

- **Service manager**: Windows Service Control Manager (SCM)
- **Binary location**: `C:\Program Files\OurWay\ourway-agent.exe`
- **Service name**: `OurWayAgent`
- **Log location**: Windows Event Log (Application log, source: OurWayAgent)
- **Permissions**: Runs as LocalSystem by default

#### Requirements

- Windows 10+ / Windows Server 2016+
- No external dependencies (static binary)

### Cross-Platform Differences

| Feature | Linux | macOS | Windows |
|---------|-------|-------|---------|
| Service type | systemd unit | launchd agent | Windows service |
| Default user | root | current user | LocalSystem |
| Log location | journal | ~/Library/Logs/ | Event Log |
| Binary extension | none | none | .exe |
| Path separator | / | / | \ |

---

## Agent CLI Usage

### Basic Commands

```bash
# Print version
ourway-agent --version

# Run the agent (foreground)
ourway-agent --server ws://localhost:8080/ws --key <device-key>

# Install as a service
ourway-agent --server ws://localhost:8080/ws --key <device-key> --install

# Uninstall the service
ourway-agent --uninstall

# Run with custom log file
ourway-agent --server ws://localhost:8080/ws --key <device-key> --log-file /var/log/ourway-agent.log
```

### Environment Variable Usage

```bash
# Set via environment
export OURWAY_SERVER="ws://localhost:8080/ws"
export OURWAY_DEVICE_KEY="<device-key>"

# Run the agent
ourway-agent

# Or install as service
ourway-agent --install
```

### Configuration Precedence

```
CLI flags > Environment variables > Defaults
```

Example: If `--server` is specified on the command line, it overrides `OURWAY_SERVER` environment variable.

### Full CLI Reference

```
OurWay Agent v1.0.0

Usage: ourway-agent [flags]

Flags:
  --server string       WebSocket server URL (default "ws://localhost:8080")
  --key string          Device key
  --install             Install as a service and exit
  --uninstall           Uninstall service and exit
  --version             Print version and exit
  --log-file string     Log file path
  -h, --help            help for ourway-agent

Environment Variables:
  OURWAY_SERVER           WebSocket server URL
  OURWAY_DEVICE_KEY       Device key
  OURWAY_HEARTBEAT        Heartbeat interval (default "15s")
  OURWAY_METRICS_INTERVAL Metrics interval (default "60s")
```

### Troubleshooting

#### Agent not connecting

```bash
# Check if the agent is running
systemctl status ourway-agent  # Linux
launchctl list | grep ourway  # macOS
sc query OurWayAgent           # Windows

# Check logs
journalctl -u ourway-agent -f  # Linux
tail -f ~/Library/Logs/ourway-agent.log  # macOS

# Verify server is reachable
curl http://localhost:8080/health
```

#### Agent shows offline in dashboard

1. Verify the device key is correct
2. Check network connectivity between agent and server
3. Ensure firewall allows outbound WebSocket connections
4. Check agent logs for connection errors

#### High CPU usage by agent

- Increase the metrics interval: `export OURWAY_METRICS_INTERVAL="120s"`
- The agent has no active streaming mode; metrics are sent at the configured interval (default 60s)
