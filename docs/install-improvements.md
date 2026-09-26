# Agent Installation Improvements

This document describes the improved agent installation experience for OurWay RMM.

## Problem

Previously, installing the OurWay agent required:
1. Knowing the exact binary for your platform
2. Manually downloading the binary
3. Setting up a service file (systemd, launchd, or Windows Service)
4. Manually configuring the server URL and device key

## Solution

We've created multiple installation methods to make the process as simple as possible:

### Method 1: One-line Bash Install (Linux/macOS)

```bash
curl -sL https://ourway.example.com/install.sh | bash -s -- --server wss://ourway.example.com --register
```

This single command:
- Detects OS and architecture
- Downloads the correct binary from the server
- Auto-registers the device (if --register is used)
- Installs as a service (systemd/launchd)
- Creates configuration files
- Starts the service

### Method 2: PowerShell Install (Windows)

```powershell
.\install.ps1 -Server "wss://ourway.example.com" -Register
```

### Method 3: Single Binary Installer

```bash
# Download and run the installer binary
curl -sL https://ourway.example.com/installers/ourway-installer-linux-amd64 -o /tmp/ourway-installer
chmod +x /tmp/ourway-installer
/tmp/ourway-installer --server wss://ourway.example.com --register
```

### Method 4: Docker

```bash
docker run -d --name ourway-agent \
  -e OURWAY_SERVER=wss://ourway.example.com \
  -e OURWAY_DEVICE_KEY=YOUR_KEY \
  ourway/agent:latest
```

## New Features

### Auto-Registration (--register)
The install scripts can now automatically register a device with the server and obtain a device key. This eliminates the need to manually create a device in the web UI and copy the key.

### Binary Download Endpoint
The server now exposes a `/api/agent/binary` endpoint that serves the correct agent binary based on `os` and `arch` query parameters:
```
GET /api/agent/binary?os=linux&arch=amd64
```

### Cross-Platform Build Script
The `docker/build-agents.sh` script now builds:
- Agent binaries for all platforms (Linux amd64/arm64, macOS amd64/arm64, Windows amd64)
- Installer CLI for all platforms
- Copies install scripts to the dist folder

### Docker Image for Agent
A new `docker/Dockerfile.agent` allows running the agent in a container, which is useful for:
- Testing the agent
- Running the agent on containerized hosts
- Development

## Installation Options

| Option | Description | Default |
|--------|-------------|---------|
| `--server URL` | OurWay server URL | http://localhost:8080 |
| `--key KEY` | Device key (or use --register) | (none) |
| `--register` | Auto-register device with server | (off) |
| `--install-dir DIR` | Installation directory | /opt/ourway (Linux/macOS), C:\Program Files\OurWay\Agent (Windows) |
| `--skip-service` | Don't install as a service | (off) |
| `--version VER` | Agent version to install | latest |

## Files Changed

| File | Description |
|------|-------------|
| `install.sh` | New one-line Bash installer |
| `install.ps1` | New PowerShell installer |
| `cmd/ourway-installer/main.go` | New single-binary installer |
| `cmd/ourway-installer/go.mod` | Module file for installer |
| `server/api/handlers.go` | Added `/api/agent/binary` endpoint |
| `docker/Dockerfile.web` | Now serves install scripts |
| `docker/Dockerfile.agent` | New Docker image for the agent |
| `docker/build-agents.sh` | Updated to build all binaries and installers |
| `docker/docker-compose.yml` | Added optional agent service |
| `agent/config/config.go` | Fixed default server URL |
| `README.md` | Updated installation instructions |

## Development Workflow

For local development:

1. Build all binaries:
   ```bash
   docker/build-agents.sh
   ```

2. Start the server:
   ```bash
   cd server && go run .
   ```

3. Install the agent locally:
   ```bash
   ./install.sh --server http://localhost:8080 --register
   ```
