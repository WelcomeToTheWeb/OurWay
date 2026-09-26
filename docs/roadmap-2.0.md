# OurWay RMM 2.0 Roadmap

**Version:** 2.0  
**Target Release:** Q2 2027  
**Status:** Phases 1-3 & 4.1-4.2 Complete, Phase 4.3 In Progress, Phase 4.4 Complete  

## Vision

OurWay 2.0 transforms from a solid monitoring foundation into a complete RMM platform. The focus is on expanding remote management capabilities (sessions, patching, file transfer), professional-grade access control (RBAC, SSO), and true cross-platform support (macOS agent).

The guiding principle: **deepen what makes OurWay great (lightweight, fast, real-time) while adding the professional features MSPs and IT teams expect from enterprise RMM tools.**

---

## Release Phases

### Phase 1: Foundation & Scale (Weeks 1-4)

**Goal:** Strengthen the platform core, complete macOS support, and establish scalability foundations.

**Phase 1 Status:** ✅ Complete (RBAC, macOS agent, alert enhancements)

#### 1.1 macOS Agent Support
- Complete macOS agent binary (arm64 + x64)
- launchd service installation and management
- macOS-specific metrics: energy usage, disk encryption status, Spotlight indexing status
- macOS software inventory (installed apps, versions, updates available)
- Package management integration (Homebrew, system updates)
- Gatekeeper and notarization support for distribution

#### 1.2 User Management (RBAC)
- Role-based access control system
- Built-in roles: Admin, Manager, Technician, Viewer
- Custom role creation with granular permissions
- Device group assignments for role-based visibility
- User invitation and onboarding flow
- Audit log for user actions
- Multi-tenant foundation (organization isolation)

#### 1.3 Alert System Enhancements
- Per-device and per-group custom thresholds
- Alert deduplication (suppress repeat alerts within configurable window)
- Alert acknowledgment and assignment workflow
- Alert history and resolution tracking
- Alert templates

### Phase 2: Remote Management (Weeks 5-10)

**Goal:** Build the remote management features that make OurWay a true RMM.  
**Architecture Decisions:**
- Remote Sessions: WebRTC via `pion/webrtc/v3`
- Patching: Full automated management (scan → approve → deploy → reboot → report → rollback)
- File Transfer: Server-mediated HTTP (agent pulls from server)

#### 2.1 Remote Sessions (VNC-like Protocol)
- Custom lightweight remote desktop protocol in Go
- Real-time screen capture and streaming (H.264/WebRTC or custom codec)
- Remote input: keyboard, mouse, clipboard sync
- Session quality modes: low bandwidth, balanced, high quality
- Multi-monitor support
- Session recording and playback (integrate with existing session system)
- Concurrent viewing (multiple observers, one controller)
- Session initiation: from device detail page, from search
- Cross-platform: connect from browser on any OS to any managed device
- Security: session-level authentication, end-to-end encryption
- Performance target: <100ms latency at 1080p/30fps on LAN, <300ms over WAN

**Architecture:**
```
[Browser (WebRTC)] ↕ [Go Session Gateway] ↕ [Agent on Device]
```
- Session Gateway: Go service that bridges WebRTC from browser to WebSocket/TCP to agent
- Agent: Captures screen (platform-specific: GDI on Windows, CoreGraphics on macOS, X11/Wayland on Linux), sends frames, receives input events
- Protocol: Custom binary protocol optimized for remote desktop (delta encoding, compression)

#### 2.2 Software Patching
- Windows: Windows Update integration, group policy management, driver updates
- macOS: Software Update integration, managed updates via MDM profile
- Linux: Package manager integration (apt, yum/dnf, pacman, zypper)
- Patch scanning: Identify missing updates across fleet
- Patch deployment: Staged rollouts (pilot → groups → all)
- Patch scheduling: Maintenance windows per device or group
- Reboot management: Auto-reboot after patching, reboot deferral
- Patch reporting: Compliance dashboards, failure tracking
- Approval workflow: Scan → Report → Approve → Deploy
- Rollback support for failed patches

**Data Model:**
```
software_updates: id, device_id, source, title, version, size, 
                  status, installed_at, error_message, created_at
patch_policies: id, name, scope, schedule, auto_reboot, approval_required
patch_deployments: id, policy_id, status, devices_total, devices_success, 
                   devices_failed, started_at, completed_at
```

#### 2.3 File Transfer
- Push files to single device or device group
- Pull files from device to server
- Drag-and-drop file selection in web UI
- Upload progress and status tracking
- Large file support (chunked upload, resumable)
- Directory support (zip/unzip on device)
- File type validation and size limits
- Recent transfers history
- Download links for retrieved files (expiring tokens)

### Phase 3: Authentication & Integration (Weeks 11-14)

**Phase 3 Status:** ✅ Complete (SSO OAuth 2.0, JIT provisioning, webhooks, rate limiting)

**Goal:** Enterprise-grade authentication and third-party integrations.

#### 3.1 SSO Integration
- ✅ OAuth 2.0 provider support (Google, Microsoft, Apple)
- [ ] SAML 2.0 IdP integration (for enterprise SSO)
- ✅ OpenID Connect (OIDC) support
- ✅ Just-in-time user provisioning (auto-create users on first SSO login)
- ✅ SSO session timeout and refresh policies
- ✅ SSO configuration UI
- ✅ Fallback to local authentication

**User Model Changes:**
```
users: ... + provider (local/oauth/saml), provider_id, sso_attributes (JSON)
sso_providers: id, type, name, config (JSON), enabled, created_at
```

