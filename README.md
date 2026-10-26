# OurWay RMM

Cross-platform Remote Monitoring and Management tool for Windows, Linux, and macOS. Focused on automation, remote sessions, patching, and a sleek UI/UX — combining the best of all top RMMs with a focus on technology over monetization.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend API | Go (Gin framework) |
| Agent | Go (gopsutil for system metrics) |
| Frontend | React + TypeScript + Tailwind CSS |
| Database | PostgreSQL |
| Real-time | WebSocket (nhooyr.io/websocket) |
| Remote Sessions | Custom VNC-like protocol in Go |
| Deployment | Docker Compose |
| Monitoring UI | Recharts for charts |

## Architecture

```
[Agent on Windows/Linux/macOS]
    ↕ WebSocket (persistent, TLS)
[Go Backend: API + WebSocket Hub]
    ↕ REST + WS events
[React Web UI]
[PostgreSQL]
```

### Agent Features
- Single static binary per platform
- Heartbeat every 15s, full metrics every 60s
- Streaming mode: sends metrics every 2s when detail page is open
- Collects: CPU, RAM, disk (usage + I/O), network, uptime, processes
- OS-specific: Windows services, Linux systemd, macOS launchd
- Configured via env vars or config file

### Backend Features
- REST API for CRUD operations
- WebSocket hub for real-time communication
- Device registry and inventory
- Alert engine (threshold-based) with auto-resolution
- Event stream for UI updates
- Authentication (JWT + refresh tokens, RBAC roles)
- Remote sessions (agent frame relay over HTTP, input over the device WebSocket)
- Native Windows remote-control viewer (`cmd/ourway-viewer`), launched via `ourway://`
- Patching (scan, deploy, policies) for Windows, macOS, and Linux
- File transfer (push/pull with transfer tracking)
- SSO (OpenID Connect, OAuth 2.0)
- Webhooks with HMAC-SHA256 signatures and delivery retries
- API keys with read/write scopes
- Rate limiting (in-memory or Redis token bucket)
- Redis-backed scaling (cache, rate limits)

### Frontend Features
- Dashboard with overview of all devices
- Device list with realtime status updates
- Device detail page with real-time charts
- Remote session viewer (browser) plus the native Windows viewer
- Alert center with acknowledge/assign/resolve
- Patch manager (updates, deployments, policies)
- File transfer with drag-and-drop and download links
- User management (create, roles, delete)
- SSO provider configuration
- Webhook management with delivery history
- Sessions page for active remote sessions
- Settings and configuration

## Project Structure

```
ourway/
├── server/
│   ├── main.go
│   ├── config/
│   ├── api/              # REST handlers
│   ├── ws/               # WebSocket hub
│   ├── models/           # Data models
│   ├── store/            # Database layer
│   ├── auth/             # JWT authentication
│   └── alerts/           # Alert engine
├── agent/
│   ├── main.go
│   ├── collector/        # System metric collectors
│   │   ├── cpu.go
│   │   ├── memory.go
│   │   ├── disk.go
│   │   ├── network.go
│   │   └── processes.go
│   ├── client/           # WebSocket client
│   ├── install/          # Platform-specific install
│   │   ├── windows.go    # Service registration
│   │   ├── linux.go      # systemd unit
│   │   └── darwin.go     # launchd plist
│   └── config/
├── web/
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx
│   │   │   ├── Devices.tsx
│   │   │   ├── DeviceDetail.tsx
│   │   │   ├── Alerts.tsx
│   │   │   └── Settings.tsx
│   │   ├── hooks/
│   │   ├── api/
│   │   ├── stores/       # State management
│   │   └── App.tsx
│   └── package.json
├── docker/
│   ├── Dockerfile.server
│   ├── Dockerfile.web
│   └── docker-compose.yml
├── cmd/
│   ├── ourway-cli/       # CLI for agent install/manage
│   ├── ourway-installer/ # single-binary installer (all platforms)
│   └── ourway-viewer/    # native Windows remote-control viewer (Gio)
├── docs/
└── README.md
```

## Quick Start

### From pre-built images (no clone required)

