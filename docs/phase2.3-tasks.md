# Phase 2.3: MSP Tenancy — Task Breakdown

**Status:** Planned
**Target release:** `v2.3.0`
**Parent plan:** [`roadmap-2.x.md`](roadmap-2.x.md)
**Baseline:** `v2.2.x`

**Why this release exists.** `PRODUCT.md` names MSP technicians and admins as the
primary user, running "many client fleets and hundreds to thousands of endpoints".
There is no client, organization, or tenant concept anywhere in `server/models`
(verified on `v2.1.0`), and RBAC is a **global** role: a `technician` sees every
device in the installation. Device tags scope *patch policies*; they scope nothing
about **who may see**. This release adds the missing axis.

**The rule that governs every task below.** A caller who cannot see a client's
device must receive **404 on that device's routes** — not a filtered list, not an
empty field, not a greyed-out button. Client scoping is enforced server-side at the
route, and the console mirrors it. A console-only filter is a cosmetic feature; a
route-level guarantee is the feature.

---

## 2.3-0 · Schema: clients, device assignment, user scope

**Context:** `server/store/migrate.go` keeps an append-only list of contiguous
versioned migrations; the highest shipped is **version 7**
(`devices_input_and_capture_capability`, 2.2-1), and a shipped migration is never
edited. This lands as **version 8**.

**Do**
- [ ] `Client` model (`server/models/client.go`): `id`, `name`, `slug` (unique,
      normalized like `NormalizeTags`), `notes`, `disabled`, timestamps.
- [ ] `Device.ClientID` — nullable UUID, indexed, FK to `clients`.
- [ ] `UserClient` junction (`user_id`, `client_id`) — the client scope of a
      `manager`/`technician`. `admin` is scope-all by role, with no junction rows.
- [ ] Migration **7** `clients_and_user_client_scope`: create both tables, add
      `devices.client_id`, add the index. No data rewrite.
- [ ] **Unassigned is `client_id IS NULL`, not a fake row.** The API presents a
      sentinel client (`id: "unassigned"`) in list responses, and assigning a device
      back to unassigned is `client_id: null`. One definition, used everywhere.
- [ ] Cascade the store interface change to the mocks in `_test.go` files in the
      same change (`CONTRIBUTING.md`): `DeviceStoreInterface` gains
      client-scoped list signatures.

**Acceptance criteria**
- A 2.2 database migrates forward with every pre-existing device left
  `client_id IS NULL` and appearing under `unassigned` in `GET /api/clients`.
- Migration 7 is idempotent on re-run (the pattern `store/migrate_test.go` and
  `postgres_integration_test.go` already enforce).
- `slug` collisions return 409, normalized (lower-case, trimmed) like tags.
- Cross-compiles and the full gate green.

---

## 2.3-1 · Scope resolution: one place that answers "may this caller see it"

**Do**
- [ ] A single resolver in `server/api/` — `clientScope(c)` (the caller's allowed
      client IDs, `all` for admin) and `scopeAllowsDevice(c, deviceID)`.
- [ ] Scope is resolved from the **user row + junction**, not from a token claim,
      so revoking a scope takes effect on the next request, not the next login.
- [ ] Out-of-scope device → `404` with the same body as a nonexistent device
      (`{"error":"device not found"}`). Same shape, same status: scoping must not
      leak which IDs exist.
- [ ] Scope changes to a user are audited (2.3-6) and take effect without
      re-authentication.

**Acceptance criteria**
- A technician scoped to client A requesting `GET /api/devices/<B's id>` gets
  **404 with the identical body** a nonexistent ID gets — asserted byte-for-byte in
  `server/api/`.
- `GET /api/devices` for that technician contains no client-B rows and the
  response carries the caller's scope (`X-OurWay-Client-Scope: <ids>|all`), so the
  console and support can see why a unit is invisible.
- Removing a technician's client scope takes effect on their next request with the
  same access token.

---

## 2.3-2 · Scope every read route

