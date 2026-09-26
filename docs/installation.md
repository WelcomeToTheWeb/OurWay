# OurWay RMM — Installation Guide

This guide covers detailed installation procedures for all components.

---

## Table of Contents

- [Server Installation](#server-installation)
  - [Linux systemd Service](#linux-systemd-service)
  - [Manual Server Run](#manual-server-run)
- [Agent Installation](#agent-installation)
  - [Linux (systemd)](#linux-systemd)
  - [macOS (launchd)](#macos-launchd)
  - [Windows Service](#windows-service)
  - [Using the Installer Script](#using-the-installer-script)
- [Docker Compose Installation](#docker-compose-installation)
- [CLI Tool Installation](#cli-tool-installation)
- [Configuration File Locations](#configuration-file-locations)
- [Uninstallation](#uninstallation)

---

## Server Installation

### Prerequisites

- Go 1.26+
- PostgreSQL 16+
- (Optional) Nginx for reverse proxy

### 1. Set Up the Database

```bash
# Debian/Ubuntu
sudo apt install postgresql-16

# Create user and database
sudo -u postgres psql <<SQL
CREATE USER ourway WITH PASSWORD 'ourway';
CREATE DATABASE ourway OWNER ourway;
GRANT ALL PRIVILEGES ON DATABASE ourway TO ourway;
SQL
```

### 2. Build the Server

```bash
cd server
go build -o ourway-server .
```

### 3. Configure Environment Variables

Create a systemd environment file or shell profile:

```bash
# /etc/ourway/server.env
SERVER_PORT=":8080"
DATABASE_URL="postgresql://ourway:ourway@localhost:5432/ourway?sslmode=disable"
JWT_SECRET="change-me-to-a-random-64-char-string"
WS_PATH="/ws"
TZ="UTC"
```

Generate a strong JWT secret:

```bash
openssl rand -hex 32
```

### 4. Create systemd Service

Create `/etc/systemd/system/ourway-server.service`:

```ini
[Unit]
Description=OurWay RMM Server
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=ourway
Group=ourway
WorkingDirectory=/opt/ourway/server
EnvironmentFile=/etc/ourway/server.env
ExecStart=/opt/ourway/server/ourway-server
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ourway-server

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ourway-server
sudo systemctl start ourway-server
sudo systemctl status ourway-server
```

### Manual Server Run

For development or testing:

```bash
cd server
SERVER_PORT=":8080" \
DATABASE_URL="postgresql://ourway:ourway@localhost:5432/ourway?sslmode=disable" \
JWT_SECRET="dev-secret" \
go run .
```

---

## Agent Installation

The OurWay agent is a single Go binary that runs on the monitored device.

### Build the Agent

```bash
cd agent
go build -o ourway-agent .
```

### Linux (systemd)

Copy the binary:

```bash
sudo cp ourway-agent /usr/local/bin/
```

Install as a service:

```bash
sudo ourway-agent --server wss://ourway.example.com/ws --key <device-key> --install
```

This creates `/etc/systemd/system/ourway-agent.service`:

```ini
[Unit]
Description=OurWay Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ourway-agent --server wss://ourway.example.com/ws --key <device-key>
Restart=always
RestartSec=5
User=root
Environment=OURWAY_SERVER=wss://ourway.example.com/ws
Environment=OURWAY_DEVICE_KEY=<device-key>

[Install]
WantedBy=multi-user.target
```

Check status:

```bash
systemctl status ourway-agent
journalctl -u ourway-agent -f
```

### macOS (launchd)

Copy the binary:

```bash
sudo cp ourway-agent /usr/local/bin/
```

Install as a launch agent:

```bash
sudo ourway-agent --server wss://ourway.example.com/ws --key <device-key> --install
```

This creates `~/Library/LaunchAgents/com.ourway.agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ourway.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/ourway-agent</string>
        <string>--server</string>
        <string>wss://ourway.example.com/ws</string>
        <string>--key</string>
        <string>&lt;device-key&gt;</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/&lt;user&gt;/Library/Logs/ourway-agent.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/&lt;user&gt;/Library/Logs/ourway-agent.log</string>
</dict>
</plist>
```

Check status:

```bash
launchctl list | grep com.ourway.agent
```

### Windows Service

Copy the binary:

```powershell
Copy-Item ourway-agent.exe C:\Program Files\OurWay\
```

Install as a Windows service:

```powershell
cd "C:\Program Files\OurWay"
ourway-agent.exe --server wss://ourway.example.com/ws --key <device-key> --install
```

This creates a Windows service named `OurWayAgent` that starts automatically.

Check status:

```powershell
sc query OurWayAgent
```

### Using the Installer Script

For Linux and macOS, a shell installer is provided:

```bash
# Download the installer
curl -sL https://releases.ourway.io/agent/install.sh | bash -s \
  --server wss://ourway.example.com/ws \
  --key <device-key>
```

Options:

| Flag | Description | Default |
|------|-------------|---------|
| `--server` | WebSocket server URL | `ws://localhost:8081` |
| `--key` | Device key | (required) |
| `--user` | Install as user agent (not system) | false |
| `--version` | Show installer version | - |
| `--help` | Show help | - |

---

## Docker Compose Installation

The easiest way to run the full stack (server + web + database):

```bash
# Clone the repository
git clone https://github.com/ourway-rmm/ourway.git
cd ourway

# Start all services
docker compose up -d

# Check status
docker compose ps

# View logs
docker compose logs -f server
```

This creates:

| Service | Container Name | Port | Image |
|---------|----------------|------|-------|
| PostgreSQL | `ourway-postgres` | 5432 (internal) | `postgres:16-alpine` |
| Server | `ourway-server` | 8080 → 8080 | Built from `docker/Dockerfile.server` |
| Web | `ourway-web` | 3000 → 80 | Built from `docker/Dockerfile.web` |

### Docker Volume

PostgreSQL data is stored in the `postgres_data` named volume. To persist across recreations:

```bash
docker volume ls | grep ourway
```

### Custom Configuration

Override environment variables in `docker-compose.yml` or pass them at runtime:

```bash
JWT_SECRET="my-custom-secret" docker compose up -d
```

### Build Tags

Pass build-time version information:

```bash
VERSION=1.0.0 GIT_COMMIT=$(git rev-parse --short HEAD) docker compose build server
```

---

## CLI Tool Installation

The `ourway-cli` tool helps manage agent installation and device registration.

```bash
cd cmd/ourway-cli
go build -o ourway-cli .
```

Install to PATH:

```bash
sudo mv ourway-cli /usr/local/bin/
```

Build for all platforms (cross-compilation):

```bash
GOOS=linux GOARCH=amd64 go build -o ourway-cli-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o ourway-cli-linux-arm64 .
GOOS=darwin GOARCH=amd64 go build -o ourway-cli-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o ourway-cli-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o ourway-cli-windows-amd64.exe .
```

---

## Configuration File Locations

| Component | Config Location | Format |
|-----------|-----------------|--------|
| Server (env) | `/etc/ourway/server.env` | Shell environment variables |
| Server (systemd) | `/etc/systemd/system/ourway-server.service` | systemd unit file |
| Agent (env) | `/etc/ourway/agent.conf` (Linux) or `~/.ourway/agent.conf` (macOS) | Shell environment variables |
| Agent (systemd) | `/etc/systemd/system/ourway-agent.service` | systemd unit file |
| Agent (launchd) | `~/Library/LaunchAgents/com.ourway.agent.plist` | launchd plist |
| Web (nginx) | `/etc/nginx/conf.d/ourway.conf` or `docker/nginx.conf` | Nginx configuration |

---

## Uninstallation

### Uninstall the Agent

**Linux:**

```bash
sudo systemctl stop ourway-agent
sudo systemctl disable ourway-agent
sudo rm /etc/systemd/system/ourway-agent.service
sudo systemctl daemon-reload
sudo rm /usr/local/bin/ourway-agent
```

Or use the built-in uninstall command:

```bash
sudo ourway-agent --uninstall
```

**macOS:**

```bash
launchctl unload ~/Library/LaunchAgents/com.ourway.agent.plist
rm ~/Library/LaunchAgents/com.ourway.agent.plist
rm ~/Library/Logs/ourway-agent.log
sudo rm /usr/local/bin/ourway-agent
```

Or use the built-in uninstall command:

```bash
ourway-agent --uninstall
```

**Windows:**

```powershell
sc stop OurWayAgent
sc delete OurWayAgent
Remove-Item "C:\Program Files\OurWay\ourway-agent.exe"
```

Or use the built-in uninstall command:

```powershell
ourway-agent.exe --uninstall
```

### Uninstall the Server

**Linux:**

```bash
sudo systemctl stop ourway-server
sudo systemctl disable ourway-server
sudo rm /etc/systemd/system/ourway-server.service
sudo systemctl daemon-reload
sudo rm -rf /opt/ourway/server
sudo rm -rf /etc/ourway
```

### Uninstall via Docker Compose

```bash
cd ourway
docker compose down
docker compose down -v  # Also remove volumes (deletes database!)
docker compose down --rmi all  # Also remove images
```
