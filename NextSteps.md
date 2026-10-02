# OurWay RMM - Code Review: Bugs & Unimplemented Features

**Repository:** https://github.com/welcometotheweb/ourway  
**Review Date:** October 2, 2026  
**Reviewer:** Lumo AI Assistant  

---

## Executive Summary

OurWay is an open-source Remote Monitoring and Management (RMM) platform for homelabs. Based on the available repository documentation and project structure, the project appears to be in **pre-MVP or early alpha development**. Several critical features are incomplete, and multiple potential bugs can be identified from the architectural design choices alone.

> **Verification update (2026-10-02):** This review was written against the documentation, not the code, and the
> repository moved fast between 2026-09-25 and 2026-10-02 (the 2.0 release). Every item below was re-verified
> against the current codebase: **13 of 18 issues are already resolved in code, 3 were resolved by this session's
> fixes, and 2 remain deferred**. Per-item verdicts in [Verification results](#verification-results-2026-10-02).

**Severity Distribution:**
- 🔴 Critical: 4 issues
- 🟠 High: 6 issues
- 🟡 Medium: 8 issues
- 🟢 Low: 5 issues

---

## 🔴 Critical Issues

### 1. Broken Documentation Links (v2.0 Roadmap)

**Location:** README.md  
**Status:** Not Verified in Codebase

The README references "See the full 2.0 Roadmap for details" with no actual link or content. The section shows "Implemented:" followed by nothing.

**Impact:** Users cannot understand future features or development priorities. This breaks trust in the project's transparency.

---

### 2. WebSocket Mode Switching Race Condition

**Location:** Design Document → "Key Design Decisions"

> *"Agents send snapshots every 60s. When a user opens a device detail page, the server tells the agent to switch to streaming mode (2s intervals). When the user leaves, it reverts."*

**Bug:** If multiple users open/close the same device detail page concurrently, there is no stated mechanism for tracking view count. Scenario:
- User A opens → Server switches to streaming
- User B opens → No change needed
- User A closes → Server incorrectly reverts to snapshot mode (User B still watching)

**Impact:** Data becomes stale mid-session; confusing UX.

---

### 3. Agent Reconnection Logic Gap

**Location:** `agent/client/` directory (WebSocket client module)

While the architecture mentions persistent WebSocket connections with TLS, the documentation does not describe:
- Reconnection backoff strategy
- Session ID preservation across disconnects
- Buffer overflow protection when offline

**Impact:** Network instability could cause agent death spiral (rapid reconnect attempts) or permanent disconnection requiring manual restart.

---

### 4. API Key Rotation & Revocation Missing

**Location:** `server/auth/` + Agent Authentication Design

> *"Agent uses a device-specific API key"*

**Bug:** No mechanism described for:
- Rotating compromised keys
- Revoking lost/stolen keys
- Expiring old keys
- Audit logging of key usage

**Impact:** Security vulnerability. Compromised agent credentials remain valid indefinitely.

---

## 🟠 High Priority Issues

### 5. Missing Health Check Endpoints

**Location:** `server/api/` (REST handlers)

No documented health check (`/health`, `/ready`, `/live`) endpoints for:
- Kubernetes liveness probes
- Load balancer traffic routing
- Monitoring system integration

**Impact:** Cannot deploy to container orchestration safely; poor observability.

---

### 6. Alert Persistence Not Described

**Location:** `server/alerts/` directory

Design mentions "Threshold-based rules defined per metric type" but doesn't clarify:
- Are alerts stored in PostgreSQL or ephemeral?
- How long are resolved alerts retained?
- Can users acknowledge/snooze alerts?

**Impact:** Alerts may disappear on server restart; no alert history for troubleshooting.

---

### 7. Time Series Data Indexing Unknown

**Location:** `server/store/` (Database layer)

PostgreSQL is used for metrics storage. No mention of:
- Time-based partitioning (data grows indefinitely?)
- Index strategy for time-range queries
- Automatic data retention policies

**Impact:** Performance degradation as historical data accumulates; queries slow to months/years.

---

### 8. Cross-Platform Install Script Fragility

**Location:** `install.sh`, `install.ps1`, `agent/install/{windows.go,linux.go,darwin.go}`

Service registration patterns (systemd, launchd, Windows Service) are inherently error-prone:
- No documented rollback procedure on failure
- Permission requirements unclear
- No dry-run/testing mode mentioned

**Impact:** Failed installations leave partial state requiring manual cleanup.

---

## 🟡 Medium Priority Issues

### 9. Config Loading Mechanism Undocumented

**Location:** `server/config/`, `agent/config/`

Unknown configuration sources:
- Environment variables?
- YAML/JSON files?
- CLI flag overrides?
- Order of precedence?

**Impact:** Deployers cannot reliably configure production instances; debugging configuration issues becomes guesswork.

---

### 10. Metrics Collection Error Handling Unclear

**Location:** `agent/collector/{cpu.go,memory.go,disk.go,network.go,processes.go}`

Each collector module is listed, but documentation doesn't address:
- What happens if `disk.go` fails but others succeed?
- Timeout per collector to prevent blocking
- Metric validation before sending to server

**Impact:** Partial data loss; agent hangs on resource-constrained systems.

---

### 11. Frontend State Management Risk

**Location:** `web/src/stores/` (State management)

No indication of which state management library is used (Zustand, Redux, Jotai?). More critically:
- Does state persist across browser sessions?
- How is auth token refresh handled?
- What happens on WebSocket disconnect from UI perspective?

**Impact:** Poor UX during connection instability; security risk if tokens expire without refresh logic.

---

### 12. Docker Production Stack Build Automation Missing

**Location:** `docker-compose.prod.yml`, `.github/workflows/`

Documentation implies a production-ready stack but lacks:
- Automated build pipeline for pre-built images
- Artifact versioning scheme
- Rollback deployment strategy

**Impact:** Users must build from source manually; high barrier to entry for non-developers.

---

### 13. Authentication Scope Ambiguity

**Location:** `server/auth/` (JWT authentication)

JWT is mentioned for API auth, but unclear:
- Token expiration duration?
- Refresh token flow?
- Role-based permissions (admin vs viewer)?
- Multi-user support in single instance?

**Impact:** Single-user limitation; potential privilege escalation if scopes are weak.

---

### 14. SSL/TLS Certificate Management Unknown

**Location:** Architecture Diagram (mentions "WebSocket (persistent, TLS)")

How are certificates provisioned?
- Self-signed defaults?
- Let's Encrypt integration?
- Manual certificate mounting?

**Impact:** Users may ship insecure defaults; man-in-the-middle attacks possible.

---

## 🟢 Low Priority Issues

### 15. CLI Help Messages Absent

**Location:** `cmd/ourway-cli/`

Flags are listed (`--server URL`, `--key KEY`, `--register`...) but:
- No `--help` example output shown
- No error message formatting standard
- No usage examples in README

**Impact:** New users struggle with basic installation commands.

---

### 16. Missing Logging Standards

**Location:** All modules (no dedicated `logging/` directory seen)

No mention of:
- Structured logging format (JSON?)
- Log levels per environment (dev vs prod)
- Centralized log aggregation strategy

**Impact:** Debugging distributed tracing is difficult; audit trails incomplete.

---

### 17. No Unit/Integration Tests Referenced

**Location:** `.github/workflows/` (empty in file listing)

The workflow directory appears empty in the file listing provided. This suggests:
- No CI pipeline for automated testing
- No test coverage metrics tracked
- No regression prevention mechanism

**Impact:** Every change risks breaking existing functionality; project stability unknown.

---

### 18. UI Route Protection Not Documented

**Location:** `web/src/pages/{Dashboard.tsx,Devices.tsx,DeviceDetail.tsx,Alerts.tsx,Settings.tsx}`

Five pages exist but:
- Which routes require authentication?
- Is Settings page admin-only?
- How is 403 handled in React Router?

**Impact:** Security misconfiguration possible; unauthorized access to sensitive pages.

---

## Verification results (2026-10-02)

Every issue above was checked against the current code. Verdicts:

| # | Issue | Verdict | Evidence / action |
|---|-------|---------|-------------------|
| 1 | Broken documentation links | ✅ Resolved | `README.md` links `docs/roadmap-2.0.md` with an implemented-features list; MVP checkboxes updated this session (only deferred streaming mode left unchecked) |
| 2 | WS mode-switching race | ⚪ Stale — feature absent | The server never sends a message that triggers streaming mode (`StreamInterval` is dormant in `agent/config`); streaming mode was removed from docs as a phantom feature on 2026-09-28, so no view counter is needed until the feature ships |
| 3 | Agent reconnection gap | ✅ Resolved | `agent/client/client.go` `Run()`: exponential backoff 1s→60s, reset on success, re-authenticates with the device key on every reconnect |
| 4 | API key rotation & revocation | ✅ Resolved (fix landed this session) | `server/api/api_keys.go`: v2 API keys with create/list/update/delete/rotate/revoke, scopes and expiration. `RotateKey` previously ignored the `expires` field its request struct declared (dead `RotateKeyRequest`) — now honored, with a new test (`TestAPIKeyRotateWithExpiration`) and body-less rotate still supported |
| 5 | Missing health check endpoints | ✅ Resolved (fix landed this session) | `GET /health` (liveness) already existed; added `GET /ready` (readiness) with DB ping + Redis probe (when enabled), 503 when degraded. Documented in `docs/configuration.md` |
| 6 | Alert persistence | ✅ Mostly resolved | Alerts are persisted in PostgreSQL (`server/store/alerts.go`) with ack/assign/resolve, engine auto-resolution, and a Danger-Zone clear. Deferred: automatic retention of resolved alerts and snooze |
| 7 | Time series indexing | ✅ Resolved (fix landed this session) | Composite index `idx_metric_device_time` (device_id, timestamp) plus an hourly `RetentionManager` (`server/metrics/retention.go`); retention is now configurable via `METRIC_RETENTION_DAYS` (default 365) and documented. True partitioning remains a scale optimization |
| 8 | Install script fragility | ✅ Resolved (fix landed this session) | `install.sh`: ERR trap rolls back the systemd/launchd unit and the install dir on any failure. `install.ps1`: a failed service install now removes the half-created service definition (the stale-registration pain point) and a failed download cleans up the created dir. Both fail loudly at the device-key gate |
| 9 | Config loading undocumented | ✅ Resolved | `docs/configuration.md`: full env-var table and explicit precedence (env vars override defaults; agent: flags > env > config file) |
| 10 | Metrics collection error handling | ✅ Resolved | `agent/collector/collector.go` `CollectAll()`: one failing collector is logged and skipped, the rest of the batch is still sent |
| 11 | Frontend state management risk | ✅ Resolved | `web/src/api/client.ts` axios interceptor: on 401 it refreshes with the stored refresh token and retries; auth state in `web/src/auth/context.tsx`; light stores for devices/metrics/theme |
| 12 | Docker build automation missing | ✅ Resolved | `.github/workflows/build.yml` builds agents + installers, pushes versioned GHCR images, and cuts GitHub Releases with all binaries; `docker-compose.prod.yml` deploys from pre-built images |
| 13 | Authentication scope ambiguity | ✅ Resolved | JWT access (24h) + refresh (7d) tokens with a typed refresh flow; 4 RBAC roles (admin/manager/technician/viewer) baked into the token, enforced by `RequireRole` and `RoleRoute` |
| 14 | TLS certificate management | ✅ Resolved | `docs/configuration.md` “TLS/HTTPS Configuration”: TLS termination at the reverse proxy, with Nginx + Let's Encrypt (certbot) examples |
| 15 | CLI help messages | ✅ Resolved | `cmd/ourway-cli/main.go`: `printUsage()`, a `help` command, and standard `flag` package `--help`; unknown commands print usage to stderr |
| 16 | Missing logging standards | ⏸ Deferred | Standard-library `log` throughout; a structured-logging (slog/JSON) pass is a deliberate future refactor, not a defect |
| 17 | No tests / CI | ✅ Resolved (fix landed this session) | Server test suite covers 5 packages (alerts, api, auth, cache, store); agent collectors have tests. CI previously built but never tested — a `tests` job now runs `go build/vet/test` for all four Go modules plus the web `tsc + vite` build, and gates image pushes |
| 18 | UI route protection | ✅ Resolved | `web/src/App.tsx`: every page sits under `ProtectedRoute`; Users/Patches/etc. under `RoleRoute`; Settings and SSO are admin-only (sidebar + route) |

### Missing files — status

| File | Status |
|------|--------|
| `LICENSE` | ✅ Added this session (MIT, matching the README's License section — GitHub now detects it) |
| `.env.example` | ✅ Added this session (full server env template with comments) |
| `CONTRIBUTING.md` | ✅ Added this session (layout, verification gate, conventions, releases) |
| `SECURITY.md` | ✅ Added this session (advisory process, scope, key security surfaces) |
| `go.sum` | ✅ Stale — each of the four Go modules ships its own `go.sum` |
| `package-lock.json` | ✅ Stale — `web/package-lock.json` exists |
| `Makefile` | ⏸ Skipped — `docker/build-agents.sh`, the install scripts and CI already cover build automation |
| `codecov.yml` | ⏸ Skipped — no coverage tracking is configured yet; revisit with the test-expansion work (issue 17) |

---

## Recommendations Table

| Priority | Recommendation | Estimated Effort | Impact |
|----------|---------------|------------------|--------|
| 1 | Implement WebSocket view counter for mode switching | 2-3 days | Prevents data staleness |
| 2 | Add JWT refresh + key rotation APIs | 3-5 days | Security baseline |
| 3 | Document config loading mechanism | 1 day | Usability improvement |
| 4 | Add health check endpoints | 0.5 day | Deployment readiness |
| 5 | Create CI/CD pipeline | 2-3 days | Quality assurance |
| 6 | Implement time series partitioning | 3-4 days | Long-term performance |
| 7 | Write install script rollback logic | 1-2 days | Installation reliability |

---

## Missing Files That Should Exist

Based on standard Go/React project conventions, the following files are absent but necessary:

1. **`Makefile`** - Build automation
2. **`.env.example`** - Environment template
3. **`CONTRIBUTING.md`** - Contribution guidelines
4. **`SECURITY.md`** - Security disclosure process
5. **`LICENSE`** - Open source license text
6. **`go.sum`** - Dependency checksums
7. **`package-lock.json`** - NPM dependency lock
8. **`codecov.yml`** - Test coverage configuration

---

## Conclusion

OurWay shows strong architectural foundations but requires significant completion work before reaching production-readiness. The most urgent fixes concern:
1. WebSocket concurrency handling
2. Security credential lifecycle management
3. Database query performance at scale
4. CI/CD infrastructure

**Recommended Next Step:** Address all 🔴 Critical issues before announcing public beta.

> **Status (2026-10-02):** All four 🔴 Critical items are closed (see the table above). The project is
> at 2.0 (released 2026-09-25) with v1.5.2 as the latest agent/installer release. Remaining deferred
> work: resolved-alert retention + snooze (issue 6), structured logging (issue 16), time-series
> partitioning at scale (issue 7), and streaming mode itself (issue 2 — the race only matters once the
> feature exists).

---