Enumerated from `server/api/handlers.go` — every route below takes a scope filter:

| Route | Scoping |
|---|---|
| `GET /devices`, `GET /devices/:id`, `GET /devices/:id/metrics`, `/metrics/history` | by `device.client_id` |
| `GET /tags` (and `PUT /devices/:id/tags` reads the device first) | tags present in the caller's scope only |
| `GET /alerts` | by the alert's device |
| `GET /sessions` | intersection of the existing owner/admin rule (2.2 era) **and** client scope |
| `GET /devices/:id/updates`, `GET /patch/overview`, `GET /patch/updates`, `GET /patch/deployments`, `GET /patch/deployments/:id/results` | by the update/deployment's device(s) |
| `GET /patch/policies` | policies whose scope is inside the caller's clients; admin sees all |
| `GET /files/transfers`, `/files/transfers/:id`, `/files/:transfer_id/file` | by the transfer's device; a download token for an out-of-scope transfer is 404 |

**Acceptance criteria**
- A parameterized scoping test walks **every route above** × {admin, manager
  scoped to A, technician scoped to A, viewer scoped to A, technician with no
  client} and asserts: in-scope rows present, out-of-scope rows absent,
  out-of-scope by-ID requests **404**.
- Fleet views (`/patch/overview`, `/patch/updates`) are computed **inside** the
  scope — a scoped manager's compliance percentage is over their clients, not the
  installation. Add a fixture where the two numbers differ, so the test proves the
  difference.
- No route filters in the handler by post-filtering a full store read: scoping is a
  store-level query. A test asserts the SQL carries the client filter (row counts
  from the store, not the handler).

---

## 2.3-3 · Scope every bulk and mutating route (the security task)

Bulk routes are where tenancy leaks: they take lists of IDs, and a partial success
is an action on a client the caller must not touch.

| Route | Required behaviour |
|---|---|
| `POST /patch/deploy`, `POST /patch/scan`, `POST /devices/tags/bulk`, `POST /files/push`, `POST /files/pull` | every target device in scope, **or the whole request is refused** (`403`, body lists the out-of-scope IDs). No partial deploy, no partial queueing. |
| `POST /devices/:id/reboot`, `/stream`, `/stream/stop`, `/updates/scan`, `POST /devices/:id/sessions` | 404 when the device is out of scope |
| `POST /alerts/:id/resolve`, `/acknowledge`, `/assign` | 404 when the alert's device is out of scope |
| `POST /updates/:id/approve`, `POST /patch/updates/approve`, `/skip` | update belongs to an in-scope device; bulk approval refuses the batch otherwise |
| `POST /sessions/:id/input`, `/quality`, `DELETE /sessions/:id` | the session's device in scope, **in addition to** the existing ownership check |
| `PUT /devices/:id/tags` | 404 out of scope |
| `DELETE /devices/:id` | admin only, unchanged |
| `POST /patch/policies`, `PUT`, `DELETE` | admin only; a policy whose scope reaches a client the creator cannot see is refused with a named error |

**Acceptance criteria**
- **Atomicity test:** a technician scoped to A calls `POST /patch/deploy` with
  `[A-device, B-device]` → `403`, **and the store shows no deployment row, no
  deployment results, and no queued deploy for either device**. Same for
  `/patch/scan`, `/devices/tags/bulk`, `/files/push`.
- A technician scoped to A cannot start a session, send input, reboot, push a file,
  or resolve an alert on any B device — each asserted with the exact status code.
- An `admin` is unaffected: the full existing test suite passes unchanged.
- Scoping is checked **before** any side effect, proven by a store spy in the test
  (zero writes on a refused request).

---

## 2.3-4 · Client CRUD + the Clients surface

**Do**
- [ ] `GET/POST /api/clients`, `PUT/DELETE /api/clients/:id`; `PUT /api/clients/:id/devices`
      (assign batch), `GET /api/clients/:id/rollup`.
