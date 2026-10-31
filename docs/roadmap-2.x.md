# OurWay Roadmap — 2.x

**Status:** Active
**Supersedes:** [`docs/roadmap-2.0.md`](roadmap-2.0.md) (kept as the 2.0 record)
**Baseline:** `v2.1.0`
**Scope of this document:** the next four minor releases, their scope, and what each
one must pass before it is tagged.

Release numbering follows [`docs/versioning.md`](versioning.md): the git tag is the
version, no version is invented before the work exists, a wire-contract **shape**
break is a MAJOR, a new capability is a MINOR, a fix is a PATCH. Nothing below
requires a MAJOR — every change is additive to the wire contract.

No dates are promised. Releases ship when their scope passes the gate.

---

## Why this plan

Roadmap 2.0 is functionally complete except Phase 4.3 (testing & quality). The
2026-10 remediation closed every Critical, High, and Medium finding in
[`buglist.md`](../buglist.md). 2.1.0 rebuilt the console and established the
release pipeline.

What is left is not a list of missing features — it is the distance between what
the product **claims** and what the code does. Four claims are currently untrue:

| Claim | Reality (verified in code) |
|---|---|
| Remote control on Windows, Linux **and macOS** | `agent/session/input_darwin.go` — `synthesizeKey` and `synthesizeMouse` are stubs that log. **macOS remote control is view-only.** |
| "Remote session viewer (WebRTC)" (README) | WebRTC was removed from the server (`buglist.md` H4). Frames travel over HTTP `POST /api/sessions/:id/frame`; input over the device WebSocket. `agent/session/webrtc.go` is dead code and drags `pion/webrtc/v3` into the agent's direct dependencies. |
| MSP with many client fleets is the primary user (`PRODUCT.md`) | No tenant/organization/client entity exists anywhere in `server/models`. RBAC is a global role: a `technician` sees **every** device in the installation. |
| Per-device and per-group custom thresholds (roadmap 2.0, Phase 1) | `server/alerts/engine.go` holds one global `thresholds` map; `SetThresholds` replaces it wholesale. No per-device, per-group, and no rules UI. |

The 2.x plan closes those gaps first, in the order that a 200-endpoint MSP feels
them.

---

## v2.2.0 — Make the claim true

**Theme:** every capability the product advertises works on all three platforms,
and the console stops throttling itself.

**Task breakdown with acceptance criteria:** [`phase2.2-tasks.md`](phase2.2-tasks.md)

