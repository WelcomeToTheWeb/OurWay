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

## 2.3-0 · Schema: clients, device assignment, user scope · ✅ delivered

**Delivered:** `server/models/client.go` (`Client`, `NormalizeSlug`, and
`UnassignedClientID` as the *name* of a NULL — no row is stored for it),
`server/models/user_client.go` (the scope grant, a row per user+client,
unique-indexed as a pair), `Device.ClientID` (nullable, indexed), migration
**8** `clients_and_user_client_scope`, `ClientStoreInterface` + `ClientStore`
(`server/store/clients.go`), `DeviceStoreInterface.ListScoped`, and the mock
cascade. Correction to this file as written: the migration number in the **Do**
and **Acceptance** sections below says 7 — 2.2-1 consumed 7, so this shipped as
8 (matching the Context paragraph and `migrate.go`).

Measured: server coverage 39.50 % → **40.50 %**, agent 30.50 % → **33.00 %**
(baseline re-recorded). The 2.2 → 2.3 Postgres upgrade test
(`TestPostgresMigrationsUpgradeFromV22`) shows every device surviving the
upgrade, readable, `client_id IS NULL`, with `devices.client_id` indexed.

**Context:** `server/store/migrate.go` keeps an append-only list of contiguous
versioned migrations; the highest shipped is **version 7**
(`devices_input_and_capture_capability`, 2.2-1), and a shipped migration is never
edited. This lands as **version 8**.

**Do**
- [x] `Client` model (`server/models/client.go`): `id`, `name`, `slug` (unique,
      normalized like `NormalizeTags`), `notes`, `disabled`, timestamps.
- [x] `Device.ClientID` — nullable UUID, indexed, FK to `clients`.
- [x] `UserClient` junction (`user_id`, `client_id`) — the client scope of a
      `manager`/`technician`. `admin` is scope-all by role, with no junction rows.
- [x] Migration **8** `clients_and_user_client_scope`: create both tables, add
      `devices.client_id`, add the index. No data rewrite.
- [x] **Unassigned is `client_id IS NULL`, not a fake row.** The API presents a
      sentinel client (`id: "unassigned"`) in list responses, and assigning a device
      back to unassigned is `client_id: null`. One definition, used everywhere.
- [x] Cascade the store interface change to the mocks in `_test.go` files in the
      same change (`CONTRIBUTING.md`): `DeviceStoreInterface` gains
      client-scoped list signatures.

**Acceptance criteria**
- A 2.2 database migrates forward with every pre-existing device left
  `client_id IS NULL` and appearing under `unassigned` in `GET /api/clients`.
- Migration 8 is idempotent on re-run (the pattern `store/migrate_test.go` and
  `postgres_integration_test.go` already enforce).
- `slug` collisions return 409, normalized (lower-case, trimmed) like tags.
- Cross-compiles and the full gate green.

---

## 2.3-1 · Scope resolution: one place that answers "may this caller see it" · ✅ delivered

Shipped as `server/api/scope.go`: `ClientScope(c, store)`, `clientScope.AllowsDevice`,
`DeviceNotFound(c)` as the single 404 door, and `X-OurWay-Client-Scope`. Applied to
the device reads (`/devices`, `/devices/:id`, `/metrics`, `/metrics/history`).
Tests: `server/api/scope_test.go`.

**Do**
- [x] A single resolver in `server/api/` — `ClientScope(c, store)` (the caller's
      allowed client IDs, `all` for admin) and `clientScope.AllowsDevice(d)`.
- [x] Scope is resolved from the **user row + junction**, not from a token claim,
      so revoking a scope takes effect on the next request, not the next login.
- [x] Out-of-scope device → `404` with the same body as a nonexistent device
      (`{"error":"device not found"}`). Same shape, same status: scoping must not
      leak which IDs exist.
- [x] Scope changes to a user are audited (2.3-6) and take effect without
      re-authentication. *Both halves: `TestClientsGrantUsers` asserts the grant is
      live on the **next request with the same access token** (no logout, no
      re-login), and `TestAuditRecordsClientGrant` asserts the grant is a row —
      naming the tenant (`client_id`, `target_id`), the granted account and the
      `was` set it replaced, the technician's refused attempt as a
      `refused` row written by `RequireAnyRole` (which aborts before any handler,
      so no handler-side writer could have recorded it), and the tenant scoping of
      the rows themselves: the granted client's manager reads exactly their
      client's grant rows, and a manager of another tenant reads none.*

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

## 2.3-2 · Scope every read route · ✅ delivered

