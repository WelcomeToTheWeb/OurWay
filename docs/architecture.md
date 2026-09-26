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
- Send metrics every 60 seconds (2 seconds in streaming mode)
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
5. (Future) Broadcast alert notification to users
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

### Future Enhancements

- Per-device custom thresholds
- Alert deduplication (don't repeat within a time window)
- Alert escalation
- Email/SMS/Slack notifications
- Alert acknowledgment workflow

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

1. **Streaming vs Snapshots**: Agents send snapshots every 60 seconds by default. When a user opens a device detail page, the server tells the agent to switch to streaming mode (2-second intervals). When the user leaves, it reverts. This minimizes bandwidth and processing while providing real-time data when needed.

2. **Alerting**: Threshold-based rules are evaluated server-side every time metrics are received. This is simple, fast, and doesn't require a separate alerting system.

3. **Authentication**: JWT tokens for API auth (stateless, scalable). Agents use a simple device-specific API key (no token refresh needed).

4. **Cross-platform Agent**: Uses Go's build tags for OS-specific code. gopsutil handles 80% of the work cross-platform. The agent is a single static binary per platform.

5. **WebSocket over REST for real-time**: REST is used for CRUD operations. WebSocket is used for real-time bidirectional communication (heartbeats, metrics streaming, alerts). This avoids polling and provides instant updates.

6. **No message queue**: The current architecture uses in-memory channels and direct database writes. For scale-out, a message queue (e.g., Redis, RabbitMQ) can be added later.

---

## Scalability Considerations

### Current Limitations (MVP)

- Single server process (no horizontal scaling)
- In-memory WebSocket hub (connections lost on restart)
- No message queue (direct database writes)
- No read replicas

### Future Scaling Path

1. **Horizontal scaling**: Multiple server instances behind a load balancer with shared WebSocket state (Redis pub/sub)
2. **Database scaling**: Read replicas for analytics, connection pooling (PgBouncer)
3. **Metrics storage**: Time-series database (InfluxDB, TimescaleDB) for historical metrics
4. **Message queue**: RabbitMQ or Redis for decoupling agent ingestion from processing
5. **Caching**: Redis for device status and recent metrics
6. **CDN**: Static assets served from CDN for global users