| Scope | Detail |
|---|---|
| macOS remote input | **Delivered 2026-10-26**: CGEvent posting on darwin (key/mouse/scroll, with the browser's modifier state on every key), the Accessibility (`AXIsProcessTrusted`) gate, `ourway-agent --grant-input-permission`, and the state surfaced as `input_capability` + `capture_state` on the unit view and session panel. Consequence for the release: **macOS agents must ship as cgo builds** (`CGO_ENABLED=1` on a macOS builder, arm64 + amd64) — a `CGO_ENABLED=0` cross-compile reports `unavailable` and drops input by design. The real-Mac control gate (type/click/drag/right-click/scroll, control surviving a self-update at a fixed path) **passed 2026-10-27**, which closes 2.2-1's last open item. |
| Shared fleet store | **Delivered 2026-10-26**: `web/src/stores/fleet.ts` + `useFleet()` — one ref-counted poller per collection, pages as views. Measured on the seeded e2e fleet: cold dashboard mount 7 reads (14 in the finding), a unit view opened from it adds zero fleet reads, two surfaces ≈ 15 reads/min against the documented 100. The rate-limit rest (`useRateLimitAwarePoll`) folded into the shared poller. |
| Rate-limit headroom | With the shared store in place, re-measure the 100 req/min user budget (`server/api/ratelimit.go`, burst 10) and make the budget configurable, documented, and correct in `configuration.md`. **Delivered 2026-10-26** as `RATE_LIMIT_PER_MINUTE`/`RATE_LIMIT_BURST`, printed in the shift bar. The shared store (2.2-2) supplied the re-measurement: two surfaces open ≈ 15 reads/min, so the default budget is headroom, not a wall — the remaining question is the server-wide default, not the console's appetite. |
| Confirm-sheet finish | **Delivered 2026-10-27** (2.2-9): `web/src/components/ConfirmSheet.tsx` + `confirmRequest()` in `web/src/utils/confirmSheet.ts`; the ten destructive actions confirm in the page, with the target named in the title, the consequence in the body and the verb on the control, copy in `confirm.<action>.*` in `en`/`de`/`fr`, and `scripts/browser-dialogs-check.sh` failing CI on any native dialog call. Correction to the finding as written here: `ConfirmSheet` was **not** shipped by 2.1.0 — the component did not exist in the tree; the sheet is built by this task. `window.alert` is gone from the console too (Settings reports its result inline). |
| Alert count reconciliation | **Delivered 2026-10-26**: one definition — *open is unresolved, acknowledged included* — in `web/src/utils/alerts.ts`, pinned server-side by `server/store/alerts_open_test.go`. The ledger owns the shared `alerts` collection and filters client-side; the dashboard tile rides on it instead of counting units whose status is `alert`; count chips measure the filtered population; the WebSocket alert event invalidates the collection so counts move on the event. |
| i18n lockstep finish | Admin surfaces still carry hardcoded English (verdict debt #3). Finish `en`/`de`/`fr` in lockstep across `Settings`, `PatchPolicies` help copy, and the new fleet-store strings. |
| Empty-state copy | History micro-fields at <3 samples render an empty ruled slot with no "no samples yet" (verdict debt #4). |
| Documentation truth | README's remote-session claims, the 2.0-era narrative docs, `docs/api.md` session endpoints, and the versioning doc's "last released version" line. |

**Gate to tag:** the standard gate green; macOS remote input exercised against a
real macOS unit (view **and** control); the dashboard mount stays under budget
with two console tabs open; vitest green with a fleet-store test; i18n key parity
check across the three locale files.

**Wire contract:** additive only — new WS message types for the fleet store are
MINOR. Agents are unaffected.

---

## v2.3.0 — MSP tenancy

**Theme:** the release that makes "OurWay for MSPs" true. The stated primary user
has no support in the data model today.

**Task breakdown with acceptance criteria:** [`phase2.3-tasks.md`](phase2.3-tasks.md)

| Scope | Detail |
|---|---|
| Client entity | `client` (name, slug, notes) + device→client assignment, as a new numbered migration in `server/store/migrate.go`. Untagged devices land in an implicit `Unassigned` client so every existing install keeps working. |
| Scoped visibility | `manager`/`technician` scoped to a set of clients. Every list route filters by the caller's client scope; scoping enforced server-side, not in the console. |
| Scale of the scoped read | **Delivered 2026-10-28** (2.3-8): scoping is a `WHERE` over the caller's **clients**, not a list of their units and not a per-row check — `store.scopeByClient` states it as `device_id IN (SELECT id FROM devices WHERE client_id IN (…))`, so a 2,000-unit tenant's list statement carries the same two bind parameters as a two-unit tenant's; the scope is resolved once per request (`VisibleDeviceIDsIn`). Migration 10 adds the composite indexes the semi-join walks (`(device_id, created_at)`, `(device_id, status)`), `TestPostgresScopedReadUsesTheIndexes` asks the Postgres planner to prove it, and the three query-count tests fail the build if a check scales with the target count. Measured: 10,000 units, technician scoped to 2,000, 100 req/s, zero failures, scoped p95 124/64/137 ms against an unscoped baseline of 830/122/746 ms. |
| Client filter everywhere | **Delivered 2026-10-27** (2.3-5): a tenant facet in the existing filter row of Devices, Alerts, Patches, Sessions and Files, the tenant column on the unit sheets, the tenant line on the unit record, and per-client rows on the dashboard from `GET /api/clients/rollups` — one read, the same `rollupOf` arithmetic the Clients ledger uses. The filter is URL state (`?client=<slug>`, `unassigned` for admin), so a filtered view is a link, and the shift bar prints the scope the server said it applied on the read. |
| Audit log | **Delivered 2026-10-27** (2.3-6): `audit_log` as migration 9, with actor, action, target, client, outcome, details, source IP. Written by a deferred `auditForAnswer` in the handlers, so the outcome column is the status the caller got, and by `auditRoleDenial` in the RBAC middlewares, which abort before any handler runs — the viewer's refused delete is a row. `TestEveryMutatingRouteIsAudited` compares the registry against the live router and fails the build on a mutating route with no action and no written reason. Refusals are rows; retention is `AUDIT_RETENTION_DAYS` (365), pruned by the metric retention manager's hourly sweep with counters the ledger page prints. The Phase 1.2 item that shipped with zero code now has a table, a writer, a gate and a page. |
| Console surfaces | Clients page (**delivered 2026-10-27**, 2.3-4 console half); audit ledger (**delivered 2026-10-27**, 2.3-6: filters are URL state, CSV export, scope and retention stated on the page); client header on device detail (**delivered 2026-10-27**, 2.3-5: the tenant's name on the unit record, linking to that tenant's filtered fleet, "Unassigned" for a unit the upgrade left unplaced). |
| Tags stay as they are | Device tags and tag-scoped patch policies (`d3d4a0a`, `48e3e98`) are orthogonal to clients and stay; clients are for **who can see**, tags for **what a policy touches**. |

**Gate to tag:** standard gate; a Postgres integration test proving migration from
a 2.2 database assigns every pre-existing device to `Unassigned`; a scoping test
proving a technician scoped to client A receives **404/empty** for client B's
device IDs on every route (not a filtered list — the object must not be reachable);
audit log assertions on the mutating routes; audit and client strings in all three
locales.

**Wire contract:** additive fields (`client_id` on devices, `scope` on users) →
MINOR. Agents are unaffected. A technician's visible set shrinks — that is the
point, and `docs/upgrade-guide.md` must say so plainly, because it is a
behaviour change an operator has to expect.

---

## v2.4.0 — Escalate to humans, own your identity

**Theme:** the alert bus exists; nothing reaches a person. And the identity
story stops being three vendors' hosted endpoints.

**Task breakdown with acceptance criteria:**
[`phase2.4-tasks.md`](phase2.4-tasks.md)

| Scope | Detail |
|---|---|
| **Generic OIDC provider** (lead item) | **Delivered 2026-10-29** (2.4-1 discovery, 2.4-2 verification, 2.4-3 contract, 2.4-4 console). Configure OurWay against **any** conformant IdP — Keycloak, Authentik, Zitadel, kanidm, Okta, Entra ID with a fixed tenant, JumpCloud, a home IdP: **issuer + OIDC discovery**, **`id_token` verification** (JWKS, `iss`/`aud`/`exp`/`nonce`, PKCE), **claim mapping**, **role mapping**, **domain allow-list**, multiple concurrent providers ordered, a **test-connection** probe as named states, and the provider ledger. The pre-2.4 shape this replaced: `applyProviderDefaults` hardcoded the big-3 endpoints and `web/src/pages/SSO.tsx` hardcoded the dropdown, and identity came from the userinfo call alone with the raw `id_token` stored in `users.sso_attributes`. |
| Per-device / per-group thresholds | **Delivered 2026-10-29** (2.4-5): threshold overrides on device, client, and tag; the engine resolves device → tag → client → global. Alert-rule configuration UI in the console (roadmap 2.0 Phase 1 open item, `docs/phase1-tasks.md:50-56`). |
| Notification channels | **Delivered 2026-10-29** (2.4-6): Email (SMTP), Slack, and Microsoft Teams on top of `server/events` — the webhook machinery (HMAC signatures, retries, delivery tracking) reused, not duplicated. Per-client routing: "client A's critical alerts go to my phone". |
| SAML 2.0 SSO | **Delivered 2026-10-30** (2.4-7): the feature `buglist.md` L8 declared and guarded as missing is implemented, and L8's guard survives as its rule — a provider row that cannot answer logins is refused at save, loudly, and an unknown protocol type is still refused. A provider row is `oidc` **or** `saml`: SP metadata per provider (public, no key material), an assertion consumer service, XML-DSig assertion verification against the operator's pinned certificate, audience/recipient/destination/expiry/subject-confirmation checks, replay protection, and the **2.4-3 identity contract unchanged** on the asserted identity — one contract, two protocols. Ordered **after** generic OIDC as planned: discovery-based OIDC covered Keycloak/Authentik/Zitadel/Okta; SAML needed SP metadata, XML signing and an assertion consumer. **Interop pass run 2026-10-31** (2.4-0, SAML half): a live Keycloak 26 realm at `kc.sso.test`, the client created from **OurWay's own metadata**, signed requests and assertion signature on, realm certificate pinned — a browser SP-initiated flow lands at the ACS 302 with a single-use code that exchanges for tokens. It found three real bugs, each pinned by a test: exclusive canonicalization dropped the **namespaces in scope from the enclosing `Response`** (Keycloak declares `xmlns:saml` there and only the default namespace on the assertion, so a valid assertion digested to bytes the IdP never signed — the vendor-shape case `testsaml` could not produce, because our fixture declared everything on the assertion); `AuthnRequest.Destination` was our ACS URL where the profile names the **IdP's SSO location** (`invalid_destination`); and the HTTP-Redirect signature covered the decoded parameters, not the **percent-encoded query string the browser transmits** (`invalid_signature`). The proof is a permanent probe, `server/sso/capture_vendor_saml_test.go`, fed a captured vendor document through the environment — validation, one tampered NameID byte refused, the stripped signature refused — skipping when no capture is configured, so the suite stays hermetic. **Open:** the OIDC half of 2.4-0 (Keycloak discovery through the *published* issuer, which needs `localhost` issuer support first) and a second vendor (AD FS). |
| Session recording | **Delivered 2026-10-30** (2.4-8, migration 17 `session_recordings`). Frame-sequence recording with playback, so "who was controlling that machine at 14:00" is answerable (roadmap 2.0 §2.1). The policy is the installation's — `RECORD_SESSIONS=off|on|control-only`, **off by default**, `RECORDINGS_DIR`, `RECORDING_RETENTION_DAYS` — and the toggle starts a recording while nothing in the product stops one: the only end a recording knows is its session's end. Frames come from the path the live session already uses, stored as an append-only `OWRS1` sequence (no container, no re-encode, no ffmpeg): a crash leaves the earlier frames readable, a restart appends to the same file and states the interruption, and the size cap counts what it dropped. Playback in the console shows the frames with their times and the input events the audit ledger recorded for that session, laid over the frames they produced — the events are read from the ledger, never copied into a media file. Reading a recording is scoped (2.3.1) and audited: opening it writes `session.recording_play`, exporting writes `session.recording_fetch`, and starting one writes `session.record_start`. A manager/technician reads their tenants' recordings **and always their own**. |

### The security contract the OIDC item must ship with

Generic OIDC widens the identity surface, and the current JIT path has an
account-linking behaviour that a permissive IdP turns into **account takeover**:
`findOrCreateUser` falls back to `Users.FindByEmail(info.Email)` and then stamps
`Provider`/`ProviderID` onto the existing local account
(`server/sso/oauth.go:189-210`). A provider that lets anyone assert
`ops-admin@corp.example` therefore claims that account, including its local,
password-authenticated admin. Custom providers must therefore ship with:

1. **Domain allow-list** — an IdP may only assert identities in the domains the
   operator lists; assertion outside it is refused.
2. **Verified-email requirement** — `email_verified` / the IdP's email-claim
   verification is honored, not ignored (`UserInfo.EmailVerified` is parsed today
   and never checked).
3. **Explicit-linking default for new provider/email pairs** — JIT creates a user
   in a pending, no-role state (the `viewer` default from `HandleCallback` stays
   the floor) and linking an **existing** account requires an admin to confirm the
   match. No silent promotion, no silent link.
4. **Role mapping is opt-in** — if configured, it owns role assignment; if not,
   the `viewer` floor is unchanged. Mapping from a claim an IdP lets a user
   self-edit is a privilege-escalation path, so the mapping is admin-declared
   against a claim the operator controls.
5. **`iss`/`aud`/`exp` verified on the `id_token`** before any identity claim is
   trusted, and the raw `id_token` stops being stored in `users.sso_attributes`.

**Gate to tag:** standard gate; threshold-precedence tests in `server/alerts/`;
channel delivery + retry tests with a mock SMTP/Slack/Teams endpoint (the Phase 3
unit-test items, `docs/phase3-tasks.md:83-86`); an OIDC test-IdP fixture covering
discovery, JWKS rotation, a bad `aud`, a domain outside the allow-list, an
unverified email, and an attempt to link an existing admin account (which must
land in pending, not linked); SAML round-trip against a real IdP fixture (**shipped
2026-10-30 against `server/sso/testsaml`, a Go peer IdP that signs real XML-DSig
assertions and sabotages itself on command; the vendor-container pass over the
metadata + pinned-certificate path is the open half**); recording/playback test on a
recorded frame sequence (**shipped 2026-10-30**: `server/recordings/writer_test.go`
pins the format's promises — append-only, resumable, truncated-tail readable, cap
counted — and `server/api/recordings_test.go` runs the recording through the routes,
including a restart mid-session and the scoped 404).

**Wire contract:** new event types and new routes → MINOR. New provider fields
are additive on the provider row; the admin-only provider endpoint keeps
`client_secret` as `json:"-"` as it is today.

---

## v2.5.0 — The ops plane

**Theme:** a self-hosted operator can see what their infrastructure is doing, and
what is broken about it (`PRODUCT.md`, Product Principle 5).

**Task breakdown with acceptance criteria:**
[`phase2.5-tasks.md`](phase2.5-tasks.md)

| Scope | Detail |
|---|---|
| Self-metrics | Counters and histograms for WS drops, full-queue drops, frame throughput, alert-engine latency, session counts, queue depth, deploy outcomes. The 2026-10 review's standing complaint: "no dropped-frame counters anywhere" (`buglist.md` M8, M10). |
| `/metrics` | Prometheus-format endpoint, documented and optional (auth-scoped). |
| Console server-health surface | Redis connected/degraded, DB pool, WS connections per instance, queue depth, agent version spread across the fleet, and the update channel each unit is on. |
| Patch parity | Linux: pacman and zypper (`docs/roadmap-2.0.md` §2.2 promised them; `agent/patch/patch.go` implements apt + yum/dnf only). macOS: rollback support (`agent/patch/patch.go:626` currently refuses). |
| Fleet update posture | Console view of which agents are on which version and channel, stalled self-updates, and a staged roll-out control. (Built on the ordering rule from `docs/versioning.md`.) |

**Gate to tag:** standard gate; a load-test run against the 10,000-device
simulation with the metrics path enabled and **zero** silent drops; patch tests
against mock package managers (`docs/phase2-tasks.md:169`).

---

## Continuous lanes (not releases)

These run across every release, and are the honest completion of roadmap 2.0
Phase 4.3:

1. **Test coverage toward 80%.** The roadmap records 23.7% at Phase 4.1; that
   number has not been re-measured since. Re-measure at the start of 2.2 and
   ratchet upward per release. `server/ws`, `server/store`, `server/sessions`,
   and the agent's platform code are the thin spots.
2. **Playwright end-to-end suite in CI** — login, device list, alert acknowledge,
   patch approve/deploy, session start.
3. **Agent smoke test on all three OSes in CI**, including the macOS remote-input
   path added in 2.2.
4. **OWASP Top 10 pass** with findings written into `SECURITY.md`.
5. **Performance benchmarks** as a tracked artifact, not a one-off.
6. **Security fixes ship alone as PATCH releases**, never bundled with features
   (`docs/versioning.md` rule 5).

---

## Explicitly deferred

Deliberately not in 2.x, so the four releases above stay four:

- GraphQL endpoint (roadmap 2.0 marked it "optional, evaluate demand")
- Native mobile app (roadmap 2.0 open question #4 — deferred to a 3.x decision)
- Plugin/extension marketplace (roadmap 2.0 open question #6)
- Multi-instance HA beyond the existing Redis pub/sub sharing
- PostgreSQL read replicas
- Per-agent licensing or SaaS packaging (outside the MIT positioning in `PRODUCT.md`)

---

## Success metrics for the 2.x line

| Release | Measured outcome |
|---|---|
| 2.2.0 | Remote control works on all three OSes. A dashboard mount costs under 10 reads. Zero hardcoded English in the console. Every documented endpoint exists. |
| 2.3.0 | A technician scoped to one client cannot read another client's devices on any route. Every mutating action appears in the audit log. |
| 2.4.0 | A critical alert on a client reaches a human outside the console within one engine pass. OurWay logs in against a self-hosted IdP via issuer + discovery, with `iss`/`aud` verified and no identity accepted outside the domain allow-list. SAML works against a real IdP — proven against a live Keycloak 26 realm over the metadata + pinned-certificate path, with a vendor capture as a permanent probe; a second vendor (AD FS) is not covered. "Who was controlling that machine at 14:00" is answerable from a recording, and reading one is scoped and audited. |
| 2.5.0 | WS drops are counted, visible in the console, and non-zero drops are explainable. Linux fleets beyond apt/dnf are patchable. |

**2.x line-level metric:** a 200-endpoint, three-client MSP can run a full shift
— triage, act, remote-control, patch, hand off to a colleague — without leaving
the console and without hitting a rate limit.

---

## Decisions settled in this plan

Recorded so the next session does not re-open them:

- **Generic OIDC stays in 2.4.0**, not pulled into 2.3.0. Tenancy stays about
  tenancy; identity work (OIDC, its linking contract, SAML) lands together with the
  escalation features that consume identity. (Settled 2026-10-26.)
- **WebRTC stays removed.** Frames travel over HTTP with input over the device
  WebSocket. Any future latency work is a new decision with a benchmark in front of
  it, not a resurrection of the deleted gateway (`buglist.md` H4).
- **Tenancy is client-scoped visibility enforced at the route** — an out-of-scope
  device returns 404 with the body a nonexistent ID returns. Console filtering is
  not the feature.
- **Bulk mutations refuse the whole request** when any target is out of scope. No
  partial deploys.
- **The `viewer` floor is the default for SSO-provisioned identities**, and linking
  an SSO identity to an existing local account requires an admin's confirmation.
- **macOS input (2.2-1) ships with the Accessibility-permission state surfaced** in
  the console. A capability that silently no-ops is treated as a bug, not a limitation.

---

## Release hygiene carried forward

From the release pipeline established in 2.1.0:

- `scripts/release.sh <x.y.z>` cuts every release: refuses a dirty tree, a
  non-`main` branch, an existing tag, and a version not greater than the latest
  tag; runs the gate; stamps `web/package.json`; commits, tags, pushes.
- `--skip-gate` exists for cutting a release from a machine **without** the Go
  toolchain. Treat it as an emergency path: the gate is the release contract
  (`docs/versioning.md` rule 3), so use it only when CI has already proven the
  tag commit.
- Changelog: one section per `vX.Y.Z`, in release order; release notes come from
  the changelog section for the version.
- Every new `web` string lands in `en`, `de`, and `fr` in the same change.
- Interface changes in `server/store` cascade to the mocks in the same change
  (`CONTRIBUTING.md`).

*Last updated: 2026-10-26, at v2.1.0.*
