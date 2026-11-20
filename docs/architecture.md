# OurWay RMM Architecture

This document describes the high-level architecture of the OurWay RMM system, including component design, data flow, and key technical decisions.

---

## Table of Contents

- [System Overview](#system-overview)
- [Components](#components)
  - [Server](#server)
  - [Agent](#agent)
  - [Frontend](#frontend)
  - [Database](#database)
- [Data Flow](#data-flow)
- [WebSocket Hub](#websocket-hub)
- [Operational Counters](#operational-counters)
- [Alert Engine](#alert-engine)
- [Authentication Flow](#authentication-flow)
- [Technology Choices](#technology-choices)
- [Scalability Considerations](#scalability-considerations)

---

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          OurWay RMM System                          │
│                                                                       │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐  │
│  │   Agent (Linux) │    │   Agent (macOS) │    │ Agent (Windows) │  │
│  │   ws://         │    │   ws://         │    │   ws://         │  │
│  └────────┬────────┘    └────────┬────────┘    └────────┬────────┘  │
│           │ WebSocket (TLS)      │ WebSocket (TLS)      │ WebSocket  │
│           ▼                      ▼                      ▼           │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                   Go Backend Server                         │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐ │   │
│  │  │ REST API │  │  WS Hub  │  │  Alerts  │  │    Auth    │ │   │
│  │  │  (Gin)   │  │ (nhooyr) │  │  Engine  │  │  (JWT)     │ │   │
│  │  └──────────┘  └──────────┘  └──────────┘  └────────────┘ │   │
│  │                      │                                      │   │
│  │                      ▼                                      │   │
│  │               ┌────────────┐                                │   │
│  │               │ GORM ORM   │                                │   │
│  │               └─────┬──────┘                                │   │
│  └─────────────────────┼───────────────────────────────────────┘   │
│                        │                                           │
│                        ▼                                           │
│               ┌────────────────┐                                   │
│               │   PostgreSQL   │                                   │
│               │       16       │                                   │
│               └────────────────┘                                   │
│                                                                    │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                   Web Dashboard (React)                     │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐ │   │
│  │  │Dashboard │  │ Devices  │  │  Alerts  │  │  Settings  │ │   │
│  │  └──────────┘  └──────────┘  └──────────┘  └────────────┘ │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                        ▲                                           │
│                        │ HTTP + WebSocket                           │
│                        │ (via Nginx reverse proxy)                  │
└────────────────────────┼───────────────────────────────────────────┘
                         │
                    ┌────┴────┐
                    │  Nginx  │
                    │ (TLS)   │
                    └─────────┘
```

---

## Components

### Server

The server is a single Go binary that combines:

1. **REST API** (Gin framework) — CRUD operations for users, devices, alerts
2. **WebSocket Hub** — Real-time bidirectional communication
3. **Alert Engine** — Threshold-based alert evaluation
4. **Authentication Service** — JWT token generation and validation

**Key characteristics:**

- Stateful (maintains WebSocket connections in memory)
- Single process, single thread per connection (Go goroutines)
- Uses GORM for database access
- Automatic database migrations on startup

**Port:** 8080 (configurable via `SERVER_PORT`)

**Endpoints:**

| Path | Type | Description |
|------|------|-------------|
| `/api/auth/*` | REST | User authentication |
| `/api/devices/*` | REST | Device management |
| `/api/agent/*` | REST | Agent communication |
| `/api/alerts/*` | REST | Alert management |
| `/ws` | WebSocket | Real-time communication |
| `/health` | HTTP | Health check |

### Agent

The agent is a lightweight Go binary deployed on each managed device.

**Key characteristics:**

- Stateless (no local database)
- Single process
- Automatic reconnection with exponential backoff
- Low resource footprint (< 50MB RAM, < 5% CPU)

**Responsibilities:**

- Collect system metrics (CPU, RAM, disk, network, processes)
- Send heartbeats every 15 seconds
- Send metrics every 60 seconds
- Maintain persistent WebSocket connection to server
- Support platform-specific service installation

**Platforms:** Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64)

### Frontend

The web dashboard is a React Single Page Application (SPA).

**Key characteristics:**

- Built with Vite for fast development and production builds
- TypeScript for type safety
- Tailwind CSS for styling
- React Router for client-side routing
- Zustand for state management
- Recharts for data visualization
- Axios for HTTP requests

**Pages:**

| Route | Component | Description |
|-------|-----------|-------------|
| `/login` | `Login` | User authentication |
| `/` | `Dashboard` | Overview of all devices |
| `/devices` | `Devices` | Device list with status |
| `/devices/:id` | `DeviceDetail` | Detailed device metrics and charts |
| `/alerts` | `Alerts` | Alert center with filtering |
| `/settings` | `Settings` | User account and preferences |

**Port:** 3000 (dev: Vite dev server, prod: Nginx serving static files)

### Database

PostgreSQL 16 is used for persistent storage.

**Schema (auto-migrated by GORM):**

#### users

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | User ID |
| `username` | VARCHAR (unique) | Username |
| `email` | VARCHAR | Email address |
| `password_hash` | VARCHAR | bcrypt password hash |
| `created_at` | TIMESTAMP | Creation timestamp |

#### devices

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | Device ID |
| `name` | VARCHAR (indexed) | Device name |
| `hostname` | VARCHAR | System hostname |
| `os` | VARCHAR | Operating system |
| `arch` | VARCHAR | CPU architecture |
| `agent_version` | VARCHAR | Agent version |
| `status` | VARCHAR (default: offline) | online/offline |
| `last_seen` | TIMESTAMP | Last heartbeat timestamp |
| `public_ip` | VARCHAR | Public IP address |
| `private_ip` | VARCHAR | Private IP address |
| `device_key` | VARCHAR (unique, indexed) | Device authentication key |
| `created_at` | TIMESTAMP | Creation timestamp |
| `updated_at` | TIMESTAMP | Last update timestamp |

#### alerts

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | Alert ID |
| `device_id` | UUID (indexed) | Associated device ID |
| `device_name` | VARCHAR | Device name (denormalized) |
| `severity` | VARCHAR | info/warning/critical |
| `message` | VARCHAR | Alert message |
| `metric` | VARCHAR | Metric name (cpu, ram, disk) |
| `value` | FLOAT | Current value |
| `threshold` | FLOAT | Alert threshold |
| `resolved` | BOOLEAN (indexed, default: false) | Resolution status |
| `acknowledged` | BOOLEAN (default: false) | Acknowledgment status |
| `created_at` | TIMESTAMP | Creation timestamp |

---

## Data Flow

### Metrics Collection Flow

```
1. Agent's CollectorManager runs all collectors (every 60s)
       ↓
2. Collectors gather system data via gopsutil
       ↓
3. Agent packages metrics into JSON message
       ↓
4. Agent sends message over WebSocket connection
       ↓
5. Server's WS Hub receives message from agent
       ↓
6. Hub routes message to metrics handler
       ↓
7. Handler updates device last_seen timestamp
       ↓
8. Handler passes metrics to Alert Engine
       ↓
9. Alert Engine evaluates thresholds, creates alerts if needed
       ↓
10. Hub broadcasts metrics to all connected user WebSocket clients
        ↓
11. Web dashboard receives metrics via WebSocket
        ↓
12. Dashboard updates charts and device status in real-time
```

### Alert Generation Flow

```
1. Metrics received from agent
       ↓
2. Alert Engine checks thresholds:
   - CPU > 90% → critical
   - CPU > 80% → warning
   - RAM > 95% → critical
   - RAM > 85% → warning
   - Disk > 95% → critical
   - Disk > 85% → warning
       ↓
3. If threshold exceeded, create Alert record
       ↓
4. Save alert to database
       ↓
5. Publish an `alert_created` webhook event; the web dashboard loads alerts via the REST API
```

### Authentication Flow

```
1. User enters credentials in web dashboard
       ↓
2. Frontend POSTs to /api/auth/login
       ↓
3. Server validates credentials (bcrypt comparison)
       ↓
4. Server generates JWT access token (24h) and refresh token (7d)
       ↓
5. Server returns tokens to frontend
       ↓
6. Frontend stores tokens in localStorage
       ↓
7. Frontend includes Bearer token in subsequent API requests
       ↓
8. Server validates JWT on each protected request
```

### Device Registration Flow

```
1. Admin runs `ourway-cli register` on target device
       ↓
2. CLI detects hostname, OS, arch, IPs
       ↓
3. CLI POSTs to /api/agent/register
       ↓
4. Server creates Device record with unique device_key
       ↓
5. Server returns device_key to CLI
       ↓
6. CLI displays device_key to admin
       ↓
7. Admin installs agent with device_key
       ↓
8. Agent connects to WebSocket with device_key
       ↓
9. Server identifies agent by device_key
```

---

## WebSocket Hub

The WebSocket Hub manages all real-time connections and message routing.

### Architecture

```
┌─────────────────────────────────────────┐
│              WebSocket Hub               │
│                                          │
│  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │ Client A │  │ Client B │  │ Client │ │
│  │ (user)   │  │ (user)   │  │ C (dev)│ │
│  └────┬─────┘  └────┬─────┘  └───┬────┘ │
│       │             │            │      │
│       ▼             ▼            ▼      │
│  ┌─────────────────────────────────┐   │
│  │         Hub (Go channels)       │   │
│  │  • register channel             │   │
│  │  • unregister channel           │   │
│  │  • broadcast channel            │   │
│  └─────────────────────────────────┘   │
│              │                          │
│              ▼                          │
│  ┌─────────────────────────────────┐   │
│  │     Client Map (thread-safe)    │   │
│  │  map[string]*Client             │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### Client Registration

Each WebSocket connection is wrapped in a `Client` struct:

```go
type Client struct {
    ID        string          // "user:<token>" or "device:<key>"
    Type      string          // "user" or "device"
    DeviceKey string          // For device clients
    Conn      *websocket.Conn // The WebSocket connection
    SendCh    chan []byte     // Per-client send channel (buffered: 100)
}
```

### Message Routing

The hub uses Go channels for thread-safe message handling:

1. **register channel** — New clients register themselves
2. **unregister channel** — Disconnecting clients remove themselves
3. **broadcast channel** — Messages to send to all clients

Each client has its own send channel, allowing non-blocking sends. If a client is slow, messages are dropped (the `default` case in the select statement).

### Connection Lifecycle

1. Client connects to `/ws?token=...` or `/ws?device_key=...`
2. Hub accepts connection and creates Client struct
3. Client sends registration message to register channel
4. Hub adds client to client map
5. Messages are sent to client via its SendCh
6. On disconnect, client sends unregister message
7. Hub removes client from map and closes SendCh

---

## Operational Counters

`server/metrics/counters.go` holds one process-local registry of named numbers.
It exists because the 2026-10 review's standing finding was "no dropped-frame
counters anywhere", and buglist.md M8/M10 are silent drops whose only trace was a
`log.Printf`. A log line is not observable: nobody greps a live server for a
number, and a drop storm buries the evidence it is supposed to leave.

Grammar is `domain.what.reason` for a drop — the reason says *why*, because "the
viewer froze" and "the alert engine fell behind" are different incidents that one
`dropped` total cannot tell apart.

**The names are an API, not a rename.** `/metrics` (2.5-1) and the console health
panel (2.5-2) read this registry, so a dashboard and an operator's alert rule point
at the string. Renaming one is a breaking change.

The review rule this list exists to enforce: **a new send site with no counter is a
review defect.** Every `select { … default: }` in `server/ws` moves one of the names
below; adding a tenth send path without adding a name and a row here is what this
table is for.

### Drop sites — all 20, each with exactly one call site

| counter | site |
|---|---|
| `ws.broadcast.full` | `hub.go` — broadcast queue full, no Redis to hand it to |
| `ws.broadcast.fallback` | `hub.go` — queue full, Redis took it: a degradation, not a loss (M8) |
| `ws.broadcast.local` | `hub.go` — a local client's send buffer was full during fan-out |
| `ws.broadcast.remote` | `hub.go` — a client could not take a broadcast from another instance |
| `ws.broadcast.suppressed` | `hub.go` — own Redis publish echo, suppressed. **Not a drop**: nonzero is expected on every multi-instance install |
| `ws.targeted.remote` | `hub.go` — cross-instance targeted message (H2) could not be queued |
| `ws.send.device` | `hub.go` — device connection full |
| `ws.send.user` | `hub.go` — one of the user's browser connections full |
| `ws.send.viewer` | `viewer.go` — native viewer attached to a session full |
| `ws.send.remote_exe` | `viewer.go` — remote-control executable connection full |
| `ws.send.agent` | `viewer.go` — agent service connection full |
| `ws.send.pong` | `hub.go` — app-level ping reply to a browser or remote exe (H1) |
| `ws.send.viewer_pong` | `viewer.go` — the same ping reply on a viewer connection |
| `ws.send.viewer_relay` | `viewer.go` — exe→viewer message could not be queued |
| `ws.send.binary_frames` | `hub.go` — capability announcement missed; the exe falls back to HTTP frame upload (works, slower) |
| `ws.send.server_version` | `hub.go` — the agent learns of an update on its next heartbeat instead of this one |
| `ws.viewer_frame.evicted` | `viewer.go` — a queued frame evicted so the viewer always sees the newest screen. Perishable by design |
| `ws.viewer_frame.full` | `viewer.go` — eviction found no room; the new frame was discarded |
| `ws.alert_queue.full` | `hub.go` — **an alert that did not fire**: this sample is never measured against its thresholds |
| `ws.session_frame.unconnected` | `hub.go` — session owner has no browser to carry the frame; the console shows a live, black session |

### Also emitted

`ingest.heartbeat.ws`, `ingest.heartbeat.http`, `ingest.metrics.ws` (counts, not
rates — the snapshot's `SampledAt` is what turns two reads into a rate);
`session.end` (where a viewer socket closing visibly ends a session);
`alert.eval.latency` (the histogram 2.5-8's release gate reads);
`redis.degraded.transitions`, `redis.recovered.transitions`, `redis.degraded` (the
**transition**, not one count per failed publish — a Redis that stays down keeps
failing, and 400 counts reads as 400 incidents); and the
`ws.connections.active` gauge, labelled `role`.

### Declared, not emitted

`ingest.metrics.http`, `session.start`, `deploy.queued.depth`, `deploy.outcome`,
and `db.pool.{open,idle,in_use,max_open,wait_total,wait_duration_ms}`. Each is
marked in the source with the reason and the task that owns the call site. They are
named so the vocabulary is settled once; **no consumer may present them as a
measurement until something moves them.** The database pool reaches a snapshot
through `DBPoolStats` (six struct fields, plus `Available: false` until
`SetDBPool` gains a caller), not through those registry names.

Two deliberate properties, because they are the ones a reader gets wrong:

- **The snapshot is process-local.** A fleet of three instances has three
  snapshots; merging is the console's job, because merging here would hide *which*
  instance dropped the frames.
- **A device key is never a label.** C3's rule applied to this surface: a metrics
  payload is readable by whoever holds the metrics token, and a fleet's device keys
  are credentials.

---

## Alert Engine

The alert engine evaluates metrics against configurable thresholds and creates alert records.

### Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| CPU    | > 80%   | > 90%    |
| RAM    | > 85%   | > 95%    |
| Disk   | > 85%   | > 95%    |

### Evaluation

The engine is called every time metrics are received from an agent:

```go
func (e *Engine) Evaluate(m models.Metrics, deviceID, deviceName string) {
    if m.CPU > 90 {
        createAlert("critical", "CPU usage high", "cpu", m.CPU, 90)
    } else if m.CPU > 80 {
        createAlert("warning", "CPU usage elevated", "cpu", m.CPU, 80)
    }
    // ... similar for RAM and disk
}
```

### Alert Workflow

- **Deduplication**: The same alert (device + metric + severity) is not re-fired within the deduplication window (5 minutes by default)
- **Acknowledgment and assignment**: Alerts can be marked as acknowledged and assigned to a user
- **Auto-resolution**: An alert is marked resolved automatically when its metric drops back to or below the warning threshold

### Future Enhancements

- Per-device custom thresholds
- Alert escalation
- Email/SMS/Slack notifications

---

## Authentication Flow

### User Authentication (JWT)

```
┌──────────┐                    ┌──────────┐
│ Frontend │                    │  Server  │
└────┬─────┘                    └────┬─────┘
     │  POST /api/auth/login         │
     │  {username, password}         │
     │──────────────────────────────>│
     │                               │ bcrypt compare
     │                               │ generate JWT tokens
     │  {access_token, refresh_token} │
     │<──────────────────────────────│
     │                               │
     │  GET /api/devices             │
     │  Authorization: Bearer <token>│
     │──────────────────────────────>│
     │                               │ validate JWT
     │  {devices: [...]}             │
     │<──────────────────────────────│
```

**JWT Claims:**

```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "admin",
  "exp": 1727318400,
  "iat": 1727232000,
  "iss": "ourway"
}
```

### Device Authentication (API Key)

```
┌──────────┐                    ┌──────────┐
│  Agent   │                    │  Server  │
└────┬─────┘                    └────┬─────┘
     │  POST /api/agent/heartbeat    │
     │  X-Device-Key: <key>          │
     │──────────────────────────────>│
     │                               │ lookup device by key
     │  {status: "ok"}               │
     │<──────────────────────────────│
```

---

## Technology Choices

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Backend framework | Go + Gin | High performance, low memory, simple concurrency model, single binary deployment |
| Agent language | Go | Cross-platform compilation, single static binary, low resource usage |
| System metrics | gopsutil | Mature, cross-platform, comprehensive metrics coverage |
| Frontend framework | React | Large ecosystem, component-based, excellent tooling |
| Build tool | Vite | Extremely fast development server, optimized production builds |
| Styling | Tailwind CSS | Utility-first, rapid development, small bundle size |
| State management | Zustand | Minimal, unopinionated, easy to use with React |
| Charts | Recharts | Declarative, React-based, good documentation |
| Database | PostgreSQL | ACID compliance, JSON support, excellent performance, reliable |
| ORM | GORM | Easy migrations, active record pattern, good PostgreSQL support |
| WebSocket library | nhooyr.io/websocket | Modern, standards-compliant, no external dependencies |
| Containerization | Docker | Reproducible builds, easy deployment |
| Reverse proxy | Nginx | Mature, fast, TLS termination, WebSocket support |
| Authentication | JWT | Stateless, scalable, industry standard |

### Key Design Decisions

1. **Snapshot Metrics**: Agents send a full metrics snapshot every 60 seconds. The server relays them to the dashboard in real time over WebSocket; there is no high-frequency streaming mode, which keeps bandwidth and processing minimal.

2. **Alerting**: Threshold-based rules are evaluated server-side every time metrics are received. This is simple, fast, and doesn't require a separate alerting system.

3. **Authentication**: JWT tokens for API auth (stateless, scalable). Agents use a simple device-specific API key (no token refresh needed).

4. **Cross-platform Agent**: Uses Go's build tags for OS-specific code. gopsutil handles 80% of the work cross-platform. The agent is a single static binary per platform.

5. **WebSocket over REST for real-time**: REST is used for CRUD operations. WebSocket is used for real-time bidirectional communication (heartbeats, metrics, status updates). This avoids polling and provides instant updates.

6. **No message queue**: The current architecture uses in-memory channels and direct database writes. For scale-out, a message queue (e.g., Redis, RabbitMQ) can be added later.

---

## Scalability Considerations

### Current Limitations (MVP)

- No message queue (direct database writes)
- No read replicas

### Future Scaling Path

1. **Database scaling**: Read replicas for analytics, connection pooling (PgBouncer)
2. **Metrics storage**: Time-series database (InfluxDB, TimescaleDB) for historical metrics
3. **Message queue**: RabbitMQ or Redis for decoupling agent ingestion from processing
4. **CDN**: Static assets served from CDN for global users