- [ ] `admin` manages clients; `manager` reads clients in scope. Deleting a client
      moves its devices to `unassigned` — never deletes devices.
- [ ] Assign a client at enrollment via the enrollment flow when
      `ENROLLMENT_SECRET` is paired with a client (a per-client enrollment token);
      otherwise a new unit lands in `unassigned`.
- [ ] Console: Clients ledger (units, alerts by severity, patch compliance, last
      seen), client detail with unit list, assign/unassign, per-client technician
      assignment.

**Acceptance criteria**
- Deleting a client leaves every device intact and `client_id IS NULL`, asserted in
  a store test.
- A technician assigned to two clients sees exactly those two in the ledger.
- `GET /api/clients/:id/rollup` numbers equal the device-ledger numbers computed
  independently — the same single-source rule as 2.2-4.
- Every string in `en`/`de`/`fr` in lockstep, and `scripts/i18n-parity.sh` (2.2-5)
  is green.

---

## 2.3-5 · Client filter and header on every ledger

**Do**
- [ ] Client column + filter on Devices, Alerts, Patches, Sessions, Files;
      client header on device detail; per-client rollup on the dashboard.
- [ ] The client is URL state (`?client=…`), following the 2.1.0 view-in-URL
      pattern — a filtered view is linkable and survives a reload.
- [ ] The shift bar prints the operator's client scope, so a technician who
      "can't find" a unit sees the reason at a glance.
- [ ] Density at MSP scale: the client filter is a facet in the existing filter
      row, not a new page.

**Acceptance criteria**
- On a fixture set of 3 clients × 40 units, Devices with a client filter renders in
  one list read and stays virtualized past 20 cards.
- `?client=a&status=warning&sort=last-seen` deep-links to exactly that view.
- The dashboard's per-client rows agree with the Clients ledger (2.2-4's
  single-source rule extended to per-client counts).

---

## 2.3-6 · Audit log (the Phase 1.2 item that shipped with zero code)

**Context:** `docs/phase1-tasks.md` lists "Audit log for user actions" as complete;
there is no table, no writer, and no UI. This lands as migration **9**.

**Do**
- [ ] `AuditLog` model + migration 9: `id`, `actor_user_id`, `actor_username`,
      `actor_provider`, `action` (verb, e.g. `device.delete`, `patch.deploy`),
      `target_type`, `target_id`, `client_id`, `details` (JSON), `source_ip`,
      `created_at`. Index on `(client_id, created_at)` and `(action, created_at)`.
- [ ] A store-level writer (`store.Audit.Record(...)`) called from handlers, **not**
      a gin middleware — middleware cannot know the target, the client, or whether
      the action succeeded.
- [ ] Record every mutating route in `server/api/handlers.go`, plus: login
      success/failure, logout, password change, SSO JIT provisioning, role and
      client-scope changes, API key create/rotate/revoke, session start/end,
      reboot, device delete, tag changes, deploy/approve/skip/rollback.
- [ ] Failed and refused actions are recorded with the outcome — a refused
      out-of-scope deploy is exactly the event an MSP wants to see.
- [ ] Retention: `AUDIT_RETENTION_DAYS` (default 365), pruned by the existing
      retention manager pattern (`store/metrics_history.go` has the shape; M11's
      lesson is that a delete method with no caller is not a retention policy).
