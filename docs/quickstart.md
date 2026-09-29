# OurWay RMM — Quick Start

Get OurWay RMM up and running in 5 minutes.

## Prerequisites

- **Go** 1.26+ (server & agent)
- **Node.js** 22+ (frontend)
- **PostgreSQL** 16+ (database)
- Docker & Docker Compose (Option A only)

---

## Option A: Docker Compose (One-Command Startup)

The fastest way to get everything running.

```bash
# Clone the repository
git clone https://github.com/ourway-rmm/ourway.git
cd ourway

# Build and start all services (server, web, postgres)
docker compose -f docker/docker-compose.yml up -d
```

This starts:

| Service | Port | Description |
|---------|------|-------------|
| `ourway-web` | 3000 | Web dashboard (nginx) |
| `ourway-server` | 8080 | Go backend API + WebSocket hub |
| `ourway-postgres` | 5432 (internal) | PostgreSQL database |

Wait for all services to become healthy:

```bash
docker compose -f docker/docker-compose.yml ps
# All services should show "healthy"
```

---

## Option B: Manual Installation

### 1. Set Up PostgreSQL

```bash
# Create the database
sudo -u postgres psql -c "CREATE USER ourway WITH PASSWORD 'ourway';"
sudo -u postgres psql -c "CREATE DATABASE ourway OWNER ourway;"
```

### 2. Build and Run the Server

```bash
cd server

# Set environment variables
export SERVER_PORT=":8080"
export DATABASE_URL="postgresql://ourway:ourway@localhost:5432/ourway?sslmode=disable"
export JWT_SECRET="your-secret-key-here"
export WS_PATH="/ws"

# Run the server
go run .
```

### 3. Build and Run the Frontend

```bash
cd ../web

# Install dependencies
npm install

# Build for production
npm run build

# Or run in development mode (with hot reload)
npm run dev
```

Note: the Vite dev server proxies `/api` and `/ws` to `http://localhost:9090` (see `web/vite.config.ts`). When using the dev server against a local backend, start the server with `SERVER_PORT=":9090"` so the web UI can reach it.

### 4. Verify

```bash
curl http://localhost:8080/health
# Expected: {"status":"ok"}
```

Open `http://localhost:3000` in your browser.

---

## First Login

The first time you use OurWay, you need to register an admin user.

**Via the Web UI:**
1. Open `http://localhost:3000`
2. Click **Register** (or use the provided registration form)
3. Enter your credentials (e.g., username: `admin`, password: `password123`)

**Via the API:**

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "email": "admin@example.com",
    "password": "password123"
  }'
```

Then log in:

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "password123"
  }'
```

Save the `access_token` — you'll need it for authenticated requests.

---

## Register a Device

Register a device from the command line using the CLI tool:

```bash
cd ../cmd/ourway-cli
go run . register --server http://localhost:8080
```

This will:
1. Detect your hostname, OS, architecture, and IP addresses
2. Send a registration request to the server
3. Return a **device key** — save this!

Example output:

```
Device registered successfully!
Device name: my-workstation
OS: linux/amd64
Public IP: 203.0.113.50
Private IP: 192.168.1.100

Device Key: 550e8400-e29b-41d4-a716-446655440000
Save this key - it will be used for authentication.
```

---

## Install the Agent on a Device

### On Linux/macOS

```bash
# Download and run the agent
./ourway-agent --server ws://localhost:8080/ws --key 550e8400-e29b-41d4-a716-446655440000

# Or install as a service (runs in the background)
./ourway-agent --server ws://localhost:8080/ws --key 550e8400-e29b-41d4-a716-446655440000 --install
```

### On Windows

```powershell
ourway-agent.exe --server ws://localhost:8080/ws --key 550e8400-e29b-41d4-a716-446655440000
```

### Using Environment Variables

Instead of CLI flags, you can use environment variables:

```bash
export OURWAY_SERVER="ws://localhost:8080/ws"
export OURWAY_DEVICE_KEY="550e8400-e29b-41d4-a716-446655440000"
./ourway-agent
```

---

## View the Dashboard

Open `http://localhost:3000` in your browser and log in with your admin credentials.

You'll see:

- **Dashboard**: Overview of all devices with online/offline status
- **Devices**: Detailed list with CPU, RAM, disk usage for each device
- **Device Detail**: Real-time charts for a specific device (click on a device)
- **Alerts**: Threshold-based alerts (CPU > 90%, RAM > 95%, disk > 95%)
- **Settings**: User account and notification preferences

Your registered device should appear as **online** within 15 seconds (heartbeat interval).

---

## Next Steps

- [Installation Guide](installation.md) — detailed installation procedures
- [Configuration](configuration.md) — full configuration reference
- [API Reference](api.md) — complete REST API documentation
- [Development](development.md) — contributing and development setup
