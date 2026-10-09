# OurWay RMM — Bug List & Unimplemented Features (historical)

**Note:** this is a historical review record (October 2026). All Critical, High and Medium findings were fixed; SAML SSO remains the only open feature gap.

**Repository:** WelcomeToTheWeb/OurWay
**Review period:** October 2–3, 2026
**Scope:** ~45 files across `server/` (api, ws, auth, alerts, store, models, sessions, patching),
`agent/` (client, collector), and `web/src/` (routing, api layer, stores)
**Reviewer:** Lumo (AI-assisted code review)

**Status (2026-10-02):** All 7 Critical, 7 High, and 14 Medium items are
fixed and verified (Go build + full server/agent test suites + web build
green). Low: 11 of 12 fixed; L8 (SAML) remains as a feature gap. Incomplete
features: 6 of 10 were implemented in the 2026-10-02 sweep; the remaining
patch-policy items (scheduling, scoping, auto-reboot) followed on
2026-10-03, leaving SAML as the only open feature (see the table below).
See the ✅ status line under each item.

---

## Legend

| Symbol | Meaning |
|---|---|
| 🔴 | Critical — exploitable security vulnerability or data corruption |
| 🟠 | High — functional failure or significant security/UX gap |
| 🟡 | Medium — reliability, integrity, or incomplete feature |
| 🟢 | Low — code smell, minor bug, or cleanup |
| 🧩 | Feature declared/planned but not implemented |
| ✅ | Previously found, since verified as fixed |

---

## Summary

| Severity | Found | Fixed 2026-10-02 | Open |
|---|---|---|---|
| 🔴 Critical | 7 | 7 | 0 |
| 🟠 High | 7 | 7 | 0 |
| 🟡 Medium | 14 | 14 | 0 |
| 🟢 Low | 12 | 11 | 1 |
| 🧩 Incomplete features | 10 | 6 | 4 |
| ✅ Cleared during review | 6 | — | — |

**Strongest areas:** idempotent patch-deployment bookkeeping (`deploy.go`,
`deployment_results.go`), single-writer WebSocket patterns on both sides, graceful
shutdown, careful reconnection/backoff in the agent.

**Weakest areas:** device identity and credential handling (C1–C5), role
enforcement on remote-control routes, and half-finished WebRTC/streaming
features.

---

## 🔴 Critical

### C1. Unauthorized remote control via self-registration chain
**Status:** ✅ Fixed 2026-10-02 — `Register` returns 403 once any user
exists; session start/input/quality/end routes require
admin/manager/technician (`handlers.go`).
**Files:** `server/api/auth.go` (Register), `server/api/sessions.go` (StartSession, SendInput), `server/api/handlers.go`

Anyone can `POST /api/auth/register` at any time — the first-run gate
(`Status()`) exists but is never enforced. Registration yields the default
`viewer` role, yet `POST /api/devices/:id/sessions` has **no role check**
(unlike nearly every other mutating route). The new user then owns a session and
passes `POST /api/sessions/:id/input` ownership checks — keyboard/mouse
injection into any device. Frontend `RoleRoute` guards on the Sessions page
confirm this is an oversight, not design.

**Fix:** gate `Register` on `store.Users.Count() == 0`; add
`RequireAnyRole("admin","manager","technician")` to session start, input,
quality, and answer routes.

### C2. Cross-device metrics spoofing over WebSocket
**Status:** ✅ Fixed 2026-10-02 — the metrics case uses the connection's
authenticated device key; a payload-declared `device_key` is ignored
(`hub.go`).
**File:** `server/ws/hub.go` — `handleDeviceMessage`, metrics case

The metrics handler trusts the **payload's** `device_key` field instead of the
connection's authenticated key:

```go
if dk, ok := payloadMap["device_key"].(string); ok && dk != "" {
    dev, err := store.Devices.GetByKey(dk)  // attacker-controlled
```

One compromised agent can inject fake CPU/RAM/disk metrics for any device —
triggering or *suppressing* alerts fleet-wide (autoResolve on injected low
values). Every other agent-auth path in the codebase uses connection identity
correctly; this is the lone outlier.

**Fix:** delete the payload lookup; use the `deviceKey` parameter already
passed into `handleDeviceMessage`.