Enumerated from `server/api/handlers.go` — every route below takes a scope filter.
Shipped in the 2.3-2 branch: the by-device door `ScopedDevice(c, store)`, the
store-level scoped reads (`AlertStore.ListScoped`,
`SoftwareUpdateStore.ListByStatusesScoped`, `FileTransferStore.ListScoped`,
`DeviceStore.ListScoped`), and the role matrix test
`TestScopeRoleMatrixOnEveryReadSurface` in `server/api/scope_routes_test.go`.

| Route | Scoping |
|---|---|
| `GET /devices`, `GET /devices/:id`, `GET /devices/:id/metrics`, `/metrics/history` | by `device.client_id` |
| `GET /tags` (and `PUT /devices/:id/tags` reads the device first) | tags present in the caller's scope only — the counts are built over the caller's units, so the tag vocabulary cannot be a side channel to another client's fleet |
| `GET /alerts` | by the alert's device |
| `GET /sessions` | intersection of the existing owner/admin rule (2.2 era) **and** client scope |
| `GET /devices/:id/updates`, `GET /patch/overview`, `GET /patch/updates`, `GET /patch/deployments`, `GET /patch/deployments/:id/results` | by the update/deployment's device(s) |
| `GET /patch/policies` | policies whose scope is inside the caller's clients; admin sees all |
| `GET /files/transfers`, `/files/transfers/:id`, `/files/:transfer_id/file` | by the transfer's device; a download token for an out-of-scope transfer is 404 |

**Acceptance criteria**
- [x] A parameterized scoping test walks **every route above** × {admin, manager
  scoped to A, technician scoped to A, viewer scoped to A, technician with no
  client} and asserts: in-scope rows present, out-of-scope rows absent,
  out-of-scope by-ID requests **404** (`TestScopeRoleMatrixOnEveryReadSurface`).
- [x] Fleet views (`/patch/overview`, `/patch/updates`) are computed **inside**
  the scope — a scoped manager's compliance percentage is over their clients, not
  the installation. The fixture makes the numbers differ on purpose: client A
  carries three open updates, client B one, the unassigned unit none, so the
  installation's total (4) and A's total (3) are provably not the same number.
- [x] No route filters in the handler by post-filtering a full store read:
  scoping is a store-level query. Devices scope through `ListScoped`; the
  entity tables that reference a unit (`alerts`, `software_updates`,
  `file_transfers`) scope through `ListScoped` / `ListByStatusesScoped`, whose
  SQL carries `device_id IN (the caller's units)`. **Recorded limit:** a
  deployment's unit list is a serialized JSON array column, so deployments are
  filtered in the API layer over the caller's visible units — the one place in
  2.3-2 where the filter is not in SQL, and the reason is the column's shape,
  not convenience. `GET /patch/policies` likewise answers only the policies
  that can reach the caller's units (`all`, matching tags, matching devices).

---

## 2.3-3 · Scope every bulk and mutating route (the security task) · ✅ bulk rows delivered

**Delivered with 2.3-2** (the same door, the same tests): every by-device route
answers 404 out of scope (`reboot`, `stream`, `stream/stop`, `updates/scan`,
`sessions` start, `PUT /devices/:id/tags`); alerts by id answer
alert-not-found; `POST /updates/:id/approve` answers update-not-found; bulk
`/patch/deploy`, `/patch/scan`, `/devices/tags/bulk`, `/files/push`,
`/files/pull` refuse the whole request naming the out-of-scope ids; bulk
approval/skip refuses naming the out-of-scope update ids; sessions by id check
the unit's scope **in addition to** ownership.

**Status code, decided and recorded:** the plan's table says bulk refusal is
`403`. It ships as **`400` with the offending ids named**. 403 is this API's
RBAC answer — "your role may not call this endpoint" — and a caller whose role
is fine, on a request whose *targets* are not theirs, needs the second reading,
which is also the actionable one: remove the named ids and the request is valid.
Two meanings in one status code is what makes a console show "you lack
permission" to an operator who has it.

**Remaining:** `POST /patch/policies` scope refusal — vacuous today, because
those routes are `RequireRole("admin")` and admin is scope-all, so no creator
exists who cannot see a client; it becomes real the moment a manager may create
a policy.

Bulk routes are where tenancy leaks: they take lists of IDs, and a partial success
is an action on a client the caller must not touch.

