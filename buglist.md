# OurWay RMM — Bug List & Unimplemented Features

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

**Status (2026-10-26, at v2.1.0):** a re-audit found 1 High, 2 Medium and 1
feature gap not covered by the October sweep — see
[Re-audit at v2.1.0](#-re-audit-at-v210-2026-10-26). The dead-code and
documentation items were fixed the same day; the macOS input gap and the
scope/tenancy gaps are scoped in `docs/roadmap-2.x.md`.

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

## 🔍 Re-audit at v2.1.0 (2026-10-26)

Static review of the `v2.1.0` tree, focused on the gap between what the product
advertises and what the code does. Findings are numbered continuously with the
October set.

### 🟠 H8. macOS remote control is view-only
**Status:** ✅ Fixed in `v2.2.0` (2.2-1) — `agent/session/input_darwin_cgo.go`
**Files:** `agent/session/input_darwin_cgo.go`, `agent/session/input_darwin_nocgo.go`

`synthesizeKey` and `synthesizeMouse` on darwin log the event and return. Screen
capture, geometry, framing, and the session control path are all implemented for
macOS (`screen_capture_darwin.go`, `geometry_darwin.go`, `remote_darwin.go`), so a
macOS session opens, streams, and looks fully live — while every keystroke and
click the technician sends is silently discarded. `PRODUCT.md` and `README.md`
advertise remote control on all three desktop OSes; only Windows
(`input_windows.go`) and Linux (`input_linux.go`) synthesize input.

**Fix (shipped):** CGEvent-backed keyboard/mouse/scroll posting in a cgo file,
with the browser `code`/`key` → macOS virtual-key table in
`agent/session/macosmapping.go` (tested) and the modifier state carried on every
key event. The Accessibility (TCC) state is read with `AXIsProcessTrusted`,
reported to the server on the metrics envelope (`input_capability`), and named on
the unit view and in the session panel. A `CGO_ENABLED=0` darwin build — what a
plain Linux cross-compile produces — now reports `unavailable` and drops input
*loudly*, instead of silently. `ourway-agent --grant-input-permission` shows the
grant prompt; `docs/agent.md` states the per-binary / per-path / codesign rule.
CI builds the cgo darwin binaries (arm64 + amd64) on a macOS runner and the smoke
job asserts the unit reports `denied` there. Measured control round-trip on the
smoke run: 0.034 s (server → agent, same host).

### 🟡 M15. WebRTC removal was server-side only; the agent kept it
**Status:** ✅ Fixed 2026-10-26 — `agent/session/webrtc.go` (178 lines,
`NewWebRTCManager` had zero callers) deleted; `pion/webrtc/v3` and its 15 transitive
requires dropped from `agent/go.mod`; the stale "send frames over the WebRTC data
channel" TODO in `agent/session/session.go` removed.
**Files:** `agent/session/webrtc.go` (deleted), `agent/go.mod`

H4 removed the gateway's `PeerConnection` and the dashboard's offer/answer/ICE
code, but left the agent's `WebRTCManager` in place — dead code that kept
`pion/webrtc/v3` as a **direct** agent dependency, pulling stun/turn/ice/dtls/sctp
into every agent binary built from source, in a product whose position is that the
operator owns and audits the agent.

**Fix:** delete the file and the dependency. (Run `go mod tidy -C agent` to prune
the now-unreferenced transitive requires from `go.sum`.)

### 🟡 M16. Documentation described endpoints and auth that do not exist
**Status:** ✅ Fixed 2026-10-26 (docs corrected; the missing docs below are now
the 2.2.0 documentation scope)
**Files:** `docs/api.md`, `README.md`, `docs/configuration.md`, `.env.example`,
`docs/versioning.md`, `docs/roadmap-2.0.md`, `docs/phase*-tasks.md`,
`docs/release-notes-2.0.md`, `docs/migration-guide.md`, `SECURITY.md`

Every one of these misled an operator reading it as current:

1. `docs/api.md` documented `POST /api/sessions/:id/answer` and `/ice` — routes
   never registered in `server/api/handlers.go` — and a StartSession response
   containing `"offer"`, which the handler does not produce.
2. `docs/api.md` documented WebSocket auth as `?token=` / `?device_key=` query
   parameters — the exact behaviour C5 removed. Credentials now travel in
   `Sec-WebSocket-Protocol`.
3. `POST /api/devices/:id/stream` and `/stream/stop` (the H5 fix) were not in
   `docs/api.md` at all.
4. The mouse-input example documented `x`/`y` as pixels (100, 200); the agent
   treats them as **0–100 percentages of the captured frame**
   (`agent/session/geometry.go`).
5. `ENROLLMENT_SECRET` (the C4 enrollment gate) and `WS_ORIGINS` (the C5 origin
   allow-list) were documented **nowhere** — not in `configuration.md`, not in
   `.env.example`, not in `SECURITY.md`. `WS_ORIGINS` is matched against the
   origin host[:port], so the natural-looking `http://localhost:3000` breaks every
   browser WebSocket with 403.
6. `.env.example` shipped `JWT_SECRET="ourway-secret-key"` as a template value —
   a publicly known signing key, in a repo whose own C7 finding is about shared
   default secrets. `configuration.md` listed the same string as the default,
   which no longer exists (unset yields an ephemeral random key).
7. `README.md` claimed "Remote sessions (WebRTC gateway)" and a "Remote session
   viewer (WebRTC)" after H4 removed WebRTC, and marked streaming mode "deferred
   — not shipped" after H5 shipped it.
8. `docs/versioning.md` stated "`v2.0.3` is the last released version" (v2.1.0
   shipped under the scheme) and told operators to set `OURWAY_UPDATE_CHANNEL=edge`
   "on both sides" — the server has no such variable; only the agent reads it
   (`agent/selfupdate/version.go`).

**Fix:** all eight corrected on 2026-10-26; `README.md` gained a **Known gaps**
section so the claims in H8 / M17 are stated rather than discovered.

### 🧩 M17. Declared capabilities with no implementation behind them
**Status:** ⚠️ Open — scoped in `docs/roadmap-2.x.md`

| Declared by | Feature | Reality |
|---|---|---|
| `docs/roadmap-2.0.md` Phase 1.2, `docs/phase1-tasks.md` | Audit log for user actions | No `audit_log` table, no writer, no UI — zero code |
| `docs/roadmap-2.0.md` Phase 1.3 | Per-device and per-group custom thresholds | `server/alerts/engine.go` holds one global `thresholds` map; `SetThresholds` replaces it wholesale |
| `PRODUCT.md` ("many client fleets", MSP-scale) | Client/tenant isolation | No client, organization, or tenant entity in `server/models`; a `technician` sees every device in the installation |
| `docs/roadmap-2.0.md` §2.2 | Linux package managers: apt, yum/dnf, **pacman, zypper** | `agent/patch/patch.go` implements apt and yum/dnf only |
| `docs/roadmap-2.0.md` §2.2 | Patch rollback | macOS rollback explicitly refuses (`agent/patch/patch.go:626`) |

**Fix:** 2.3.0 (tenancy + audit log), 2.4.0 (thresholds), 2.5.0 (patch parity).

### 🟠 H11. `--install` pointed the service at a binary that is not there
**Status:** ✅ Fixed 2026-10-26 — `agent/install/linux.go`, `agent/install/darwin.go`, `agent/install/service_path.go`
**Files:** `agent/install/linux.go` (systemd `ExecStart`), `agent/install/darwin.go` (launchd `ProgramArguments`)

Both installers hardcode `/usr/local/bin/ourway-agent` as the program the service
runs, while `--install` installs the service **from wherever the agent binary
currently is**. Run `./ourway-agent --install` from the directory you downloaded
it into — the way the smoke job, and plenty of people, do it — and systemd
starts a unit whose `ExecStart` does not exist: `status=203/EXEC`, a restart
loop every 5 s, and a console showing a unit that never comes online. The
installer now uses the binary it was invoked from (`os.Executable()`, symlinks
resolved), the Windows installer's behaviour all along, and warns when that path
is not one a root-run service should depend on — on macOS the Accessibility grant
follows the binary's path, so a service running out of a home directory loses
remote control at the next self-update.

### 🟠 H10. The macOS agent has never been compiled with cgo
**Status:** ✅ Fixed 2026-10-26 — `agent/session/geometry_darwin.go`, found by the 2.2-0 agent-smoke job on a macOS runner
**Files:** `agent/session/geometry_darwin.go` (and, by the same logic, every darwin cgo file added later)

`geometry_darwin.go` is guarded `//go:build darwin && cgo` and declares CoreGraphics
functions with no `#cgo LDFLAGS` for the frameworks, so the file has never
compiled: `CGDisplayPixelsHeight` sits behind a deployment-target conditional in
the SDK headers and cgo refuses to evaluate it — `could not determine what
C.CGDisplayPixelsHeight refers to`. CI never saw it because every darwin build
was a `CGO_ENABLED=0` cross-compile, which excludes the file by its own build
tag: the darwin cgo path existed only as source. A macOS build on a Mac
(`CGO_ENABLED=1`, the default there) failed at the first step of the smoke job.
Fixed by reading the geometry out of `CGDisplayBounds` through a C helper and
linking the frameworks. Rule for 2.2-1 and everything after: darwin cgo files
declare frameworks, keep version-conditioned symbols inside C helpers, and are
compiled on a macOS runner — a Linux cross-compile cannot check them.

### 🟠 H9. Every browser WebSocket on a `localhost` install was refused
**Status:** ✅ Fixed 2026-10-26 — `server/ws/hub.go`, regression test `TestHubBrowserOriginHandshake`
**Files:** `server/ws/hub.go`, found by the 2.2-0 end-to-end harness

The hub checks origins itself (`originAllowed`: `WS_ORIGINS`, same-host, local dev,
`WEB_URL`) and then called `websocket.Accept` with `OriginPatterns:
[]string{".*"}`, documented as "a pass through so the handshake succeeds". The
library matches `OriginPatterns` with `filepath.Match`, where `.` is a **literal
dot** — so an origin with no dot, i.e. `localhost:3000`, matched nothing and the
handshake failed with 403 on every browser connection, however `WS_ORIGINS` was
written. Agents (no `Origin` header) were unaffected, and the existing tests
called `originAllowed` directly, so nothing exercised the handshake: the console
worked on a dotted host name and was dead on `localhost`, which is how every
developer and single-host install runs it.

**Fix:** `AcceptOptions.InsecureSkipVerify: true` — the library's duplicate origin
check is switched off because `originAllowed` is the policy — plus a test that
dials the real handshake with a browser `Origin` and requires both the allowed
origin and the refusal of an origin outside the allow-list.

### 🟡 M18. `POST /api/users` answers 500 for a duplicate username or e-mail
**Status:** ⚠️ Open
**Files:** `server/api/users.go`, found by the 2.2-0 harness (`e2e/fixtures.ts`)

L5 gave `Register` and `UpdateProfile` 409s for collision, but the admin route that
creates a user reports the same collision as `500 failed to create user`. A client
cannot tell "this user already exists" from "the server is broken", so an idempotent
operation (seed a viewer, re-run an integration) looks like a server failure. The
harness works around it by listing users first.

**Fix:** look up username/e-mail before insert in `CreateUser` and return 409 with
the named field, matching L5's wording.

### 🟡 M19. Ten destructive actions still confirm with a browser dialog
**Status:** ✅ Fixed in `v2.2.0` (2.2-9)
**Files:** `web/src/pages/{Sessions,Settings,Users,Webhooks,SSO,DeviceDetail,PatchPolicies}.tsx`,
`web/src/components/ConfirmSheet.tsx`, `scripts/browser-dialogs-check.sh`

`DESIGN.md` states the rule outright — "Confirmation is a state of the control,
not a browser dialog… `window.confirm()` is banned". Ten call sites still used the
native dialog (`confirm(` / `window.confirm(`), including "End session", "Delete ALL
metrics history and alerts", and "Permanently delete your account". Native dialogs are
unthemeable, unlocalised (English-only strings built by string concatenation), and
are auto-dismissed by automation — the harness had to accept the dialog to end a
session at all.

**Fixed by:** `ConfirmSheet` (title names the target, body states the consequence, the
verb is on the control), the ten call sites routed through it, the copy in
`confirm.<action>.*` in `en`/`de`/`fr`, and `scripts/browser-dialogs-check.sh` in the
tests job and the release gate, so a typed `confirm(` now fails CI.

**Doc correction:** this entry claimed 2.1.0 shipped `ConfirmSheet`. It did not — the
component existed in `DESIGN.md` and in this buglist only; there was no such file in
`web/src/components/` on `v2.1.0`, `v2.1.0`'s tree, or any commit before 2.2-9. The
sheet is built in 2.2-9.

### 🟡 M20. A unit that cannot capture spams 15 log lines a second and tells the console nothing
**Status:** ✅ Fixed in `v2.2.0` (2.2-1)
**Files:** `agent/session/session.go` (capture loop), found by `scripts/agent-smoke.sh`

`captureLoop` runs at 15 fps; on capture failure it logs `session: capture error: …`
and `continue`s (`session.go:281-283`). On a unit with no usable display — a headless
box, a Mac without the Screen Recording grant, an X session the agent cannot reach —
that is 15 identical lines per second for the whole session (the smoke run produced
about 1,900 in two minutes), with no backoff, no consolidation, and no signal to the
server: the technician's session panel stays blank with no reason.

**Fix (shipped):** the capture loop records its outcome and reports it as
`capture_state` (`ok` / `error`) on the same metrics envelope as the input
capability; the console shows *Capture failing* on the unit view. Logging goes
through `logThrottled`, which emits one line per reason per 10 seconds and one on
recovery — the ~1,900 identical lines from the two-minute smoke run are gone.
The finer states (`denied-permission` vs `no-display`) are a macOS-side mapping
left for the Screen-Recording grant work.

### 🟢 L13. `TestViewerSessionRelay` can fail under container load
**Status:** ⚠️ Observed once (not reproduced in 4 later runs)
**Files:** `server/ws/viewer_test.go`

It failed once with `context deadline exceeded` inside a 5 s read deadline while
the package ran next to the whole `-race` suite on a loaded container; it passed
3/3 in isolation and the full `./ws/` suite passed afterwards. The test's fixed
5 s deadlines are tight for a CI runner under load — worth widening before the
e2e job makes CI noisier.

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

### 🟡 M21. The Windows service was managed by shelling out to `sc.exe`
**Status:** ✅ Fixed 2026-10-27 — `agent/install/windows_service.go`, `agent/install/service_command.go`, tests `TestServiceBinPathQuoting` / `TestValidateServiceBinary`
**Files:** `agent/install/windows.go` (create/start/stop/delete), `agent/install/windows_service.go`

Install, start, recovery and delete all went through `sc.exe`, whose options
(`binPath=`, `start=`, `display=`) include the equal sign in the name and are
parsed from the **raw command line**. Go's `exec.Command` quotes any argument
with spaces, which hides the option name; quoting the path alone leaves the
agent's `--server`/`--key` arguments outside the value; the option and its value
must be one token. Four command-line shapes were tried on the CI Windows runner —
separate tokens, path-only quoting, whole-value quoting, and a verbatim
`SysProcAttr.CmdLine` — and all four answered **1639** with a usage dump, a
number that names nothing. The job also had to work around Windows runners
shipping PostgreSQL as a service the runner user cannot start.

The service is now managed through the Service Control Manager API
(`OpenSCManager` / `CreateService` / `ChangeServiceConfig2` / `StartService` /
`ControlService` / `DeleteService`), which takes the same fields as arguments and
returns the system's real error. The binary-path rule the SCM enforces (the path
is quoted when it contains spaces, the agent's arguments stay outside the quotes,
and a binary with no `.exe` name is refused) is stated in `serviceBinPath` /
`validateServiceBinary` and pinned by tests.


**Status:** ✅ Fixed 2026-10-26 — `agent/install/sc_args.go`, test `TestSCCreateArgsQuoting`
**Files:** `agent/install/windows.go`

`sc.exe` includes the equal sign in each option name, and a value containing a
space is only read as that option's value when it travels **with** the option, in
one argument. The installer passed `binPath=` and the quoted path as two
arguments, and `display=` and `OurWay Agent` as two more, so `sc.exe` printed its
usage and exited 1639 (`ERROR_INVALID_NAME`). The Windows agent smoke job found
it at the last assertion — six checks after the agent had proven it enrolls,
connects, heartbeats, reports metrics and ships remote-control frames — which is
the wrong order for a user: the service install is the first thing an install
does.

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