### C3. Device keys exposed to every authenticated user
**Status:** ✅ Fixed 2026-10-02 — `DeviceKey` is `json:"-"`; the key is
returned only in the agent registration response.
**Files:** `server/models/device.go`, `server/api/devices.go` (ListDevices)

```go
DeviceKey string `gorm:"uniqueIndex;not null" json:"device_key"`
```

The key serializes to JSON, and `ListDevices` has no role restriction — any
`viewer` can harvest every device key. A device key is a full device
credential: connect as that device, **capture keystrokes a technician sends to
it**, push files, fake metrics, accept deploys/reboots.

**Fix:** `json:"-"` on the field; never return the key outside the agent
registration response.

### C4. Unauthenticated re-registration steals device identity
**Status:** ✅ Fixed 2026-10-02 — re-registration of an existing
(hostname, private_ip) requires the shared enrollment secret; the initial
install stays open when the secret is empty (`devices.go`).
**Files:** `server/api/devices.go` (RegisterDevice), `server/store/devices.go`

The register endpoint is intentionally public, but the idempotency path
returns the existing device's key to anyone who knows its (easily
discoverable) hostname + private IP:

```go
if existing, err := h.store.Devices.GetByHostnameAndIP(req.Hostname, req.PrivateIP); err == nil {
    ...
    c.JSON(200, gin.H{"device": existing, "device_key": existing.DeviceKey})
```

The attacker also rewrites `public_ip`/metadata. No enrollment secret is
checked anywhere. No unique constraint on (hostname, private_ip) either —
concurrent installs can create duplicates.

**Fix:** per-installation enrollment token required at registration; proof-of-
possession for re-registration.

### C5. Credentials in WebSocket query strings + wildcard origins
**Status:** ✅ Fixed 2026-10-02 — credentials travel in
`Sec-WebSocket-Protocol` (server, agent, and dashboard all updated);
allowed Origins are restricted to the web URL plus `WS_ORIGINS`.
**Files:** `server/ws/hub.go` (ServeHTTP), `agent/client/client.go`

Device keys and JWTs travel as `?device_key=` / `?token=` URL parameters —
logged in access logs, proxy logs, browser history; additionally
`OriginPatterns: []string{"*"}` allows any origin to connect.

**Fix:** authenticate via first WS message or `Sec-WebSocket-Protocol`;
restrict origins in production.

### C6. SSO callback leaks access + refresh tokens in URL
**Status:** ✅ Fixed 2026-10-02 — the provider code is exchanged for a
one-time `sso_code` (120 s TTL); tokens never appear in a URL; the login
page posts to `/api/auth/sso/exchange`.
**File:** `server/api/sso.go` (Callback)

```go
c.Redirect(302, h.redirect+"/login?token="+token+"&refresh="+refreshToken)
```

Both tokens land in the query string (logs, history, Referer). The refresh
token grants a 7-day session, and neither is revocable (see H4).

**Fix:** one-time code exchanged via POST.

### C7. ⚠️ VERIFY: JWT secret default
**Status:** ✅ Fixed 2026-10-02 — the hardcoded default is gone:
`JWT_SECRET` is required; when unset an ephemeral random secret is used with
a loud startup warning that tokens will not survive a restart
(`config.go`).
**Files:** `server/auth/jwt.go`, `server/config/config.go`

`NewJWTAuth(secret string)` uses whatever `config.Load()` provides. If the
default is a hardcoded `"changeme"`-style value, every unconfigured install
shares a forgible signing key — making all JWT auth theater. **Five-second
check in config.go.**

---

## 🟠 High

### H1. Browser WebSocket connections die after ~120 s
**Status:** ✅ Fixed 2026-10-02 — server-side ping ticker (40 s) on every
connection; a blackholed peer is closed when its ping fails (`hub.go`).
**File:** `server/ws/hub.go` — 120 s read deadline on user connections; server
never pings. Comments assume the client pings, but user connections are
receive-only in all reviewed code, and the frontend WS hook was never provided.
Dashboards would silently disconnect every 2 minutes. **Fix:** server-side
ping ticker, or confirm the frontend keepalive.

### H2. Distributed (Redis) mode functionally incomplete
**Status:** ✅ Fixed 2026-10-02 — Redis presence keys gate `SendToDevice`
and `IsDeviceConnected` across instances; stale-connection teardown only
writes `offline` when that connection is still the registered one.
**File:** `server/ws/hub.go`

