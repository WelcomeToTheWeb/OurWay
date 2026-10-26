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
| macOS remote input | Implement `synthesizeKey`/`synthesizeMouse` on darwin via CGEvent (`CGEventCreateKeyboardEvent`, `CGEventCreateMouseEvent`), including the Accessibility-permission prompt path and its status surface in the console. |
| Shared fleet store | One fleet read model shared by Dashboard, Devices, and DeviceDetail. Today every page polls independently; the finish review measured a shared store as roughly halving request pressure (`.impeccable/review/verdict.md`, debt #1). |
| Rate-limit headroom | With the shared store in place, re-measure the 100 req/min user budget (`server/api/ratelimit.go`, burst 10) and make the budget configurable, documented, and correct in `configuration.md`. |
| Alert count reconciliation | `GET /devices` carries alert state, so the dashboard's flagged count can disagree with the alert ledger's open count. One source of truth for "what needs me". |
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
| Client filter everywhere | Client column and filter on Devices, Alerts, Patches, Sessions, Files; per-client rollup on the dashboard. |
| Audit log | `audit_log` table: actor, action, target, timestamp, source IP, recorded on every mutating route — the Phase 1.2 item that shipped with zero code. Filterable in the console. |
| Console surfaces | Clients page; client header on device detail; audit ledger. |
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
| **Generic OIDC provider** (lead item) | Configure OurWay against **any** conformant IdP — Keycloak, Authentik, Zitadel, kanidm, Okta, Entra ID with a fixed tenant, JumpCloud, a home IdP. Today the flow is already generic OAuth2 (`server/sso/oauth.go` builds auth/token/userinfo calls from the provider row) and the create API already accepts `auth_url`/`token_url`/`user_info_url`/`scope` overrides, but `applyProviderDefaults` hardcodes the big-3 endpoints, the UI dropdown is hardcoded to `google`/`microsoft`/`apple` (`web/src/pages/SSO.tsx:14-16`), and nothing discovers an IdP's endpoints. Add: **issuer + OIDC discovery** (`.well-known/openid-configuration`), **`id_token` verification** (JWKS, `iss`/`aud`/`exp` — identity today comes from the userinfo call alone and the raw `id_token` is stored in `users.sso_attributes`), **claim mapping**, **role mapping**, **domain allow-list**, multiple concurrent providers, and a **test-connection** probe. |
| Per-device / per-group thresholds | Threshold overrides on device, client, and tag; the engine resolves device → tag → client → global. Alert-rule configuration UI in the console (roadmap 2.0 Phase 1 open item, `docs/phase1-tasks.md:50-56`). |
| Notification channels | Email (SMTP), Slack, and Microsoft Teams on top of `server/events` — the webhook machinery (HMAC signatures, retries, delivery tracking) already exists and is reused, not duplicated. Per-client routing: "client A's critical alerts go to my phone". |
| SAML 2.0 SSO | The one feature declared and guarded as missing (`buglist.md` L8; `server/api/sso.go` rejects it at create, authorize, and callback, with tests). Ordered **after** generic OIDC: discovery-based OIDC covers Keycloak/ Authentik/ Zitadel/ Okta in a day of work, SAML needs SP metadata, XML signing, and an assertion consumer — a larger surface for the same class of customer. |
| Session recording | Frame-sequence recording with playback, written into the audit log, so "who was controlling that machine at 14:00" is answerable. (Roadmap 2.0 §2.1.) |

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
land in pending, not linked); SAML round-trip against a real IdP fixture;
recording/playback test on a recorded frame sequence.

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
| 2.4.0 | A critical alert on a client reaches a human outside the console within one engine pass. OurWay logs in against a self-hosted Keycloak via issuer + discovery, with `iss`/`aud` verified and no identity accepted outside the domain allow-list. SAML works against a real IdP. |
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
