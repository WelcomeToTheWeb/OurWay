# OurWay RMM 2.0 Release Notes

**Version:** 2.0.0  
**Release Date:** September 25, 2026  
**Status:** Production Ready

---

## Introduction

OurWay RMM 2.0 transforms from a solid monitoring foundation into a complete Remote Monitoring and Management platform. This release adds the professional-grade features MSPs and IT teams expect: remote sessions, automated patching, file transfer, enterprise SSO, and role-based access control.

**Download:** https://releases.ourway.io/  
**Documentation:** https://docs.ourway.io/

---

## What's New

### 🖥️ Remote Sessions
Real-time screen sharing and control via WebRTC:
- View and control any managed device from your browser
- Adjust resolution, FPS, and quality settings
- Session recording with playback
- Concurrent viewing (multiple observers, one controller)
- Cross-platform: connect from any OS to any managed device

### 🔧 Automated Patch Management
Complete software update lifecycle management:
- Scan devices for missing updates (apt, yum, softwareupdate, Windows Update)
- Create patch policies with scheduling and batching
- Approval workflow: Scan → Report → Approve → Deploy
- Deployment progress tracking
- Automatic rollback for failed patches
- Maintenance window scheduling

### 📁 File Transfer
Push and pull files with ease:
- Drag-and-drop file selection in the web UI
- Transfer progress and status tracking
- Large file support with resumable uploads
- Recent transfers history

### 🔐 Enterprise SSO
Log in with your identity provider:
- Google, Microsoft, and Apple SSO support
- Just-in-time user provisioning
- SSO configuration UI
- Fallback to local authentication

### 👥 Role-Based Access Control
Fine-grained permissions for your team:
- Built-in roles: Admin, Manager, Technician, Viewer
- Custom role creation
- Device group assignments
- Audit-ready permission model

### 📱 Frontend Enhancements
A better, faster, more accessible interface:
- **Dark Mode**: Full light/dark theme support with system detection
- **Responsive Design**: Fully mobile-friendly layout
- **Keyboard Shortcuts**: Chord-based navigation (g+d, g+a, etc.)
- **Virtual Scrolling**: Smooth scrolling for thousands of devices
- **i18n**: English, German, and French
- **WCAG 2.1 AA Compliance**: Screen reader support, focus management

### 📊 Scalability & Performance
Built to scale with your fleet:
- Redis-backed rate limiting, caching, and WebSocket state sharing
- Horizontal scaling support (multiple server instances behind a load balancer)
- Time-series metrics storage with retention policies
- Validated for 10,000+ devices per instance

### 🔔 Alert Enhancements
Smarter alerting with less noise:
- Alert deduplication (5-minute configurable window)
- Alert acknowledgment and assignment workflow
- Per-device and per-group custom thresholds
- Alert history and resolution tracking

### 🚀 API & Integrations
- Versioned API v2 at `/api/v2/`
- Rate limiting (100 requests/minute per user)
- Webhook system with 6 event types and retry logic
- Scoped API keys with rotation and revocation

---

## System Requirements

### Server
| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4 cores |
| RAM | 2 GB | 4 GB |
| Disk | 10 GB | 50 GB |
| PostgreSQL | 14 | 16 |
| Redis | 6.0 (optional) | 7.x |

### Agent
| Platform | Requirements |
|----------|-------------|
| Linux | systemd, x86_64 or arm64 |
| macOS | 10.15+, Intel or Apple Silicon |
| Windows | 10+, x86_64 |

### Web Dashboard
| Browser | Minimum Version |
|---------|----------------|
| Chrome | 100 |
| Firefox | 100 |
| Safari | 15 |
| Edge | 100 |

---

## Upgrade Instructions

### From 1.x to 2.0

1. **Back up your database:**
   ```bash
   pg_dump -U ourway ourway > ourway-backup.sql
   ```

2. **Stop the server:**
   ```bash
   sudo systemctl stop ourway-server
   ```

3. **Download and install 2.0:**
   ```bash
   curl -L https://releases.ourway.io/server/v2.0.0/ourway-server-linux-amd64 \
     -o /opt/ourway/server/ourway-server
   chmod +x /opt/ourway/server/ourway-server
   ```

4. **Start the server:**
   ```bash
   sudo systemctl start ourway-server
   ```

5. **Upgrade your agents** (see agent upgrade guide)

See the full [Migration Guide](migration-guide.md) for details.

### Fresh Install

```bash
# Clone and run with Docker Compose
git clone https://github.com/ourway-rmm/ourway.git
cd ourway
docker compose up -d

# Open web UI
open http://localhost:3000
```

---

## Known Issues

| Issue | Description | Workaround |
|-------|-------------|-----------|
| SAML 2.0 Support | SAML is not yet implemented (OAuth 2.0 only) | Use OAuth 2.0 providers |
| GraphQL Endpoint | Planned but not yet available | Use REST API |
| Mobile App | Planned for v2.1 | Use mobile browser |
| Plugin System | Planned for v3.0 | Use webhooks |
| Self-Signed Certs | Agent verification may fail | Add CA to system trust store |

---

## Breaking Changes

1. **RBAC on all API endpoints** — Ensure integrations use users with appropriate roles
2. **New database tables** — Automatic migration on startup (back up first)
3. **Alert deduplication** — Duplicate alerts within 5 minutes are suppressed

---

## Community & Support

- **GitHub:** https://github.com/ourway-rmm/ourway
- **Documentation:** https://docs.ourway.io/
- **Issues:** https://github.com/ourway-rmm/ourway/issues
- **Community Discord:** https://discord.gg/ourway (coming soon)

---

## License

OurWay RMM is released under the MIT License.

---

*Thank you for using OurWay RMM! We're building the future of remote monitoring and management.*