- `IsDeviceConnected` checks only local clients → `StartSession` returns 409
  for devices connected to another instance.
- `SendToDevice` returns success after a Redis publish even if the device is
  connected nowhere (reboot/session/deploy callers report false success).
- Stale connection teardown writes `offline` to the DB unconditionally — even
  after a newer connection re-registered under the same key. The `current ==
  client` guard protects the registry map only, not the DB write.

### H3. JWTs non-revocable; 24-hour access-token lifetime
**Status:** ✅ Fixed 2026-10-02 — 15-min access tokens with JTI + denylist,
`POST /api/auth/logout`, and a token-generation bump on password change
that invalidates all outstanding tokens.
**Files:** `server/auth/jwt.go`, `server/api/auth.go`

No `jti`/denylist, no logout, password change doesn't invalidate outstanding
tokens. Access tokens live 24 hours (industry norm: 5–15 min with refresh
carrying the session). Combined with C5/C6, leakage windows are measured in
days.

### H4. WebRTC machinery is dead weight with known holes
**Status:** ✅ Fixed 2026-10-02 — decision: remove. Pion `PeerConnection`
is gone from the gateway (frames already flowed over WS/HTTP); the
dashboard's offer/answer/ICE code and the orphaned `SendData` were deleted.
**File:** `server/sessions/gateway.go`

The agent never negotiates WebRTC (frames go over HTTP `ReportFrame`
instead), yet every session spins a Pion PC. Known-incomplete pieces, per the
code's own TODOs: trickle-ICE candidates after the 3 s window are dropped
("the data channel may never open"); hardcoded Google STUN server; offerer/
answerer API confusion (`OnDataChannel` + `CreateDataChannel` both used).
Either complete it or remove it.

### H5. Streaming mode never triggered
**Status:** ✅ Fixed 2026-10-02 — implemented end to end:
`POST /api/devices/:id/stream[/stop]` (interval clamped 1–60 s), the agent
honors `stream`/`stream_end` with a validated interval (1–3600 s), and the
device detail page starts/stops streaming on mount/unmount. Regression
test: `server/api/stream_test.go`.
**Files:** `agent/client/client.go` (fully implements `stream`/`stream_end`)

No server-side sender found in any reviewed file, and nothing in the frontend
API layer invokes it. The README's headline "detail page switches agent to 2 s
streaming" feature appears unwired.

### H6. Reap closes WebRTC PCs but leaves DB rows "pending" forever
**Status:** ✅ Fixed 2026-10-02 — the reaper ends stale sessions in the DB
(`status: "ended"`); it is ctx-scoped with `Gateway.Stop()`, so no
goroutine leak per `SetupRouter` invocation.
**Files:** `server/sessions/gateway.go`, `server/store/sessions.go`

The pending-session reaper (5-min TTL) cleans the in-memory gateway only.
`Session` DB rows stay `pending`/`active` indefinitely and are returned by
`ListActiveAll` — users see phantom sessions. Also: `Gateway.Stop()` is never
called; the reaper goroutine leaks per `SetupRouter` invocation.

### H7. Frontend agent-API functions are broken
**Status:** ✅ Fixed 2026-10-02 — `registerAgent`/`heartbeat`/`sendMetrics`
deleted from `web/src/api/devices.ts` (agents call `/api/agent/*`
themselves; the dashboard never did).
**File:** `web/src/api/devices.ts`

`registerAgent`, `heartbeat`, `sendMetrics` target `/agents/register`,
`/agents/:key/heartbeat`, `/agents/:key/metrics` — but the server exposes
`/api/agent/register`, `/api/agent/heartbeat` (key in header), `/api/agent/
metrics`. Field names mismatch too (`deviceKey` vs `device_key`, `agentVersion`
vs `agent_version`). Presumably dead code — delete it before someone builds on
it.

---

## 🟡 Medium