| Route | Required behaviour |
|---|---|
| `POST /patch/deploy`, `POST /patch/scan`, `POST /devices/tags/bulk`, `POST /files/push`, `POST /files/pull` | every target device in scope, **or the whole request is refused** (`400`, body lists the out-of-scope IDs — see the recorded status-code decision above). No partial deploy, no partial queueing. |
| `POST /devices/:id/reboot`, `/stream`, `/stream/stop`, `/updates/scan`, `POST /devices/:id/sessions` | 404 when the device is out of scope |
| `POST /alerts/:id/resolve`, `/acknowledge`, `/assign` | 404 when the alert's device is out of scope |
| `POST /updates/:id/approve`, `POST /patch/updates/approve`, `/skip` | update belongs to an in-scope device; bulk approval refuses the batch otherwise |
| `POST /sessions/:id/input`, `/quality`, `DELETE /sessions/:id` | the session's device in scope, **in addition to** the existing ownership check |
| `PUT /devices/:id/tags` | 404 out of scope |
| `DELETE /devices/:id` | admin only, unchanged |
| `POST /patch/policies`, `PUT`, `DELETE` | admin only; a policy whose scope reaches a client the creator cannot see is refused with a named error |

**Acceptance criteria**
- [x] **Atomicity test:** a technician scoped to A calls `POST /patch/deploy`
  with `[A-device, B-device]` → `400`, **and the store shows no deployment row**
  (`TestScopeBulkRefusalWritesNothing`, via the `Count()` store spy added to
  `PatchDeploymentStore`, `SoftwareUpdateStore` and `SessionStore`). Same test
  proves `/devices/tags/bulk` moved no unit's tags, `/patch/scan` added no
  update rows, and a session start on a foreign unit created no session row.
- A technician scoped to A cannot start a session, send input, reboot, push a file,
  or resolve an alert on any B device — each asserted with the exact status code.
- An `admin` is unaffected: the full existing test suite passes unchanged.
- Scoping is checked **before** any side effect, proven by a store spy in the test
  (zero writes on a refused request).

---

## 2.3-4 · Client CRUD + the Clients surface · ✅ delivered (API #46, console #47, enrollment tokens 2.3-4c)

**Split, and why.** The API and the console surface are two PRs: the API is the
contract (and `docs/api.md` moves with it), and the console is the ledger, the
assignment UI, `ConfirmSheet` copy in three locales, and e2e. Landing them
together would make one review that cannot say which half a comment is about.
The enrollment-token pairing is a **third** piece — it is an authentication
surface, and it deserves its own gate, not a ride along with a ledger.

**A harness fact worth keeping:** `scripts/e2e-stack.sh` serves `vite preview`
over `web/dist` — it does not build. A console change is invisible to the suite
until `npm run build` runs, and the failure looks like a missing control, not a
stale bundle.

Shipped: `server/api/clients.go` — CRUD, `PUT /api/clients/:id/devices`
(assign batch and empty-out), `GET /api/clients/:id/rollup`, and
`GET`/`PUT /api/clients/:id/users` for the grants. Routes in
`server/api/handlers.go`, documented in `docs/api.md` (§ Client Endpoints).
Tests: `server/api/clients_test.go` (6 tests over the real router).

**Do**
- [x] `GET/POST /api/clients`, `PUT/DELETE /api/clients/:id`; `PUT /api/clients/:id/devices`
      (assign batch), `GET /api/clients/:id/rollup`. **Plus** `GET`/`PUT
      /api/clients/:id/users`, which the plan's 2.3-4 acceptance needs to grant a
      technician two clients in the first place — the grants are rows, and
      without an endpoint there is no way to create them outside the store.
- [x] `admin` manages clients; `manager` reads clients in scope. Deleting a client
      moves its devices to `unassigned` — never deletes devices. (`TestClientsCRUD`
      asserts a manager's `POST /api/clients` and `PUT /api/clients/:id` answer 403;
      `TestClientsTwoGrantsSeeExactlyTwo` asserts the scoped read.)
- [x] Assign a client at enrollment via the enrollment flow when
      `ENROLLMENT_SECRET` is paired with a client (a per-client enrollment token);
      otherwise a new unit lands in `unassigned`. **Delivered as 2.3-4c:**
      `client_enrollment_tokens` (migration 11), the admin-only
      `GET/POST /api/clients/:id/tokens` +
      `DELETE /api/clients/:id/tokens/:tokenId`, and registration accepting the
      token in `X-Enrollment-Secret`. The installation secret's answer is
      unchanged — unassigned — asserted by `TestGlobalEnrollmentSecretIsUnchangedByTokens`.
- [x] Console: Clients ledger (units, alerts by severity, patch compliance, last
      seen), client detail with unit list, assign/unassign, per-client technician
      assignment (`web/src/pages/Clients.tsx`, `web/src/api/clients.ts`, nav
      entry, `/clients` route).