#### 3.2 API v2 & Webhooks
- ✅ Versioned API at `/api/v2/` (breaking changes allowed)
- ✅ Rate limiting per user/API key (in-memory sliding window)
- ✅ Webhook system for external integrations
  - ✅ Event types: device_registered, device_online, alert_created, 
    alert_resolved, patch_deployed, session_started
  - ✅ Webhook configuration UI
  - ✅ Retry logic and delivery tracking
- ✅ API key management (scoped keys, rotation, revocation)
- GraphQL endpoint (optional, evaluate demand)

### Phase 4: Polish & Release (Weeks 15-18)

**Goal:** Performance, testing, documentation, and release preparation.

#### 4.1 Scalability Enhancements
- ✅ Redis integration for:
  - ✅ Rate limiting (Redis-backed sliding window, falls back to in-memory)
  - ✅ Caching (device status with 30s TTL, cache invalidation on updates)
  - ✅ WebSocket state sharing (horizontal scaling via Redis pub/sub)
  - Session storage
- ✅ Time-series database migration for historical metrics (metric_history table with time-indexed queries)
- ✅ Metrics retention policies (hot: 7 days, warm: 30 days, cold: 365 days)
- ✅ Database connection pooling optimization (100 max open, 20 idle, 30min lifetime)
- ✅ Load testing (scale test tool, 10,000+ device simulation, ~100 req/s sustained with 0 failures)

#### 4.2 Frontend Enhancements
- ✅ Dark mode toggle (system preference detection)
- ✅ Responsive mobile layout
- ✅ Keyboard navigation and shortcuts
- ✅ Improved accessibility (WCAG 2.1 AA compliance)
- ✅ Virtual scrolling for large device lists
- ✅ Optimistic UI updates
- ✅ Internationalization (i18n) framework (en, de, fr)

#### 4.3 Testing & Quality
- Unit test coverage target: 80%+ (currently 23.7%)
- ✅ 42 API unit tests (device, auth, patching, files, alerts, webhooks, users, API keys)
- ✅ Alert engine tests (6 tests, 86.1% coverage)
- ✅ JWT auth tests (6 tests, 80.8% coverage)
- ✅ Device cache tests (5 tests)
- ✅ Redis cache tests (4 tests, skipped when Redis unavailable)
- ✅ Integration test suite (end-to-end registration, heartbeat, alerts flow)
- ✅ Load testing tool (Go-based, concurrent heartbeat simulation)
- End-to-end tests with Playwright
- Agent smoke tests on all platforms
- Performance benchmarks
- Security audit (OWASP Top 10)

#### 4.4 Documentation & Release
- Complete API documentation
- User guide updates
- Migration guide from 1.x to 2.0
- Upgrade path documentation
- Release notes and changelog
- Marketing assets and blog post

---

## Technical Architecture Changes

### 2.0 Architecture Diagram
```
[Browser (WebRTC + WebSocket)]
         ↕
[Nginx Load Balancer]
    ↕        ↕        ↕
[Server 1] [Server 2] [Server 3]  (horizontal scaling)
    ↕        ↕        ↕
[Redis Cluster] (sessions, cache, pub/sub, rate limiting)
    ↕
[PostgreSQL Primary] ←→ [PostgreSQL Read Replicas]
    ↕
[Time-series DB] (historical metrics)
```

### New Components
- **Session Gateway**: Go service for remote desktop bridging
- **Patch Engine**: Go service for update scanning and deployment
- **Webhook Dispatcher**: Background service for event delivery

### Database Changes
- Migration framework for 1.x → 2.0
- New tables: roles, permissions, role_assignments, audit_logs, 
  sso_providers, webhooks, webhook_deliveries, software_updates,
  patch_policies, patch_deployments, file_transfers, remote_sessions
- Schema versioning and rollback support

---

## Feature Priorities

| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| P0 | macOS Agent Support | Medium | High |
| P0 | User Management (RBAC) | Medium | High |
| P0 | Remote Sessions | High | Critical |
| P1 | Software Patching | High | High |
| P1 | SSO Integration | Medium | High |
| P1 | Alert Enhancements | Medium | Medium |
| P2 | File Transfer | Low | Medium |
| P2 | Webhooks & API v2 | Medium | Medium |
| P2 | Scalability (Redis) | Medium | High |
| P3 | Frontend Enhancements | Medium | Medium |
| P3 | i18n | Low | Low |

---

## Open Questions

1. **Remote Session Protocol**: Custom Go protocol vs. WebRTC standard? Custom gives more control but more work.
2. **Patch Engine Scope**: Full patch management vs. update notification + manual trigger?
3. **Multi-tenancy**: True multi-tenant from day one, or single-tenant with org structure?
4. **Mobile App**: Native mobile app for 2.0 or defer to 3.0?
5. **Pricing Model**: Free tier, per-device pricing, or feature-tiered?
6. **Marketplace**: Plugin/extension system for 2.0 or later?

---

## Success Metrics for 2.0

- Support 10,000+ devices per instance
- Remote session latency <300ms (WAN)
- Patch deployment success rate >95%
- SSO login success rate >99.9%
- Uptime: 99.9% (SLA)
- Test coverage: 80%+
- Zero critical security vulnerabilities at release

---

## Team & Resources

| Role | Count | Focus |
|------|-------|-------|
| Backend Engineers | 3 | API, auth, patching, sessions |
| Frontend Engineers | 2 | UI/UX, remote sessions, dashboards |
| Agent Engineer | 1 | Cross-platform agent, macOS |
| DevOps Engineer | 1 | Infrastructure, scaling, CI/CD |
| QA Engineer | 1 | Testing, automation |

**Total estimated effort:** 5-6 engineers × 4.5 months

---

*Last updated: 2026-09-25 (Phase 4.1 Complete: Redis integration, rate limiting, caching, WS state sharing, time-series DB, retention policies, load testing)*