```bash
curl -sL https://raw.githubusercontent.com/WelcomeToTheWeb/OurWay/main/docker-compose.prod.yml -o docker-compose.yml
docker compose up -d

# Open web UI
open http://localhost:3000
```

### From source

```bash
git clone https://github.com/WelcomeToTheWeb/OurWay.git
cd OurWay

# Run with Docker Compose
docker compose -f docker/docker-compose.yml up -d

# Open web UI
open http://localhost:3000
```

## Installing the Agent

The agent can be installed with a single command. It will automatically detect your OS and architecture, download the correct binary, and install it as a service.

### Linux / macOS (one-liner)

```bash
# Basic install (auto-registers device with server)
curl -sL https://ourway.example.com/install.sh | bash -s -- --server wss://ourway.example.com --register

# Or provide an existing device key
curl -sL https://ourway.example.com/install.sh | bash -s -- --server wss://ourway.example.com --key YOUR_DEVICE_KEY

# For development (installs from local build)
./install.sh --server http://localhost:8080 --register
```

### Windows

```powershell
# From PowerShell (run as administrator)
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Server "wss://ourway.example.com" -Register
```

### Docker

```bash
# Run the agent in a container (for testing or containerized hosts)
docker run -d --name ourway-agent \
  -e OURWAY_SERVER=wss://ourway.example.com \
  -e OURWAY_DEVICE_KEY=YOUR_KEY \
  ourway/agent:latest
```

### Installation Options

| Option | Description | Default |
|--------|-------------|---------|
| `--server URL` | OurWay server URL | http://localhost:8080 |
| `--key KEY` | Device key (or use --register) | (none) |
| `--register` | Auto-register device with server | (off) |
| `--install-dir DIR` | Installation directory | /opt/ourway |
| `--skip-service` | Don't install as a service | (off) |
| `--version VER` | Agent version to install | 1.0.0 |

### Uninstalling

The installer leaves an uninstaller next to the agent. It removes the service,
the agent and remote-control binaries, config, logs and (on Windows) the
Add/Remove Programs entry.

```bash
# Linux / macOS
sudo ourway-uninstall            # or: sudo /opt/ourway/uninstall.sh

# Windows: Settings > Apps > OurWay Agent > Uninstall
# or run:  "C:\Program Files\OurWay\Agent\uninstall.cmd"
```

All of these run `ourway-agent --uninstall` (add `--yes` to skip the confirmation
prompt for scripted removal). On Windows it asks for administrator approval.
Files you added to the install directory are left alone.

## Roadmap

**Active plan: [`docs/roadmap-2.x.md`](docs/roadmap-2.x.md)** — the next four
releases (2.2.0 make the claims true · 2.3.0 MSP tenancy + audit log · 2.4.0
alert thresholds, notification channels, generic OIDC + SAML · 2.5.0
self-host observability), what each must pass before it is tagged, and what is
deliberately deferred. Task breakdowns with acceptance criteria:
[`docs/phase2.2-tasks.md`](docs/phase2.2-tasks.md),
[`docs/phase2.3-tasks.md`](docs/phase2.3-tasks.md),
[`docs/phase2.4-tasks.md`](docs/phase2.4-tasks.md) and
[`docs/phase2.5-tasks.md`](docs/phase2.5-tasks.md).
[`docs/roadmap-2.0.md`](docs/roadmap-2.0.md) is kept as the 2.0 record.

### MVP (v1.0)

#### Phase 1: Foundation (Week 1-2)
- [x] Set up Go backend with Gin
- [x] Set up PostgreSQL with migrations
- [x] Create device model and CRUD API
- [x] Build basic React frontend with auth
- [x] WebSocket hub for real-time communication

#### Phase 2: Agent Core (Week 2-3)
- [x] Go agent with gopsutil integration
- [x] CPU, RAM, disk, network collectors
- [x] WebSocket client connecting to server
- [x] Heartbeat and metrics reporting
- [x] Platform-specific install scripts (Windows service, systemd, launchd)

