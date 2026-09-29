# Changelog

All notable changes to the OurWay RMM project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-09-25

### Added
- **macOS Agent Support** (arm64 + x64) with native metrics collectors
- **Role-Based Access Control (RBAC)** with 4 built-in roles (Admin, Manager, Technician, Viewer)
- **WebRTC Remote Sessions** with view/control modes and capture quality settings
- **Software Patch Management** with scanning, policies, deployment, and rollback
- **File Transfer** with server-mediated push/pull and drag-and-drop upload
- **SSO Integration** (OAuth 2.0 / OpenID Connect) for Google, Microsoft, and Apple
- **API v2** endpoints for webhooks and API keys at `/api/v2/`, with rate limiting (100 req/min per user)
- **Webhook System** with 8 event types, retry logic, and delivery tracking
- **API Key Management** with scoped keys, rotation, and revocation
- **Alert Enhancements**: deduplication, acknowledgment, assignment
- **Dark Mode** with light/dark/system options
- **Responsive Mobile Layout** with hamburger menu
- **Keyboard Shortcuts** with chord-based navigation
- **Internationalization (i18n)** with English, German, and French support
- **Virtual Scrolling** for large device lists
- **Redis Integration** for rate limiting, caching, and WebSocket state sharing
- **Time-Series Metrics Storage** with retention policies
- **Load Testing** validated for 10,000+ device simulation

**API Key Management**
- Scoped API keys with `owk_` prefix (SHA-256 hashed storage)
- API key authentication via `X-API-Key` header or `Bearer owk_...` format
- Key scopes: read, write (configurable per key)
- Key expiration: 1h, 24h, 7d, 30d, 90d, or never
- Key rotation (generates new key, invalidates old)
- Key revocation
- Frontend UI in Settings page for key management
- API endpoints: create, list, get, update, delete, revoke, rotate
- 6 new API key tests

**Testing Enhancements**
- Webhook API tests (create, list, get, update, delete)
- User management API tests (list, update, delete)
- Total test count: 36 (up from 23)
- Test coverage: 22.3% (up from 19%)

### Phase 4.2 - Frontend Enhancements
**Dark Mode**
- Full dark mode support with light/dark/system options
- Theme persistence via localStorage
- System preference detection (prefers-color-scheme)
- Smooth theme transitions

**Responsive Layout**
- Mobile sidebar with hamburger menu toggle
- Responsive grids on Dashboard and Devices pages
- Responsive tables on Alerts and Users pages
- Mobile-friendly filter controls

**Keyboard Shortcuts**
- Chord-based navigation: g+h (Home), g+d (Devices), g+a (Alerts), g+f (Files), g+p (Patches), g+u (Users), g+s (Settings)
- Shortcut documentation in Settings page

**Accessibility (WCAG 2.1 AA)**
- Skip-to-content link
- Focus-visible indicators on all interactive elements
- ARIA labels on key elements
- Semantic HTML structure

**Internationalization**
- i18next framework with react-i18next
- 3 locales: English (en), German (de), French (fr)
- Language selector in Settings with persistence
- Browser language detection

**Performance**
- Virtual scrolling for large device lists (react-window Grid)
- Optimistic UI updates for alert acknowledgment and device actions

### Phase 3
**SSO Integration**
- OAuth 2.0 authentication with Google, Microsoft, and Apple
- OpenID Connect support
- Just-in-time (JIT) user provisioning on first SSO login
- SSO provider configuration API and admin UI
- SSO sign-in buttons on login page
- CSRF protection with state parameter

**API v2 & Webhooks**
- Versioned API endpoints for webhooks and API keys at `/api/v2/`
- Webhook system with 8 event types: device_registered, device_online, device_offline, alert_created, alert_resolved, patch_deployed, session_started, session_frame
- Webhook delivery with exponential backoff retry (max 5 attempts)
- Delivery tracking and history
- Webhook test delivery
- Webhook management API and admin UI
- Per-user rate limiting (100 requests/minute)