- [ ] `GET /api/audit` (admin; managers see their clients' rows) with
      actor/action/client/date filters + CSV export.
- [ ] Console: Audit ledger with filters; entry points from device detail
      ("this unit's record") and from the user's own profile.

**Acceptance criteria**
- **Coverage test, not a promise:** a test walks `SetupRouter`'s route list, filters
  to mutating methods, and asserts each one is registered in an audit registry; the
  only permitted exceptions are an explicit allow-list (`POST /api/agent/*`
  agent-side writes, `/health`, `/ready`, `/api/agent/version`, static). Adding a
  mutating route without auditing it **fails the build**.
- A technician scoped to A cannot read audit rows for client B (404/empty per 2.3-1
  rules), and admin sees all.
- Login failure, password change, scope change, key rotation and a refused
  out-of-scope deploy each produce a row with the right actor, client, and outcome.
- Rows are written **before** the response is sent, so a crash after the action
  leaves the record; proven with a failing-action fixture.
- Pruning is scheduled and tested: rows older than `AUDIT_RETENTION_DAYS` disappear,
  and the prune is observable (a counter, in the 2.5.0 metrics surface).

---

## 2.3-7 · Upgrade path and the behaviour change, stated plainly

**Do**
- [ ] Postgres integration test (the pattern in
      `server/store/postgres_integration_test.go`): a 2.2 schema upgraded to 2.3,
      every device readable, `unassigned` present, admin behaviour unchanged.
- [ ] `docs/upgrade-guide.md`: **a technician's visible set shrinks after this
      upgrade** — that is the point, and an operator must expect it. Include the
      one-time assignment step, and the fact that unassigned devices are invisible
      to non-admins until assigned.
- [ ] `docs/user-guide.md`: Clients, scoping, audit ledger.
- [ ] `docs/api.md`: every new route, in the same pass as the 2.2-7 route↔docs
      diff check (which this release must keep green).
- [ ] `docs/migration-guide.md` / changelog section for `v2.3.0`, per
      `docs/versioning.md`.

**Acceptance criteria**
- The integration test proves the upgrade is non-destructive and idempotent.
- The upgrade guide states the visibility change in the first section, not in a
  footnote.
- Route↔docs check green with all new routes documented.

---

## 2.3-8 · Scale: scoping must not cost a query per row

**Do**
- [ ] Scoping is a store-level `WHERE` on the client set, not a loop of per-device
      checks: the 10,000-device load tool runs with a technician scoped to 2,000
      devices and the list request count is unchanged from 2.2.
- [ ] The scope resolution is fetched **once per request**, not once per route
      check (a bulk deploy with 500 targets must not do 500 scope queries).
- [ ] Index on `devices.client_id` verified with `EXPLAIN` on the device-list query.
- [ ] The e2e suite from 2.2-0 gains a tenancy flow: scoped login → filtered
      ledger → device detail → action → audit row visible.

**Acceptance criteria**
- Load test: 10,000 simulated devices, scoped technician, sustained ~100 req/s,
  **zero failures**, and list-query latency within 10% of the unscoped baseline.
- A query-count assertion in the scoping test (no N+1) fails if scope checks scale
  with the target count.
- The e2e tenancy flow is green in CI.

---

## Release gate for `v2.3.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/i18n-parity.sh
scripts/release.sh --check 2.3.0
```

Plus, non-negotiable for this release:

- [ ] The scoping matrix (2.3-2) and the atomicity tests (2.3-3) are green — they
      are the release.
- [ ] The audit coverage test fails the build when a mutating route is un-audited
      (2.3-6).
- [ ] 2.2 → 2.3 Postgres upgrade test green, with devices preserved (2.3-7).
- [ ] Load test green with scoping enabled (2.3-8).
- [ ] `docs/upgrade-guide.md` states the visibility change up front.

**Wire contract:** additive fields (`client_id` on devices, client rows, the scope
header) → **MINOR**. Agents are unaffected: an agent's device simply carries a
client assignment. A technician's visible set shrinks, which is a behaviour change
for **users**, documented in the upgrade guide, and does not touch the
agent↔server pairing.

---

## Deferred inside 2.3 (so the release stays shippable)

- Per-client patch policies as a first-class scope value (tag scoping covers the
  need today; client-scoped policies come with 2.4.0's threshold work).
- Client-level quota/capacity limits and per-client billing meters (no pricing
  model exists to build against — `PRODUCT.md` forbids inventing one).
- Client-scoped `ENROLLMENT_SECRET` rotation UI (assignment at enrollment ships;
  rotation is 2.5.0 with the fleet update posture).
- Audit log export to SIEM/syslog, and immutable/hash-chained audit storage.
