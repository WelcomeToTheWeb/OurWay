# OurWay RMM — Configuration Reference

Complete reference for all configuration options.

---

## Table of Contents

- [Server Configuration](#server-configuration)
- [Agent Configuration](#agent-configuration)
- [CLI Tool Configuration](#cli-tool-configuration)
- [Example Config Files](#example-config-files)
- [TLS/HTTPS Configuration](#tlshttps-configuration)

---

## Server Configuration

The server reads configuration from environment variables. All variables have sensible defaults.

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SERVER_PORT` | HTTP listen address (host:port) | `:8080` | No |
| `DATABASE_URL` | PostgreSQL connection string | `postgresql://postgres:ourway@localhost:5432/ourway?sslmode=disable` | No |
| `JWT_SECRET` | Secret key for JWT token signing | `ourway-secret-key` | No (but required for production) |
| `WS_PATH` | WebSocket endpoint path | `/ws` | No |
| `REDIS_URL` | Redis connection URL | (none) | No |
| `REDIS_ENABLED` | Enable Redis-backed rate limiting and distributed WebSocket pub/sub | `false` | No |
| `WEB_URL` | Public URL of the web dashboard | `http://localhost:3000` | No |

### JWT Token Details

- **Access Token**: Valid for 24 hours
- **Refresh Token**: Valid for 7 days
- **Signing Algorithm**: HS256
- **Issuer**: `ourway` (access) / `ourway-refresh` (refresh)

### Environment Variable Precedence

Environment variables always override defaults. There is no configuration file for the server.

### Example Server Environment

```bash
# Production server environment
export SERVER_PORT=":8080"
export DATABASE_URL="postgresql://ourway:securepassword@db.example.com:5432/ourway?sslmode=require&tlsrootcert=/etc/ssl/certs/ca-certificates.crt"
export JWT_SECRET="a-very-long-random-64-character-secret-key"
export WS_PATH="/ws"
export WEB_URL="http://localhost:3000"
```

---

## Agent Configuration

The agent supports three configuration sources with this precedence (highest to lowest):

1. **Command-line flags** (highest priority)
2. **Environment variables**
3. **Built-in defaults** (lowest priority)

### Environment Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `OURWAY_SERVER` | WebSocket server URL | `ws://localhost:8080` | `wss://ourway.example.com/ws` |
| `OURWAY_DEVICE_KEY` | Unique device key (UUID) | (required) | `550e8400-e29b-41d4-a716-446655440000` |
| `OURWAY_HEARTBEAT` | Heartbeat interval | `15s` | `30s`, `1m` |
| `OURWAY_METRICS_INTERVAL` | Metrics collection interval | `60s` | `30s`, `2m` |

### Command-Line Flags

| Flag | Description | Corresponding Env Var |
|------|-------------|----------------------|
| `--server` | WebSocket server URL | `OURWAY_SERVER` |
| `--key` | Device key | `OURWAY_DEVICE_KEY` |
| `--install` | Install as a service and exit | — |
| `--uninstall` | Uninstall service and exit | — |
| `--version` | Print version and exit | — |
| `--log-file` | Path to log file | — |

### Duration Format

Duration values accept Go duration strings:

- `15s` — 15 seconds
- `1m` — 1 minute
- `1m30s` — 1 minute 30 seconds
- `2h` — 2 hours

### Default Intervals

| Interval | Default | Description |
|----------|---------|-------------|
| Heartbeat | 15s | Device liveness check |
| Metrics | 60s | Full system metrics snapshot |
| Stream interval | 2s | Interval used while the agent is in streaming mode; no server mechanism currently triggers streaming |

---

## CLI Tool Configuration

The `ourway-cli` tool has minimal configuration:

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--server` | OurWay server URL (HTTP, not WebSocket) | `http://localhost:8081` |
| `--config` | Path to config file | (none) |

### Commands

| Command | Description |
|---------|-------------|
| `register` | Register this machine as a new device |
| `install` | Install the agent as a service |
| `uninstall` | Remove the agent service |
| `status` | Check if the agent is running |
| `logs` | View agent logs |
| `version` | Show CLI version info |
| `help` | Show usage |

### Example Usage

```bash
# Register a device
ourway-cli --server https://ourway.example.com register

# Install the agent
ourway-cli --server https://ourway.example.com install

# Check status
ourway-cli --server https://ourway.example.com status

# View logs
ourway-cli --server https://ourway.example.com logs
```

---

## Example Config Files

### Server systemd Environment File

File: `/etc/ourway/server.env`

```bash
# OurWay RMM Server Configuration
# All variables are optional but recommended for production

# HTTP listen address
SERVER_PORT=":8080"

# PostgreSQL connection string
# Format: postgresql://user:password@host:port/dbname?sslmode=mode
DATABASE_URL="postgresql://ourway:ourway_password@localhost:5432/ourway?sslmode=disable"

# JWT secret key (use openssl rand -hex 32 to generate)
JWT_SECRET="3a7f8c2b9e1d4a5c6b8f0e2d7a9c1b3e4f5d6c7a8b9e0f1d2c3b4a5c6d7e8f9a"

# WebSocket endpoint path
WS_PATH="/ws"

```

### Agent systemd Environment File

File: `/etc/ourway/agent.env`

```bash
# OurWay RMM Agent Configuration

# Server WebSocket URL
OURWAY_SERVER="ws://localhost:8080/ws"

# Device key (generated when you register the device)
OURWAY_DEVICE_KEY="550e8400-e29b-41d4-a716-446655440000"

# Heartbeat interval (how often the agent reports liveness)
OURWAY_HEARTBEAT="15s"

# Metrics collection interval (how often full system metrics are sent)
OURWAY_METRICS_INTERVAL="60s"
```

### Nginx Reverse Proxy Configuration

File: `/etc/nginx/conf.d/ourway.conf`

```nginx
upstream ourway-server {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    server_name ourway.example.com;

    # Redirect HTTP to HTTPS
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name ourway.example.com;

    # SSL certificates
    ssl_certificate /etc/letsencrypt/live/ourway.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ourway.example.com/privkey.pem;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Web dashboard
    location / {
        proxy_pass http://ourway-server;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # API endpoints
    location /api/ {
        proxy_pass http://ourway-server;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket endpoint
    location /ws {
        proxy_pass http://ourway-server;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

### Docker Compose Environment Override

Create a `.env` file in the project root:

```bash
# OurWay Docker Compose Environment
VERSION=1.0.0
JWT_SECRET=3a7f8c2b9e1d4a5c6b8f0e2d7a9c1b3e4f5d6c7a8b9e0f1d2c3b4a5c6d7e8f9a
```

---

## TLS/HTTPS Configuration

For production deployments, always use TLS/HTTPS for both HTTP and WebSocket connections.

### Option 1: Nginx Reverse Proxy (Recommended)

Use Nginx as a reverse proxy with TLS termination. See the [Nginx configuration example](#example-config-files) above.

For agents:

```bash
export OURWAY_SERVER="wss://ourway.example.com/ws"
```

### Option 2: Let's Encrypt Certificates

Automate certificate management with certbot:

```bash
# Install certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificate (Nginx plugin)
sudo certbot --nginx -d ourway.example.com

# Or standalone mode
sudo certbot certonly --standalone -d ourway.example.com
```

Certificates renew automatically via a cron job installed by certbot.

### Option 3: Self-Signed Certificates

For internal networks, you can use self-signed certificates:

```bash
# Generate CA key and certificate
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
  -subj "/CN=OurWay CA" -out ca.crt

# Generate server key and certificate
openssl genrsa -out server.key 2048
openssl req -new -key server.key \
  -subj "/CN=ourway.example.com" -out server.csr
openssl x509 -req -CA ca.crt -CAkey ca.key -CAcreateserial \
  -in server.csr -out server.crt -days 825 \
  -extfile <(echo "subjectAltName=DNS:ourway.example.com,IP:192.168.1.100")
```

### WebSocket with TLS

When using TLS, the WebSocket URL changes from `ws://` to `wss://`:

| Protocol | URL Scheme | Port |
|----------|-----------|------|
| WebSocket (no TLS) | `ws://` | 8080 (default) |
| WebSocket (TLS) | `wss://` | 443 (default) |

### Client-Side TLS Verification

Agents automatically verify server certificates against the system CA bundle. For self-signed certificates, either:

1. Add the CA certificate to the system trust store, or
2. Use `SSL_CERT_FILE` environment variable pointing to your CA bundle

### Production Checklist

- [ ] Set a strong `JWT_SECRET` (at least 32 random bytes)
- [ ] Use `sslmode=require` or `sslmode=verify-full` in `DATABASE_URL`
- [ ] Configure TLS termination (Nginx or similar)
- [ ] Use `wss://` URLs for agents
- [ ] Enable HTTP/2 for better performance
- [ ] Set up automated certificate renewal (Let's Encrypt)
- [ ] Configure firewall rules (allow ports 80, 443)
- [ ] Set up log rotation
- [ ] Configure monitoring for the OurWay server itself