| # | Title | Location | Detail | Status |
|---|---|---|---|---|
| M1 | Alert assignment/ack metadata never persisted | `models/alert.go` | `AssignedTo`, `AcknowledgedBy`, timestamps tagged `gorm:"-"` → silently discarded on save. Boolean halves work; assignment as a feature does nothing. **Fix:** remove the tags. | ✅ |
| M2 | Frames accepted for ended sessions, unbounded | `api/sessions.go::ReportFrame` | No `session.Status` check; route outside rate-limited group; 10 MB bodies at will | ✅ |
| M3 | Reboot reports success unconditionally | `api/reboot.go` | Offline device still yields `reboot_initiated`; malformed JSON swallowed → typo'd payload reboots immediately | ✅ |
| M4 | Public IP resolved once, via third party | `agent/client/{ip,client}.go` | Stale forever after network change (private IP refreshes per heartbeat — inconsistent); api.ipify.org dependency; `PrivateIP()` often returns docker0 | ✅ |
| M5 | Alert engine runs inline on WS read loop | `ws/hub.go` | Sync DB writes in the connection's read path; slow DB → stalled heartbeats → 60 s read deadline disconnects | ✅ |
| M6 | `ListSessions` leaks all users' sessions to any role | `api/sessions.go` | No filter, no pagination — viewers see everyone's live remote sessions | ✅ |
| M7 | Duplicate WS connections per device key | `ws/hub.go` | Newest wins map slot; older conn feeds stale metrics until deadline, then falsely marks device offline (interacts with H2) | ✅ |
| M8 | Broadcast loss/duplication | `ws/hub.go::BroadcastMessage` | Redis fallback path → local clients can receive duplicates; without Redis, silent drop; no dropped-frame counters anywhere | ✅ |
| M9 | Background jobs inside `SetupRouter` | `api/handlers.go` | `ScanAll` + webhook retry ticker as bare goroutines; duplicated per invocation (tests!), ignore lifecycle | ✅ |
| M10 | Silent message drops on full queues | `ws/hub.go`, `agent/client/client.go` | Log-and-drop with no metric — degraded delivery is invisible | ✅ |
| M11 | Metrics history: no retention scheduling | `store/metrics_history.go` | `DeleteOlderThan` exists but no caller; unbounded `metric_history` growth. Also 10,000-row limit per query with no downsampling | ✅ |
| M12 | Webhook event matching via SQL LIKE | `store/webhooks.go::ListByEvent` | `events LIKE '%"event"%'` — fragile; `"device_online"` substring-matches `"device_online_v2"`-style events | ✅ |
| M13 | SSO state cookie weaknesses | `api/sso.go` | `rand.Read` error ignored; state cookie not cleared after callback (replayable in window); error strings interpolated to client | ✅ |
| M14 | Rollback has no result tracking | `patching/rollback.go` | Deploys track per-device outcomes; rollbacks record nothing — a failed rollback is invisible | ✅ |

---

## 🟢 Low

1. ✅ **Blocking CPU sample** (`agent/collector/cpu.go`) — non-blocking ~100 ms total sampling (pre-warmed, per-core deltas); the 1 s sample no longer eats half the streaming budget. *(Fixed 2026-10-02)*
2. ✅ **Invalid API-key expiry silently becomes permanent** (`api/api_keys.go`) — an unparseable expiration now returns 400 instead of creating a never-expiring key. *(Fixed 2026-10-02)*
3. ✅ **Agent accepts any stream interval incl. 0/negative** — interval is clamped to 1–3600 s in the agent's `stream` handler. *(Fixed 2026-10-02)*
4. ✅ **Register returns 201 when role assignment fails** — registration now fails loudly (500) if the default role cannot be assigned. *(Fixed 2026-10-02)*
5. ✅ **`User.Email` not unique** — duplicate e-mail collisions return 409 in `Register`/`UpdateProfile` instead of 500. *(Fixed 2026-10-02)*
6. ✅ **Dead middleware** (`api/middleware.go::DeviceKeyMiddleware`) — deleted (no callers, validated nothing). *(Fixed 2026-10-02)*
7. ✅ **Alert dedup map unpruned** (`alerts/engine.go`) — entries are pruned against the dedup window on every evaluation. *(Fixed 2026-10-02)*
8. **SAML selectable but unimplemented** — `Type: saml` accepted; OAuth-only handler. *(Open — tracked as the SAML SSO feature below.)*
9. ✅ **Tokens in `localStorage`** (`web/src/api/client.ts`) — XSS-stealable trade-off documented in SECURITY.md ("Token storage"). *(Fixed 2026-10-02)*
10. ✅ **`SendData` unused** (`sessions/gateway.go`) — deleted with the WebRTC removal (H4). *(Fixed 2026-10-02)*
11. ✅ **`installers.ts` double-prefix workaround** — the server now returns an API-relative `url` (`v2/installers/<name>`); the client-side prefix strip is gone. *(Fixed 2026-10-02)*
12. ✅ **Legacy empty-type tokens accepted** (`auth/jwt.go::ValidateToken`) — tokens without `Type: "access"` are rejected. *(Fixed 2026-10-02)*