#### Phase 3: Monitoring UI (Week 3-4)
- [x] Dashboard with device grid and status
- [x] Device detail page with charts
- [x] Real-time streaming mode (2 s metrics while a detail page is open)
- [x] Alert configuration and display

#### Phase 4: Polish & Release (Week 4-5)
- [x] Docker Compose setup
- [x] CLI tool for agent installation
- [x] Documentation
- [x] Testing and bug fixes

### Version 2.0

See the full [2.0 Roadmap](docs/roadmap-2.0.md) for details. Implemented:

- ✅ **Remote Sessions**: agent frame relay with browser and native Windows viewers
  (control is implemented on Windows, Linux and macOS — macOS needs the cgo agent
  build and an Accessibility grant, see Known gaps)
- ✅ **Streaming mode**: detail page switches the agent to 2 s metric intervals
- ✅ **Patching**: OS update management for Windows, macOS, and Linux (scan, deploy, policies)
- ✅ **User Management**: RBAC with roles and permissions
- ✅ **SSO Integration**: OpenID Connect and OAuth 2.0
- ✅ **macOS Support**: Complete agent with launchd service and native metrics
- ✅ **File Transfer**: Push/pull files with drag-and-drop and download links
- ✅ **Webhooks**: Event subscriptions with HMAC signatures and retry queue
- ✅ **API Keys**: Scoped (read/write) long-lived credentials
- ✅ **Scalability**: Redis-backed cache and distributed rate limiting
- ⬜ **Automation**: Runbooks, scheduled scripts, workflows

## Known gaps

Honest list of what the product advertises and does not yet do, so nobody has to
discover it in production. Each item is tracked in
[`docs/roadmap-2.x.md`](docs/roadmap-2.x.md).

- **macOS remote control needs two conditions on the unit.** Control is implemented
  on macOS (CoreGraphics `CGEvent` posting), but it lands only on a **cgo** agent
  build (`CGO_ENABLED=1` on a macOS builder) with **Accessibility** granted on that
  Mac (`ourway-agent --grant-input-permission`). A `CGO_ENABLED=0` cross-compile
  reports `unavailable`, and a unit without the grant reports `denied` — the unit
  view and the session panel state which one it is, so nobody discovers it live.
  macOS screen capture additionally needs the Screen Recording grant.
- **SAML is not implemented.** SSO is OAuth 2.0 / OIDC; the API rejects a
  `saml`-typed provider at create, authorize, and callback.
- **SSO presets are the big three.** The flow is generic OAuth2 and the create API
  accepts `auth_url`/`token_url`/`user_info_url` overrides, but the console's
  provider list is hardcoded to Google, Microsoft and Apple, there is no OIDC
  discovery from an issuer, and `id_token` signature/`iss`/`aud` are not verified.
  Self-hosted IdPs (Keycloak, Authentik, Zitadel, Okta, a fixed-tenant Entra ID) are
  a manual paste-the-URLs exercise. Planned in 2.4.0 — see `docs/roadmap-2.x.md`.
- **macOS patch rollback is unsupported.** `agent/patch` deploys on macOS but
  refuses rollback; packages are restored manually (brew pin/reinstall).
- **Linux patching covers apt and yum/dnf only** — no pacman or zypper.
- **No client/tenant isolation.** Access scope is a global role, so a technician
  sees every device in the installation. Device tags scope patch policies, not
  visibility.
- **Alert thresholds are global.** One threshold set per metric for the whole
  installation; no per-device, per-group, or per-tag overrides.

## Key Design Decisions

1. **Streaming vs Snapshots**: Agents send snapshots every 60s. When a user opens a device detail page, the server tells the agent to switch to streaming mode (2s intervals). When the user leaves, it reverts.

2. **Alerting**: Threshold-based rules defined per metric type. Alerts trigger WebSocket notifications to the UI.

3. **Authentication**: short-lived JWT access tokens (revocable) for API auth. Agents
   authenticate with their device key; remote-control binaries with a per-session token.
   WebSocket credentials travel in `Sec-WebSocket-Protocol`, never in the URL.

4. **Cross-platform Agent**: Uses Go's build tags for OS-specific code. gopsutil handles 80% of the work cross-platform.

## License

MIT