- [x] Client filter as URL state (`?client=<slug>`) on the Clients ledger — the
      2.1.0 view-in-URL pattern, so the filtered view is linkable and survives a
      reload. On the other ledgers it is 2.3-5.

**Acceptance criteria**
- [x] Deleting a client leaves every device intact and `client_id IS NULL`,
  asserted in a store test (2.3-0's `TestPostgresMigrationsUpgradeFromV22`) **and**
  through the API (`TestClientsDeleteMovesDevicesToUnassigned`).
- [x] A technician assigned to two clients sees exactly those two in the ledger
  (`TestClientsTwoGrantsSeeExactlyTwo` — the client list, the fleet, and the
  `X-OurWay-Client-Scope` header all name exactly the two granted ids, and the
  unassigned unit is not among them).
- [x] `GET /api/clients/:id/rollup` numbers equal the device-ledger numbers
  computed independently — the same single-source rule as 2.2-4
  (`TestClientsRollupMatchesTheLedgers` recomputes units, units online, alerts by
  severity and patch compliance from `ListScoped` / `ListByStatusesScoped` and
  compares them field by field).
- [x] Every string in `en`/`de`/`fr` in lockstep, and `scripts/i18n-parity.py`
  (2.2-5) is green (436 → 474 key paths with 2.3-4).
- [x] Deleting a client confirms through `ConfirmSheet`, and the sheet states what
      the server actually does — the units survive, unassigned
      (`confirm.deleteClient.*`, the 11th destructive act migrated off
      `window.confirm`).
- [x] `web/e2e/clients.spec.ts` walks the MSP story in the console: create,
      assign, grant, then the technician's own console showing exactly that
      fleet, with the server's scope header asserting they are not scope-all.
- [x] **2.3-4c (the third piece, on its own gate):** the token places the unit in
      the tenant it names (`TestEnrollmentTokenPlacesTheUnitInTheTenant` — the mint
      response, the enrolled row's `client_id`, the list carrying **no secret**,
      the audit row naming the tenant and not the secret, and the `last_used_at`
      stamp); an inert token is indistinguishable from one that never existed
      (`TestInertEnrollmentTokenAnswersAsOneThatNeverExisted` compares the refusal
      **bodies**, so enrollment is not a probe for which tokens exist);
      `TestEnrollmentTokenRoutesAreAdminOnly` (a manager of the tenant may not read
      or mint its credentials); expiry validated at the boundary, with **omitting
      it meaning no expiry**; the credential table ships with 2.3 and carries its
      unique hash index (`TestPostgresMigrationsUpgradeFromV22`); tokens die with
      the client (`TestClientDeleteDropsItsEnrollmentTokens`); and the C4 gate
      stays shut against a tenant credential reaching another tenant's unit
      (`TestReRegistrationWithClientTokenAssignsOnlyUnassignedUnits` — found by the
      test, and the reason the grant is `the tenant the unit already belongs to`,
      not any live token). Console: `Clients.test.tsx` (mint prints the secret
      once, no control re-shows it, revocation confirms in words naming the
      consequence, a technician is asked for nothing and the read is not issued);
      `tenancy.spec.ts` mints in the console, enrolls through the agent's own
      registration call, and shows the unit in the tenant.

---

## 2.3-5 · Client filter and header on every ledger

**Do**
- [x] Client column + filter on Devices, Alerts, Patches, Sessions, Files;
      client header on device detail; per-client rollup on the dashboard.
- [x] The client is URL state (`?client=…`), following the 2.1.0 view-in-URL
      pattern — a filtered view is linkable and survives a reload.
- [x] The shift bar prints the operator's client scope, so a technician who
      "can't find" a unit sees the reason at a glance.
- [x] Density at MSP scale: the client filter is a facet in the existing filter
      row, not a new page.

**Delivered** (`ourway/2.3-5-client-filter`)
- Server: `GET /api/clients/rollups` answers every tenant the caller can see in
  **one** read — one device-list read, one grouped alert aggregate, one patch
  overview, one grouped grants count. `ClientStore.GrantsCount()` is the grouped
  count; `rollupOf` is shared with `GET /api/clients/:id/rollup`, so the two
  surfaces cannot drift by construction. `models.UnassignedClientID` is the row
  admin sees for units the 2.2 upgrade left unplaced.
- Web: `ClientFacet` in the filter row of Devices, Alerts, Patches, Sessions and
  Files writes `?client=<slug>`; `unassigned` is offered to admin only, because
  a scoped caller's answer to that filter is always empty. A facet with one
  option is not rendered at all.
- The tenant identity inside the console is the **slug**; the wire's
  `device.client_id` is translated once, at `clientSlugOfDevice`, because a
  filter chip, a link and a support ticket carry a slug and not a uuid.
  `useClientScope` is the one hook pages use for slug ↔ scoped unit set.
- `web/src/stores/fleet.ts` gained `clients` and `clientRollups` collections, so
  every facet, the dashboard rows and the Clients ledger read the same
  collection (2.2-2's single-source rule), and the shift bar's tenant names cost
  no extra read.
- Scope capture moved to the axios layer (`api/client.ts`): every read — devices,
  alerts, sessions, files, the audit ledger — updates the captured
  `X-OurWay-Client-Scope`, and `Layout` prints it with the names it resolves
  from the tenant collection. It is the server's statement, not the console's
  guess.

**Acceptance criteria**
- [x] On a fixture set of tenants × 40 units, Devices with a client filter
      renders in one list read and stays virtualized past 20 cards
      (`Devices.test.tsx` › virtualized cards: 40 units, a grid window, fewer
      mounted cards than the fleet).
- [x] `?client=alpha-co&status=alert&sort=last-seen` deep-links to exactly that
      view — the filter state is derived from the URL, not seeded from it, so
      the link is the view (`Devices.test.tsx` › tenant filter; `Alerts.test.tsx`
      › tenant filter; `DeviceDetail.test.tsx` › whose machine this is).
- [x] The dashboard's per-client rows agree with the Clients ledger: the server
      builds both from `rollupOf` (`TestClientsRollupsIsTheLedger` compares the
      list row with the per-client endpoint field by field), and the console
      renders both off the `clientRollups` collection
      (`ClientRollups.test.tsx`).
- [x] The shift bar states the scope the server applied — names for a granted
      manager, "all tenants" for admin, "no tenants" for an account with no
      grants, and nothing before a read has answered
      (`Layout.test.tsx` › tenant scope).

**Harness lessons recorded**
- The fleet store is module state with a freshness window: a page test that
  seeds collections must `resetFleetPoller()` **before** seeding, or the mount
  reads the previous test's collection and the fixture under test never runs.
- A page mount fans its reads through the api layer's read-slot queue, so the
  first row lands later than testing-library's default 1 s; these tests pass an
  explicit timeout.
- jsdom ships no `ResizeObserver`, which react-window needs to mount at all —
  shimmed once in `src/test/setup.ts`.
- A `MemoryRouter` never touches `window.location`; assert the URL the router
  is showing with a `useLocation` probe.

---

## 2.3-6 · Audit log (the Phase 1.2 item that shipped with zero code)

**Context:** `docs/phase1-tasks.md` lists "Audit log for user actions" as complete;
there is no table, no writer, and no UI. This lands as migration **9**.

**Do**
- [x] `AuditLog` model + migration 9: `id`, `actor_user_id`, `actor_username`,
      `actor_provider`, `action` (verb, e.g. `device.delete`, `patch.deploy`),
      `target_type`, `target_id`, `client_id`, `details` (JSON), `source_ip`,
      `created_at`. Index on `(client_id, created_at)` and `(action, created_at)`.
- [x] A store-level writer (`store.Audit.Record(...)`) called from handlers, **not**
      a gin middleware — middleware cannot know the target, the client, or whether
      the action succeeded.
- [x] Record every mutating route in `server/api/handlers.go`, plus: login
      success/failure, logout, password change, SSO JIT provisioning, role and
      client-scope changes, API key create/rotate/revoke, session start/end,
      reboot, device delete, tag changes, deploy/approve/skip/rollback.
- [x] Failed and refused actions are recorded with the outcome — a refused
      out-of-scope deploy is exactly the event an MSP wants to see.
- [x] Retention: `AUDIT_RETENTION_DAYS` (default 365), pruned by the existing
      retention manager pattern (`store/metrics_history.go` has the shape; M11's
      lesson is that a delete method with no caller is not a retention policy).
- [x] `GET /api/audit` (admin; managers see their clients' rows) with
      actor/action/client/date filters + CSV export.
- [x] Console: Audit ledger with filters, **including the entry points**: the unit
      view carries *This unit's record* (`/audit?target=<id>`) and the operator's
      Settings carries *My acts in the ledger* (`/audit?actor=<id>`), both shown
      only to the roles that can read the ledger. Follow-up closed; see the note
      below.

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

**Delivered (2.3-6)** — `server/api/audit.go`, `server/api/audit_list.go`,
`server/store/audit.go`, `server/models/audit.go`, migration 9,
`server/metrics/retention.go`, `web/src/pages/Audit.tsx`, `web/src/api/audit.ts`.

The decisions the code records, in the order they were forced:

- **The writer is deferred at the top of the handler, not placed above the 200.**
  `auditForAnswer` reads `c.Writer.Status()` when the handler returns, so the
  outcome column is the response the technician got, and every exit of a handler
  — success, 400, 404, 409, 500 — is a row. A record written as the handler
  *begins* is a ledger of intentions: it says a reboot happened when the agent
  never received one.
- **RBAC refusals are recorded by the middleware.** `RequireRole` and
  `RequireAnyRole` abort **before** any handler runs, so the handler-side writer
  cannot see the most important refusal class in an MSP product: a viewer
  pressing delete. `auditRoleDenial` records the actor, the action (from the
  registry, keyed on the matched route), the status, and the route.
- **The registry is one table, and the coverage test compares it to the live
  router.** `TestEveryMutatingRouteIsAudited` enumerates `SetupRouter`'s routes,
  keeps the mutating verbs (68 of them), and fails on any route with no action
  **and no written reason** for not having one. The skip list is checked in the
  same direction: an entry naming a route the router no longer has fails too, so
  the allow-list cannot rot into a list of claims about the product that stopped
  being true.
- **The agent-side writes are on that skip list, with reasons.** The actor of a
  `POST /api/agent/heartbeat` is a device key, not a person; a row for it would
  name a machine as the author of a human act. They are recorded where they
  belong — as device reports, deployment results, session rows.
- **`actor_user_id`, `target_id` and `client_id` are text, not `uuid`.** Postgres
  is what forced this: a `uuid` column rejects the empty string, and these
  columns are legitimately empty — a failed login has no authenticated user id,
  a bulk command refused before any target resolved has no entity id, a row
  about the installation has no client. A `uuid` schema cannot store the
  refusals the ledger exists to record, and it fails at migration time, not at
  the row that needs it. `TestPostgresMigrationsUpgradeFromV22` writes the
  empty-actor row and reads it back.
- **Retention shares the metric sweep, with counters.** `RetentionManager.
  WithAudit(...)` prunes the ledger on the same hourly tick as metric history;
  `Counters()` and `RetentionSummary()` report rows removed, last run, and
  failure. The ledger endpoint prints the same numbers, so the page states the
  promise and the evidence together. A failed ledger prune is counted and
  logged and never aborts the metric cleanup (`TestRetentionSurvivesAuditFailure`).
- **The CSV export is an authenticated request, not a link.** A link cannot
  carry the `Authorization` header, and the shape that works — `?access_token=`
  in the URL — puts a live bearer token in browser history, proxy logs and
  `Referer` headers. The export goes through the authenticated client as a Blob,
  and `limit` is capped server-side at 1000 so "the whole log" is bounded by
  design.
- **A `client_id` the caller is not granted answers an empty ledger.** The
  ledger's client ids are a list of the tenants that exist in the installation;
  an error naming the one you may not read leaks what the 404 rule closes.
- **Console tests cannot use the generic key-path guard here.** The ledger's own
  vocabulary is dotted by design — `device.delete`, `patch.deploy` — so
  `Audit.test.tsx` builds its guard from the locale file's real key paths. A
  test that fails on correct output is a test that gets switched off.

**Follow-up closed (the deep links).** The ledger's filters are URL state, so the
deep link is a link: `?target=<unit id>` is that machine's record and `?actor=<user
id>` is one account's acts, each arriving as a stated chip with a one-click clear.
`target` selects by **record target**, not by tenant, so a unit that has moved
between clients keeps one continuous history — that is what "what happened to this
machine" asks for. The filters are filters: the caller's tenant filter runs on top,
so `?target=` for a unit outside the grant reads an empty ledger
(`TestAuditDeepLinkFiltersByTargetAndActor`, which also asserts the target filter
and the actor filter never leak a foreign row). The unit view offers the link only
to `admin`/`manager` — a technician's `GET /api/audit` is a 403, and a link that
lands on a refusal is worse than no link — asserted from the technician's own
sign-in in `tenancy.spec.ts`, with the positive half followed through the admin's
click in `audit.spec.ts` (link → chip → URL → every row names the unit).

---

## 2.3-7 · Upgrade path and the behaviour change, stated plainly · ✅ delivered

**Do**
- [x] Postgres integration test (the pattern in
      `server/store/postgres_integration_test.go`): a 2.2 schema upgraded to 2.3,
      every device readable, `unassigned` present, admin behaviour unchanged.
      `TestPostgresMigrationsUpgradeFromV22` builds a fleet the way the product
      writes one, **rolls the schema back to 2.2** (drops `client_id`,
      `user_clients`, `clients`, and the migration records above version 7),
      re-runs `Migrate` twice — the upgrade and its idempotency — then requires
      every device readable, with no client, and the full applied set present.
- [x] `docs/upgrade-guide.md`: **a technician's visible set shrinks after this
      upgrade** — stated in the section right after the overview, not in a
      footnote: every unit arrives unassigned, unassigned is invisible to every
      non-admin, the one-time assignment is given in order (create → assign →
      grant), and `X-OurWay-Client-Scope` is named as the checkable answer to
      "the unit is gone".
- [x] `docs/user-guide.md`: **Clients and Tenants** (creating a tenant, assigning
      units, granting accounts, what a non-admin sees, the tenant facet as URL
      state) and **Audit Log** (who may read it, refusals are rows, filters, CSV,
      retention, the log starts empty on upgrade); the roles table now says
      "the units of the clients granted to them", and the shortcuts list gains
      `g c` and `g i`.
- [x] `docs/api.md`: every new route, in the same pass as the 2.2-7 route↔docs
      diff check (which this release must keep green) — clients CRUD, assignment,
      grants, rollup and rollups, `GET /api/audit` and `/api/audit/actions`.
- [x] `docs/migration-guide.md` / changelog section for `v2.3.0`, per
      `docs/versioning.md`: "What's New in 2.3", "Migrating to 2.3" (the
      migration numbers 8/9/10, the one-time assignment, the verification
      checklist, the FAQ answer that agents are untouched), and a
      `[2.3.0] - Unreleased` changelog section.

**Acceptance criteria**
- The integration test proves the upgrade is non-destructive and idempotent.
- The upgrade guide states the visibility change in the first section, not in a
  footnote.
- Route↔docs check green with all new routes documented.

---

## 2.3-8 · Scale: scoping must not cost a query per row · ✅ delivered

**Do**
- [x] Scoping is a store-level `WHERE` on the client set, not a loop of per-device
      checks: the 10,000-device load tool runs with a technician scoped to 2,000
      devices and the list request count is unchanged from 2.2.
      `store.scopeByClient` (`server/store/scope.go`) states the rule as a
      subquery — `device_id IN (SELECT id FROM devices WHERE client_id IN (…))` —
      so the statement's bind-parameter count is the caller's **grant** count,
      never their unit count. `TestScopedListQueryCountIsConstant` and
      `TestScopedListQueryIsOverTheClientSet` count statements and bind parameters
      on a 400-unit tenant; the 2.3-2 shape (`device_id IN (…units…)`, with the
      fleet read in full to build that list) fails the second test.
- [x] The scope resolution is fetched **once per request**, not once per route
      check (a bulk deploy with 500 targets must not do 500 scope queries).
      `clientScope.VisibleDeviceIDsIn` caches the resolved set on the request, and
      `AllowsDeviceID` takes the `*gin.Context` for exactly that reason.
      `TestBulkTargetChecksCostOneFleetRead` asserts the fleet-read count is a
      constant for 1 target and for 200.
- [x] Index on `devices.client_id` verified with `EXPLAIN` on the device-list query.
      `TestPostgresScopedReadUsesTheIndexes` asks the planner on Postgres (400
      units, `enable_seqscan = off`): the fleet side must walk `idx_devices_client_id`,
      the entity side must be an index access, and a `Seq Scan` on `alerts` fails
      the build. Migration 10 (`tenant_scoped_read_indexes`) adds the composite
      indexes those reads are shaped for — `(device_id, created_at)` on `alerts`,
      `sessions` and `file_transfers`, `(device_id, status)` on `software_updates`
      — because every one of these reads filters by unit *and* orders by recency.
- [x] The e2e suite from 2.2-0 gains a tenancy flow: scoped login → filtered
      ledger → device detail → action → audit row visible
      (`web/e2e/tenancy.spec.ts`, green in the Redis-shaped stack, which is CI's shape).

**Acceptance criteria**
- Load test: 10,000 simulated devices, scoped technician, sustained ~100 req/s,
  **zero failures**, and list-query latency within 10% of the unscoped baseline.
- A query-count assertion in the scoping test (no N+1) fails if scope checks scale
  with the target count.
- The e2e tenancy flow is green in CI.

**Measured, 2026-10-28** — `scripts/scale-tenancy.sh`: 10,000 units registered, 5
_tenants_ of 2,000, one technician per tenant, 20 workers at 100 req/s for 30 s per
actor (`RATE_LIMIT_PER_MINUTE` raised, because a 100 req/s campaign is above the
per-user budget by design and this measures the cost of the scoped read):

| read | unscoped baseline p95 | scoped technician p95 | failures | verdict |
|---|---|---|---|---|
| `/api/devices` | 830 ms | 124 ms | 0 | PASS |
| `/api/alerts` | 122 ms | 64 ms | 0 | PASS |
| `/api/patch/overview` | 746 ms | 137 ms | 0 | PASS |

The scoped read is the **faster** one, which is the point: the baseline pays for
10,000 rows, the technician pays for their tenant.

**What the load pass surfaced (fixed in this pass).** The Redis device cache stores
the row as JSON, and `models.Device.DeviceKey` is `json:"-"` (C3: the key is never
serialized), so a cached read answers a unit with an **empty** device key. Every
handler that loads a unit and `Save`s the whole row back — tags, reboot, client
assignment, the WS hub — therefore wrote `device_key = ''`: the first such write
locks that unit's agent out, and the next fails on `idx_devices_device_key`
(`Key (device_key)=() already exists`), which reaches the operator as a 500 on
`PUT /api/devices/:id/tags`. Invisible with Redis off, which is why the plain stack
never saw it. `DeviceStore.Update` now omits `DeviceKey` (`Create` still writes it:
enrollment is where the key is minted), and `TestUpdateNeverWritesTheDeviceKey`
reproduces the collision without the fix.

**Also fixed here:** `web/e2e/clients.spec.ts` asserted its scope header from an
in-page `fetch` that sent the scheme as `bearer`, while `server/api/middleware.go`
trims the prefix with `strings.TrimPrefix(authHeader, "Bearer ")`. The read was
answering 401 with no scope header, so `not.toBe('all')` and a one-item split were
passing on a refused request. The scheme is spelled exactly now, and a non-200 is a
test failure rather than an empty string.

---

## Release gate for `v2.3.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/i18n-parity.py
scripts/release.sh --check 2.3.0
```

Plus, non-negotiable for this release:

- [x] The scoping matrix (2.3-2) and the atomicity tests (2.3-3) are green — they
      are the release. **Measured:** 16 `TestScope*` tests pass on `./api`
      (`TestScopeRoleMatrixOnEveryReadSurface`, `TestScopeEveryDeviceRouteKindAnswersTheSameNotFound`,
      `TestScopeFleetListsCarryNoForeignRows`, `TestScopeBulkRefusalWritesNothing`,
      `TestScopeBulkTargetsFailNamingTheIds`, `TestScopeRevocationTakesEffectOnTheNextRequest`,
      …), and the whole server suite is green under `-race`.
- [x] The audit coverage test fails the build when a mutating route is un-audited
      (2.3-6). `TestEveryMutatingRouteIsAudited` enumerates the live router and
      fails on a mutating route with no registered action *and* no written skip
      reason — and it fails on a skip entry that names a route the router no
      longer has, so the exemption list cannot rot.
- [x] 2.2 → 2.3 Postgres upgrade test green, with devices preserved (2.3-7).
      **Measured** with `OURWAY_TEST_PG_DSN` set: `TestPostgresMigrationsUpgradeFromV22`
      rolls a 2.3 schema back to 2.2 (dropping `client_id`, `user_clients`,
      `clients`, the enrollment-token table, and the migration records above
      version 7), re-runs `Migrate` twice, and asserts every device survives with
      no client — the upgrade assigns nothing.
- [x] Load test green with scoping enabled (2.3-8) — 10,000 units, a technician
      scoped to 2,000, 100 req/s, zero failures, scoped p95 under the unscoped
      baseline; the numbers are in 2.3-8.
- [x] `docs/upgrade-guide.md` states the visibility change up front — it is the
      section right after the overview, not a footnote.
- [x] **Cut:** `v2.3.0` tagged 2026-10-28 at `9bbfccc` (`chore(release): 2.3.0`),
      cut by `scripts/release.sh 2.3.0` with the gate run in full on the release
      commit — server, agent (linux/windows/darwin-arm64), cli + installer + viewer
      (+ windows viewer), web (`npm ci`, tests, build), coverage floors, i18n parity,
      browser-dialog check. Publishing runs on the tag: `ourway/{server,web}:v2.3.0`
      plus `:2.3` and `:latest`, the agent/installer binaries, and the GitHub
      release. The version is the tag — see `docs/versioning.md`.

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
