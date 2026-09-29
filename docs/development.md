# OurWay RMM Development Guide

This guide covers setting up a development environment, running the project in development mode, and contributing to the codebase.

---

## Table of Contents

- [Development Setup](#development-setup)
- [Running in Development Mode](#running-in-development-mode)
- [Code Structure](#code-structure)
- [Adding New Features](#adding-new-features)
  - [Adding New Metric Collectors](#adding-new-metric-collectors)
  - [Adding New API Endpoints](#adding-new-api-endpoints)
  - [Adding New Frontend Pages](#adding-new-frontend-pages)
- [Testing](#testing)
- [Build Instructions](#build-instructions)
- [Contributing Guidelines](#contributing-guidelines)

---

## Development Setup

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.26+ | Server, agent, CLI |
| Node.js | 22+ | Frontend build and dev server |
| PostgreSQL | 16+ | Database |
| Git | Latest | Version control |
| Docker | Latest (optional) | Containerized development |

### 1. Clone the Repository

```bash
git clone https://github.com/ourway-rmm/ourway.git
cd ourway
```

### 2. Set Up PostgreSQL

Create the development database:

```bash
sudo -u postgres psql <<SQL
CREATE USER ourway_dev WITH PASSWORD 'ourway';
CREATE DATABASE ourway_dev OWNER ourway_dev;
GRANT ALL PRIVILEGES ON DATABASE ourway_dev TO ourway_dev;
SQL
```

### 3. Install Frontend Dependencies

```bash
cd web
npm install
cd ..
```

### 4. Verify Dependencies

```bash
# Server dependencies
cd server && go mod download && cd ..

# Agent dependencies
cd agent && go mod download && cd ..

# CLI dependencies
cd cmd/ourway-cli && go mod download && cd ..
```

### 5. Create Environment Files

Create `server/.env` (optional, for development convenience):

```bash
SERVER_PORT=":9090"  # Vite dev server proxies /api and /ws to localhost:9090
DATABASE_URL="postgresql://ourway_dev:ourway@localhost:5432/ourway_dev?sslmode=disable"
JWT_SECRET="dev-secret-key"
WS_PATH="/ws"
```

---

## Running in Development Mode

### Server (with auto-reload)

Use `air` for hot-reload during development:

```bash
# Install air
go install github.com/air-verse/air@latest

# Run server with auto-reload
cd server
air
```

Or run directly:

```bash
cd server
go run .
```

### Frontend (with hot-reload)

```bash
cd web
npm run dev
```

The dev server runs on `http://localhost:3000` with:

- Hot Module Replacement (HMR)
- Source maps
- Fast build times

### Agent (development)

```bash
cd agent
go run . --server ws://localhost:9090/ws --key <device-key>
```

### Full Stack (Docker Compose)

For a fully containerized development environment:

```bash
docker compose -f docker/docker-compose.yml up -d
```

### Service Overview

| Service | Port | URL |
|---------|------|-----|
| Web dev server | 3000 | http://localhost:3000 |
| Go server | 9090 | http://localhost:9090 |
| PostgreSQL | 5432 | localhost:5432 |

Note: the server's default port is 8080 (`SERVER_PORT` in `server/config/config.go`). The 9090 port above matches the Vite dev server proxy target in `web/vite.config.ts`, so the web UI can reach the backend in development mode.

---

## Code Structure

```
ourway/
├── server/                    # Go backend server
│   ├── main.go                # Server entry point
│   ├── go.mod                 # Go module definition
│   ├── config/
│   │   └── config.go          # Configuration loading
│   ├── api/
│   │   ├── handlers.go        # Route setup
│   │   ├── auth.go            # Auth endpoints
│   │   ├── devices.go         # Device endpoints
│   │   └── alerts.go          # Alert endpoints
│   ├── ws/
│   │   └── hub.go             # WebSocket hub
│   ├── models/
│   │   ├── user.go            # User model
│   │   ├── device.go          # Device model
│   │   ├── metrics.go         # Metrics model
│   │   └── alert.go           # Alert model
│   ├── store/
│   │   ├── store.go           # Database initialization
│   │   ├── users.go           # User repository
│   │   ├── devices.go         # Device repository
│   │   └── alerts.go          # Alert repository
│   ├── auth/
│   │   └── jwt.go             # JWT token handling
│   └── alerts/
│       └── engine.go          # Alert evaluation engine
│
├── agent/                     # Go cross-platform agent
│   ├── main.go                # Agent entry point
│   ├── go.mod                 # Go module definition
│   ├── config/
│   │   └── config.go          # Configuration loading
│   ├── client/
│   │   └── client.go          # WebSocket client
│   ├── collector/
│   │   ├── collector.go       # Collector manager and interfaces
│   │   ├── cpu.go             # CPU metrics collector
│   │   ├── memory.go          # Memory metrics collector
│   │   ├── disk.go            # Disk metrics collector
│   │   ├── network.go         # Network metrics collector
│   │   ├── processes.go       # Process metrics collector
│   │   └── system.go          # System info (uptime, load)
│   └── install/
│       ├── linux.go           # systemd service installation
│       ├── darwin.go          # launchd service installation
│       └── windows.go         # Windows service installation
│
├── web/                       # React frontend
│   ├── src/
│   │   ├── App.tsx            # Main application component
│   │   ├── main.tsx           # Application entry point
│   │   ├── api/
│   │   │   ├── client.ts      # Axios HTTP client
│   │   │   └── devices.ts     # API endpoint functions
│   │   ├── auth/
│   │   │   ├── context.tsx    # Authentication context
│   │   │   └── types.ts       # Auth type definitions
│   │   ├── components/
│   │   │   └── Layout.tsx     # Main layout component
│   │   ├── hooks/
│   │   │   ├── useDevices.ts  # Device data hook
│   │   │   └── useWebSocket.ts # WebSocket connection hook
│   │   ├── pages/
│   │   │   ├── Login.tsx      # Login page
│   │   │   ├── Dashboard.tsx  # Dashboard page
│   │   │   ├── Devices.tsx    # Devices list page
│   │   │   ├── DeviceDetail.tsx # Device detail page
│   │   │   ├── Alerts.tsx     # Alerts page
│   │   │   └── Settings.tsx   # Settings page
│   │   ├── stores/
│   │   │   ├── devices.ts     # Device state store
│   │   │   └── metrics.ts     # Metrics state store
│   │   └── types/
│   │       ├── device.ts      # Device type definitions
│   │       └── alert.ts       # Alert type definitions
│   ├── index.html             # HTML template
│   ├── vite.config.ts         # Vite configuration
│   ├── tailwind.config.ts     # Tailwind CSS configuration
│   ├── tsconfig.json          # TypeScript configuration
│   └── package.json           # npm package definition
│
├── cmd/
│   └── ourway-cli/            # CLI tool
│       ├── main.go            # CLI entry point
│       └── go.mod             # Go module definition
│
├── docker/
│   ├── docker-compose.yml     # Docker Compose configuration
│   ├── Dockerfile.server      # Server Docker image
│   ├── Dockerfile.web         # Web frontend Docker image
│   └── nginx.conf             # Nginx configuration
│
└── docs/                      # Documentation
    ├── quickstart.md          # Quick start guide
    ├── installation.md        # Installation guide
    ├── configuration.md       # Configuration reference
    ├── agent.md               # Agent documentation
    ├── api.md                 # API reference
    ├── architecture.md        # Architecture documentation
    ├── development.md         # Development guide
    └── changelog.md           # Release changelog
```

---

## Adding New Features

### Adding New Metric Collectors

1. **Create the collector file** in `agent/collector/`:

```go
// agent/collector/gpu.go
package collector

import (
    "github.com/shirou/gopsutil/v4/cpu"
)

type GPUCollector struct{}

func NewGPUCollector() *GPUCollector {
    return &GPUCollector{}
}

func (c *GPUCollector) Name() string {
    return "gpu"
}

func (c *GPUCollector) Collect() (map[string]interface{}, error) {
    // Collect GPU metrics
    return map[string]interface{}{
        "gpu_percent": 45.0,
        "gpu_memory_used": 1024000000,
        "gpu_memory_total": 8589934592,
    }, nil
}
```

2. **Register the collector** in `agent/collector/collector.go`:

```go
func NewCollectorManager() *CollectorManager {
    return &CollectorManager{
        collectors: []Collector{
            NewCPUCollector(),
            NewMemoryCollector(),
            NewDiskCollector(),
            NewNetworkCollector(),
            NewProcessCollector(),
            NewGPUCollector(), // Add new collector
        },
    }
}
```

3. **Update the Metrics struct** if needed:

```go
type Metrics struct {
    // ... existing fields ...
    GPU         float64 `json:"gpu"`
    GPUMemUsed  uint64  `json:"gpu_mem_used"`
    GPUMemTotal uint64  `json:"gpu_mem_total"`
}
```

4. **Add alert thresholds** in `server/alerts/engine.go` if needed.

### Adding New API Endpoints

1. **Create the handler** in `server/api/`:

```go
// server/api/reports.go
package api

import (
    "github.com/gin-gonic/gin"
)

type ReportHandler struct {
    store *store.Store
}

func NewReportHandler(s *store.Store) *ReportHandler {
    return &ReportHandler{store: s}
}

func (h *ReportHandler) GetDeviceReport(c *gin.Context) {
    id := c.Param("id")
    // ... handler logic ...
    c.JSON(200, gin.H{"report": "..."})
}
```

2. **Register the route** in `server/api/handlers.go`:

```go
func SetupRouter(store *store.Store, jwtAuth *auth.JWTAuth, hub *ws.Hub, engine *alerts.Engine) *gin.Engine {
    // ... existing code ...

    reportHandler := NewReportHandler(store)

    protected := r.Group("/api", protect)
    {
        // ... existing routes ...
        protected.GET("/devices/:id/report", reportHandler.GetDeviceReport)
    }

    return r
}
```

### Adding New Frontend Pages

1. **Create the page component** in `web/src/pages/`:

```tsx
// web/src/pages/Reports.tsx
import { useEffect, useState } from 'react';
import client from '../api/client';

export function Reports() {
    const [reports, setReports] = useState([]);

    useEffect(() => {
        client.get('/reports')
            .then(res => setReports(res.data.reports));
    }, []);

    return (
        <div>
            <h1>Reports</h1>
            {/* Render reports */}
        </div>
    );
}
```

2. **Add the route** in `web/src/App.tsx`:

```tsx
import { Reports } from './pages/Reports';

// Add to the Routes:
<Route path="reports" element={<Reports />} />
```

3. **Add navigation** in the Layout component if needed.

### Adding New WebSocket Message Types

1. **Server side** in `server/ws/hub.go`:

```go
func (h *Hub) handleDeviceMessage(client *Client, msg Message, store *store.Store, deviceKey string) {
    switch msg.Type {
    case "heartbeat":
        // ... existing code ...
    case "log":
        // Handle new message type
        h.BroadcastMessage("log", msg.Payload)
    }
}
```

2. **Agent side** in `agent/client/client.go`:

```go
// Send a log message
func (c *Client) sendLog(ctx context.Context, conn *websocket.Conn, level, message string) {
    msg := map[string]interface{}{
        "type":       "log",
        "device_key": c.deviceKey,
        "level":      level,
        "message":    message,
        "timestamp":  time.Now().Unix(),
    }
    wsjson.Write(ctx, conn, msg)
}
```

3. **Frontend side** in `web/src/hooks/useWebSocket.ts`:

```tsx
case 'log':
    // Handle log message
    console.log(payload.message);
    break;
```

---

## Testing

### Server Tests

```bash
cd server

# Run all tests
go test ./...

# Run tests for a specific package
go test ./api/...

# Run with verbose output
go test -v ./...

# Run with coverage
go test -cover ./...
```

### Agent Tests

```bash
cd agent

# Run all tests
go test ./...

# Run tests for a specific collector
go test ./collector/... -v
```

### CLI Tests

```bash
cd cmd/ourway-cli

# Run all tests
go test ./...
```

### Frontend Tests

The frontend uses the standard React testing setup:

```bash
cd web

# Type checking
npm run typecheck

# Build (catches type errors)
npm run build
```

### Integration Testing

To test the full stack:

```bash
# Start all services
docker compose -f docker/docker-compose.yml up -d

# Register a test user
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test","email":"test@example.com","password":"test123"}'

# Login
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test123"}'

# Run the agent
cd agent && go run . --server ws://localhost:8080/ws --key <device-key>

# Check the dashboard
open http://localhost:3000
```

---

## Build Instructions

### Building the Server

```bash
cd server

# Standard build
go build -o ourway-server .

# Production build with version info
VERSION=1.0.0 GIT_COMMIT=$(git rev-parse --short HEAD) \
  go build \
  -ldflags "-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" \
  -o ourway-server .
```

### Building the Agent

```bash
cd agent

# Current platform
go build -o ourway-agent .

# All platforms (cross-compilation)
GOOS=linux GOARCH=amd64 go build -o ourway-agent-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o ourway-agent-linux-arm64 .
GOOS=darwin GOARCH=amd64 go build -o ourway-agent-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o ourway-agent-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o ourway-agent-windows-amd64.exe .
```

### Building the Frontend

```bash
cd web

# Build (type-checks, then bundles with Vite)
npm run build
```

### Building Docker Images

```bash
# Build all images
docker compose -f docker/docker-compose.yml build

# Build specific service
docker compose -f docker/docker-compose.yml build server

# Build with version info
VERSION=1.0.0 docker compose -f docker/docker-compose.yml build server
```

### Building the CLI

```bash
cd cmd/ourway-cli

# Current platform
go build -o ourway-cli .

# All platforms
GOOS=linux GOARCH=amd64 go build -o ourway-cli-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o ourway-cli-linux-arm64 .
GOOS=darwin GOARCH=amd64 go build -o ourway-cli-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o ourway-cli-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o ourway-cli-windows-amd64.exe .
```

### Cross-Compilation Script

Create a `build-all.sh` script:

```bash
#!/usr/bin/env bash
set -euo pipefail

VERSION=${VERSION:-1.0.0}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD)}
OUT_DIR="dist"
mkdir -p "$OUT_DIR"

echo "Building OurWay RMM v${VERSION} (${GIT_COMMIT})"

# Server
echo "Building server..."
cd server
go build -ldflags "-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" -o "$OUT_DIR/ourway-server" .
cd ..

# Agent (all platforms)
echo "Building agent..."
cd agent
for os in linux darwin windows; do
    for arch in amd64 arm64; do
        if [ "$os" = "windows" ] && [ "$arch" = "arm64" ]; then
            continue  # No Windows ARM64 support
        fi
        ext=""
        if [ "$os" = "windows" ]; then
            ext=".exe"
        fi
        GOOS=$os GOARCH=$arch go build \
            -ldflags "-s -w" \
            -o "$OUT_DIR/ourway-agent-${os}-${arch}${ext}" .
    done
done
cd ..

# CLI (all platforms)
echo "Building CLI..."
cd cmd/ourway-cli
for os in linux darwin windows; do
    for arch in amd64 arm64; do
        if [ "$os" = "windows" ] && [ "$arch" = "arm64" ]; then
            continue
        fi
        ext=""
        if [ "$os" = "windows" ]; then
            ext=".exe"
        fi
        GOOS=$os GOARCH=$arch go build \
            -ldflags "-s -w" \
            -o "$OUT_DIR/ourway-cli-${os}-${arch}${ext}" .
    done
done
cd ..

# Frontend
echo "Building frontend..."
cd web
npm run build
cp -r dist "$OUT_DIR/web"
cd ..

echo "Build complete! Artifacts in $OUT_DIR/"
```

---

## Contributing Guidelines

### Code Style

**Go:**

- Follow standard Go formatting (`gofmt` or `go fmt`)
- Use meaningful variable and function names
- Include package-level documentation comments
- Keep functions focused and small

```bash
# Format all Go code
go fmt ./...
```

**TypeScript/React:**

- Use TypeScript strict mode
- Use functional components with hooks
- Prefer named exports over default exports
- Include JSDoc comments for complex logic

```bash
# Check types
npm run typecheck
```

### Commit Messages

Use conventional commit format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

Types:

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:

```
feat(agent): add GPU metrics collector
fix(api): handle missing device key in heartbeat
docs(readme): update quick start guide
refactor(server): simplify WebSocket hub registration
test(collector): add tests for disk collector
```

### Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/new-collector`)
3. Make your changes
4. Add tests if applicable
5. Ensure all tests pass
6. Update documentation if needed
7. Submit a pull request with a clear description

### Code Review Checklist

- [ ] Code follows project style guidelines
- [ ] All tests pass
- [ ] No linting errors
- [ ] Documentation is updated
- [ ] No breaking changes (or properly documented)
- [ ] Commits are well-organized with clear messages