### Phase 1
**RBAC**
- Role-based access control system with 4 built-in roles (Admin, Manager, Technician, Viewer)
- JWT tokens now include user roles
- RBAC middleware for route protection
- Role and user management API endpoints
- New local users are automatically assigned the viewer role at registration

**macOS Agent Support**
- Complete macOS agent support (arm64 and x64)
- macOS-specific collectors: energy/battery, disk encryption (FileVault), software inventory
- Platform-specific collector injection system
- Docker build configuration for macOS agents

**Alert System Enhancements**
- Alert deduplication (5-minute configurable window)
- Alert acknowledgment and assignment
- Configurable thresholds per metric type
- New API endpoints: acknowledge, assign alerts

### Phase 2
**Remote Sessions**
- WebRTC-based remote screen sharing and control
- Platform-specific screen capture (Linux, macOS, Windows)
- Real-time remote input (mouse/keyboard)
- Quality controls (capture quality)
- View/control mode switching
- Session management API

**Software Patching**
- Automated update scanning via native package managers
- Platform support: apt/yum (Linux), softwareupdate/brew (macOS), Windows Update
- Patch policies with scheduling and batching
- Deployment management with progress tracking
- Reboot management
- Device and deployment rollback support
- API endpoints: scan, deploy, rollback, reboot

**File Transfer**
- Server-mediated file push and pull
- Progress reporting from agent
- Drag-and-drop file upload
- Transfer history with status
- Multipart file upload support
- API endpoints: upload, push, pull, download, status

**Testing**
- Unit tests for patching APIs (scan, deploy, rollback, policies)
- Unit tests for file transfer APIs (upload, status, transfers)
- 11 new API tests added

## [1.0.0] - 2026-09-24

### Added

**Server**
- REST API with JWT authentication (access tokens: 24h, refresh tokens: 7d)
- Device management endpoints (register, list, query, update, remove)
- Metrics collection and storage (CPU, memory, disk, network)
- Alert engine with configurable thresholds (CPU: 80/90%, RAM: 85/95%, Disk: 85/95%)
- Alert severity levels: info, warning, critical
- WebSocket hub for real-time communication (`/ws` endpoint)
- Health check endpoint
- PostgreSQL 16 database with GORM ORM
- Graceful shutdown handling
- Environment-based configuration

**Agent**
- Cross-platform support (Linux x64/arm64, Windows x64)
- Heartbeat monitoring (15s interval)
- Metrics collection (60s interval)
- Platform-specific collectors (procfs for Linux, WMI for Windows)
- Service installation (systemd on Linux, Windows service on Windows)
- Logging with file rotation
- Auto-reconnect with backoff
- Graceful shutdown

**CLI**
- `ourway-cli register` - Register a new device with the server
- `ourway-cli install` - Install the OurWay agent on this machine
- `ourway-cli uninstall` - Remove the OurWay agent
- `ourway-cli status` - Show the status of the OurWay agent
- `ourway-cli logs` - Show OurWay agent logs
- `ourway-cli version` - Display version information
- `--server` flag to select the server URL (default http://localhost:8081)

**Frontend**
- Device dashboard with real-time status
- Device details page with metrics charts
- Alerts page with filtering
- Authentication pages (login/register)
- User settings
- Built with React 18, TypeScript, and Vite

**Docker**
- Multi-stage Dockerfile for server
- Multi-stage Dockerfile for each agent platform (linux-x64, linux-arm64, windows-x64)
- Docker Compose configuration with PostgreSQL 16
- Build script for all Docker images

### Fixed
- (No fixes in initial release)

### Security
- JWT-based authentication
- Password hashing
- CORS configuration
- Secure cookie options for frontend
- TLS support for agent communication (configurable)

[unreleased]: https://github.com/your-org/ourway-rmm/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/your-org/ourway-rmm/releases/tag/v1.0.0