---

## 🧩 Unimplemented / Incomplete Features

| Feature | Evidence |
|---|---|
| Patch policy scheduling | ✅ Fixed 2026-10-03 — `server/patching/policy.go` `PolicyEngine` runs hourly, evaluates each policy against its Schedule (daily/weekly/monthly) and drives scan → approve → deploy; tests in `policy_test.go` |
| Patch policy scoping | ✅ Fixed 2026-10-03 — `PolicyEngine.resolveScope` enforces Scope (all/devices; `ScopeValue` matches device IDs, names, or hostnames, comma-separated) |
| Auto-reboot after patching | ✅ Fixed 2026-10-03 — `PolicyEngine` reboots devices whose deploy result is `success` when `AutoReboot` is set; deploy batching honors `MaxDevicesPerBatch` |
| SAML SSO | ⚠️ Guarded (see L8) — API rejects non-oauth2/oidc types at create *and* at authorize/callback; `oidc` now accepted on the OAuth2 code path |

**Resolved 2026-10-02:** WebRTC remote control (removed — the agent never
negotiated it; frames already flow over the WS/HTTP paths), detail-page 2 s
streaming (H5), alert assignment (M1), session cleanup (H6), logout / token
revocation (H3), fleet shutdown signaling (M9 — every background job is now
ctx-scoped and stops with the server context).

---

## ✅ Cleared During Review (no action needed)

| Item | Resolution |
|---|---|
| Metrics payload shape mismatch (suspected) | `collector.go` flattens into `Metrics` exactly matching the hub's parser |
| Device never marked online (suspected) | `store.UpdateLastSeen` sets `status: "online"`; both heartbeat paths call it |
| Password hash exposure (suspected) | `User.PasswordHash` tagged `json:"-"` |
| API-key auth panic in GetProfile (suspected) | `APIKeyMiddleware` sets `user_id` on both auth paths |
| WebRTC PC leak on abandoned sessions | Gateway 5-min reaper handles it (DB rows still leak — see H6) |
| Empty-update deploy = full OS upgrade | `DeployToDevices` pre-filters and returns `ErrNoApprovedUpdates` |

---

## Recommended Fix Sequence

**Outcome (2026-10-02):** phases 1–3 all executed — every item above is
done (see the ✅ status line on each item). Remaining open: L8 (SAML) and
the four patch-policy/SSO features in the table above.

**Phase 1 — Security blockers (before any public exposure):**
1. C7: verify/change the JWT secret default (5-minute check)
2. C3 + C4: hide device keys from APIs; add enrollment secret
3. C1: gate registration; role-check session/input routes
4. C2: use connection identity in the WS metrics path
5. C6: SSO code-exchange flow

**Phase 2 — Stability (week after):**
6. H2: identity-guard the offline DB write
7. H6: close reaped sessions in the DB; call `Gateway.Stop()`
8. H3: shorten access TTL; add denylist; logout endpoint
9. H4/H5: decide WebRTC + streaming — complete or remove

**Phase 3 — Polish:**
10. M1 (gorm tags), M3 (reboot errors), M2 (frame status check), M11
    (retention job), plus Lows opportunistically

---

## Files Never Reviewed (remaining blind spots)

None remaining: the 2026-10-02 remediation pass read `server/config/`,
`web/src/hooks/`, `web/src/pages/*`, and the agent-side patch/session/
files/install packages (resolving C7, H1, H5 and the agent-side
privileged-operation review).

---

*Findings are based on static review of repository content as of October 3,
2026. Severity assumes an internet-reachable deployment; on a trusted LAN the
criticals degrade to "high" but remain exploitable by any authenticated user.*
