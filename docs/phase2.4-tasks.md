# Phase 2.4: Escalate to Humans, Own Your Identity — Task Breakdown

**Status:** Planned
**Target release:** `v2.4.0`
**Parent plan:** [`roadmap-2.x.md`](roadmap-2.x.md)
**Baseline:** `v2.3.x` (tenancy + audit log shipped)

Two themes, deliberately kept apart:

1. **Escalate** — the event bus (`server/events`) and the webhook machinery (HMAC
   signatures, delivery rows, retry loop, delivery tracking) already exist. Nothing
   on it reaches a *person*, and alert thresholds are one global set for the whole
   installation (`server/alerts/engine.go`).
2. **Own your identity** — the flow in `server/sso/oauth.go` is already generic
   OAuth2, and the create endpoint already accepts `auth_url`/`token_url`/
   `user_info_url`/`scope`. What is missing is discovery, `id_token` verification,
   claim/role mapping, and a UI that offers anything beyond the big three. SAML
   follows, ordered after OIDC deliberately: discovery-based OIDC covers
   Keycloak/Authentik/Zitadel/Okta in a day of work; SAML needs SP metadata, XML
   signing and an assertion consumer for the same class of customer.

**Migration numbering.** `server/store/migrate.go` is append-only with contiguous
versions; 2.3.0 ships 7 and 8, so this release uses **9 (oidc fields)**,
**10 (alert threshold scopes)**, **11 (notification channels)**,
**12 (session recordings)**.

---

## 2.4-0 · Fixtures this release needs before it needs code

**Do**
- [ ] A test IdP in CI: Keycloak (OIDC **and** SAML from the same fixture), seeded
      with users covering every claim shape the mapping must handle — verified and
      unverified email, domain inside and outside the allow-list, a group claim that
      maps to admin, and a user with no email claim.
- [ ] Mock SMTP (accepting, rejecting, and TLS-failing), Slack, and Teams webhook
      endpoints for channel tests.
- [ ] JWKS fixtures: key rotation (kid changes), a token signed by the wrong key,
      a token with `aud` = a different client, an expired token.
- [ ] A macOS/Linux agent fixture emitting metric sequences that cross thresholds
      for the precedence tests.

**Acceptance criteria**
- The IdP fixture runs in CI (`tests` job) with no network dependency beyond the
  image, and the suite proves a **real authorization-code round trip**, not a stub.
- Every 2.4 acceptance test below runs against these fixtures, not against a mock of
  the OIDC/SAML layer itself.

---

## 2.4-1 · Generic OIDC provider: issuer + discovery

**Context:** `applyProviderDefaults` (`server/api/sso.go:254-280`) hardcodes the
big-3 endpoints; `web/src/pages/SSO.tsx:14-16` hardcodes the dropdown to
`google`/`microsoft`/`apple`.

**Do**
- [ ] Migration 9: `sso_providers.issuer`, `.discovery_url`, `.jwks_uri`,
      `.claims_email`, `.claims_username`, `.claims_name`, `.claims_groups`,
      `.allowed_domains`, `.require_verified_email`, `.link_policy`, `.redirect_uri`.
- [ ] `OIDCHandler` beside the OAuth2 handler: input **an issuer URL**, fetch
      `.well-known/openid-configuration`, store the discovered
      `authorization_endpoint` / `token_endpoint` / `userinfo_endpoint` /
      `jwks_uri` / `id_token_signing_alg_values_supported`, and show the operator
      exactly what was discovered before they save it.
- [ ] Preserve the big three as **presets of the generic provider** — one code path,
      one UI. No separate "custom" branch.
- [ ] Multiple concurrent enabled providers, ordered, with the login page listing
      them (the page already maps over `GET /api/auth/sso/providers` and
      `ProviderIcon`/`providerDisplayName` already fall back for unknown names).
- [ ] `Test-connection`: discovery probe + a signed-redirect dry run, reporting
      redirect-URI mismatch, discovery failure, and JWKS failure as named console
      states, not a 500.
- [ ] `client_secret` stays `json:"-"` on the provider row as today; provider
      endpoints remain `RequireRole("admin")`.

**Acceptance criteria**
- Adding Keycloak with **only an issuer + client id/secret** yields a working login
  round trip in the fixture suite.
- A provider whose issuer returns no `.well-known` document is refused at save time,
  with a named error, and no half-saved provider row (fails loudly,
  `CONTRIBUTING.md`).
- `GET /api/sso/providers` (admin) never serializes `client_secret`; the public
  provider list exposes id/name/label only.
- Two providers enabled at once both complete a login; `sso_code` exchange (C6 fix)
  is unchanged.

---

## 2.4-2 · Verify the `id_token` before trusting an identity

**Context:** identity today comes from the userinfo call alone, and the raw
`id_token` is stuffed into `users.sso_attributes`
(`server/sso/oauth.go`, `fmt.Sprintf("{\"id_token\":\"%s\"}", idToken)`).

**Do**
- [ ] Verify `iss`, `aud` (this provider's client id), `exp`/`iat` with leeway, and
      the signature against the discovered JWKS (`kid` lookup, rotation-tolerant),
      with a cached JWKS fetch and a refresh on unknown `kid`.
- [ ] When `userinfo` and a verified `id_token` both exist, **require them to agree**
      on `sub`; disagreement is a refused login, audited.
- [ ] Stop storing the raw `id_token`. Persist only the verified claim summary the
      product needs (`provider`, `provider_id`, mapped groups), in
      `users.sso_attributes` as structured JSON, not string-concatenated.
- [ ] PKCE on the code flow, and `state`/`nonce` bound to the server-side session
      (the C6/M13 remediation already produced a one-time `sso_code` and a
      state cookie — keep that contract, don't re-open it).

**Acceptance criteria**
- Fixture suite proves: valid token accepted; wrong-key token, foreign `aud`,
  `iss` mismatch, and expired token each refused with a distinct audited outcome.
- JWKS rotation mid-session keeps logins working (kid changes between issuances).
- `users.sso_attributes` contains no raw `id_token` (asserted in a store test).
- userinfo/`id_token` `sub` disagreement → refused, and the row is audited.

---

## 2.4-3 · The identity-linking security contract

**Why this is its own task.** `findOrCreateUser` falls back to
`Users.FindByEmail(info.Email)` and stamps `Provider`/`ProviderID` onto that
existing local account (`server/sso/oauth.go:189-210`). A permissive IdP that lets
anyone assert `ops-admin@corp.example` therefore claims the local, password-
authenticated admin account. `UserInfo.EmailVerified` is parsed and never checked.
Generic OIDC widens exactly this surface, so it ships with the contract, not after.

**Do**
- [ ] **Domain allow-list** enforced on the provider: an asserted email outside the
      list is refused. `link_policy` is per provider.
- [ ] **Verified email required** when `require_verified_email` is set — honor
      `email_verified` / the mapped email-claim, per IdP claim shape.
- [ ] **No silent linking of existing accounts.** New provider/email pairs create a
      user in a **pending** state with no role (the `viewer` floor from
      `HandleCallback` stays the floor); linking to an existing account requires an
      admin to confirm the match in Users, which is audited (2.3-6).
- [ ] **Role mapping is opt-in**: if configured, the mapped claim decides roles;
      if not configured, nothing above `viewer` is granted. Mapping is
      admin-declared against a claim the operator controls, never a self-editable
      profile claim.
- [ ] The JIT path for OIDC and SAML is **one** path, sharing this contract.
- [ ] Users page: pending SSO identities need an explicit approve/merge action,
      showing provider, asserted email, and the account they would link to.

**Acceptance criteria**
- **The headline test:** with a permissive fixture IdP, asserting the email of an
  existing **admin** local account produces a **pending** identity and no access —
  the admin's roles are unchanged, the login is refused, and the attempt is audited.
  This test is the release's gate, and it must be visibly green in CI.
- An email outside the allow-list is refused at callback.
- Unverified email with `require_verified_email` set is refused.
- With no role mapping configured, a JIT user is `viewer` and cannot escalate; with
  mapping configured to `admin`, the login yields admin.
- Local password auth for existing users is unaffected (the whole existing
  `server/api/auth` suite passes unchanged).

---

## 2.4-4 · Console: provider setup and login list

**Do**
- [ ] Replace the hardcoded provider dropdown with the provider ledger: presets as
      a starting point, issuer + client id/secret + discovery preview, claim
      mapping, domain allow-list, verified-email toggle, link policy, enabled order.
- [ ] Login page renders providers in configured order; a provider the operator
      disabled disappears without a console rebuild.
- [ ] All strings in `en`/`de`/`fr` lockstep, with `scripts/i18n-parity.sh`
      (2.2-5) green.

**Acceptance criteria**
- An operator can add Keycloak, Authentik, and Zitadel from the UI alone, with no
  server restart, and each appears on the login page in the configured order.
- `Test-connection` output distinguishes discovery failure, redirect mismatch, and
  JWKS failure with named states.
- Secrets never round-trip to the browser after creation.

---

## 2.4-5 · Thresholds with precedence, and the rules UI

**Context:** `server/alerts/engine.go` keeps one `map[string]Thresholds`
(`cpu` 80/90, `ram` 85/95, `disk` 85/95) and `SetThresholds` replaces a metric
wholesale. `docs/phase1-tasks.md:50-56` declared per-device thresholds and an alert
rule UI; neither exists.

**Do**
- [ ] Migration 10: `alert_thresholds` rows — `(scope_type: device|tag|client|global,
      scope_value, metric, warning, critical, dedup_window, enabled)`.
- [ ] Engine resolution **device → tag → client → global**, most specific wins per
      metric; a device with no applicable row behaves exactly as today.
- [ ] Auto-resolution stays consistent with the resolved thresholds (the engine
      already auto-resolves on the warning threshold — it must use the *resolved*
      one, not the global one).
- [ ] `GET/POST/PUT/DELETE /api/alerts/thresholds`, admin/manager only, scoped by
      client for managers (2.3.1 rules).
- [ ] Console: threshold rules ledger with scope, metric, warning/critical,
      precedence preview for a chosen device ("this unit resolves to …"), and a
      simulation input (paste a metric value, see which rule fires).
- [ ] All resolved-threshold data crosses the client scope boundary as 2.3.0 defines
      it — a manager cannot define a threshold for a client they cannot see.

**Acceptance criteria**
- Precedence test: global 80/90, client 70/85, tag 60/75, device 50/65 — a metric
  sequence produces the alert, severity, `Alert.Threshold` value and message of the
  **most specific** rule, per metric independently (cpu from tag, ram from device).
- Auto-resolution uses the resolved warning threshold — a device whose resolved
  warning is 50 auto-resolves at 48 where the global 80 would leave it open.
- A device with no override produces byte-identical alerts to `v2.3.x` (a
  regression test on the existing engine suite).
- Dedup window honored per scope.
- The simulation input agrees with the engine on 20 fixture sequences.

---

## 2.4-6 · Channels that reach a person

**Design constraint:** reuse the webhook machinery, don't duplicate it. Generalize
`webhooks` with a `kind` column (`webhook` | `email` | `slack` | `teams`) and a
`config` JSON column, so the existing `Dispatcher` (HMAC sign, delivery rows,
`RetryPending`, `SendTestEvent`) delivers all of them, and the delivery ledger stays
one place an operator can read.

**Do**
- [ ] Migration 11: `webhooks.kind`, `webhooks.config`, `webhooks.scope_clients`,
      `webhooks.min_severity`, `webhooks.tags`. Note `webhooks.url` is `not null`
      today and is meaningless for an email channel — migration 11 drops the
      NOT-NULL on `url` and the API validates it **per kind** (required for
      webhook/slack/teams, absent for email). Event matching stays exact in Go
      (`store/webhooks.go:ListByEvent`, the M12 fix) — do not reintroduce a SQL
      match on the serialized event array when routing expands.
- [ ] Senders: SMTP (STARTTLS, auth, `MAIL FROM`/`To` from the channel config,
      templates for `alert_created` / `alert_resolved` / `patch_deployed` in
      `en`/`de`/`fr`), Slack, and Microsoft Teams (message-card payload).
- [ ] Routing: channel × client scope × minimum severity × tags. "Client A's
      criticals to the on-call mailbox, client B's to Slack". Per-channel event
      types, same as today's webhook event selection.
- [ ] `SMTP_HOST`/`SMTP_PORT`/`SMTP_USERNAME`/`SMTP_PASSWORD`/`SMTP_FROM`/
      `SMTP_STARTTLS` documented in `docs/configuration.md` **and** `.env.example`
      in the same change (the standard set by the 2.2 docs work: an env var that
      exists in code and nowhere in docs is a bug).
- [ ] Console: channel ledger with per-channel routing, delivery history, and
      "send test" (the `SendTestEvent` pattern, per kind).
- [ ] Every send outcome lands in the delivery ledger with the attempt count and
      the peer's response.

**Acceptance criteria**
- `alert_created` on a scoped critical alert reaches mock SMTP, Slack, and Teams
  within one engine pass, with the unit, client, metric, value, and threshold
  present in the payload/text.
- Severity below `min_severity`, and events from an unlisted client, produce **no**
  delivery row (not a failed row: no row at all).
- Retry: a rejecting mock endpoint produces the same backoff/retry record shape as a
  webhook does today; a channel that succeeds on attempt 2 is marked delivered with
  `attempt: 2`.
- Test-send produces a delivery row an operator can point at.
- Email templates exist in all three locales; a missing locale string fails
  `scripts/i18n-parity.sh`.

---

## 2.4-7 · SAML 2.0 (after OIDC, on purpose)

**Do**
- [ ] Replace the three rejection points (`server/api/sso.go` at create, authorize,
      and callback; `server/api/sso_test.go:25-26` asserts them) with an
      implementation — **and update those tests in the same change**, since they
      track the shipped contract (`CONTRIBUTING.md`).
- [ ] SP metadata endpoint per provider (`/api/auth/sso/saml/:id/metadata`),
      assertion consumer at `/api/auth/sso/saml/:id/acs`, SP signing key material
      configured per provider (cert + key, `json:"-"`).
- [ ] Validate: signature on the assertion (and optionally the response),
      `Audience`/`AudienceRestriction`, `Recipient`, `Destination`, `NotOnOrAfter`
      /`SessionNotOnOrAfter`, `SubjectConfirmationData` `Recipient` +
      `NotOnOrAfter`, and **replay protection** on assertion `ID` (a seen-ID store
      with a TTL).
- [ ] Identity comes through the **2.4-3 contract**, unchanged: domain allow-list,
      verified-email equivalent, pending-identity default, opt-in role mapping from
      the group attribute.
- [ ] Console: the provider row exposes SP metadata for download and the IdP
      entity ID / SSO location / certificate fields.

**Acceptance criteria**
- Full round trip against the Keycloak fixture: metadata download → IdP-configured
  → SP-initiated login → assertion accepted → session issued through the existing
  `sso_code` exchange (no token in a URL, per the C6 fix).
- Negative fixtures each refused with a distinct audited outcome: tampered
  assertion, wrong audience, expired `NotOnOrAfter`, wrong recipient, a replayed
  assertion, and an unsigned assertion.
- The identity-linking tests from 2.4-3 pass unchanged for SAML — one contract, two
  protocols.
- `server/api/sso_test.go` passes with the rejection tests replaced by acceptance
  tests for SAML and kept as rejections for any *other* unsupported type.

---

## 2.4-8 · Session recording, so the audit log has an answer

**Context:** `server/models/session.go` records tokens and status, no media;
`POST /api/sessions/:id/frame` relays frames to the owner and stores nothing.
`server/files/service.go` is the existing shape for server-side blob storage
(directory, per-transfer file, cleanup sweep) — recordings follow it.

**Do**
- [ ] Migration 12: `session_recordings` — `session_id`, `device_id`, `user_id`,
      `client_id`, `started_at`, `ended_at`, `frame_count`, `bytes`, `path`,
      `retention_until`, `reason`.
- [ ] Recording written as an append-only JPEG sequence file under
      `RECORDINGS_DIR`, with the retention sweep scheduled (the M11 lesson again:
      an unpruned delete method is not a policy) and `RECORDING_RETENTION_DAYS`.
- [ ] Policy: `RECORD_SESSIONS=off|on|control-only` (default **off**), per-session
      toggle surfaced to the technician, and the console states plainly when a
      session **is** being recorded — this is an operator-visible promise, not a
      background behaviour.
- [ ] Playback in the console: scrub, frame stepping, keyboard/mouse events shown
      over the frames from the audit rows, and a download of the recording manifest.
- [ ] Access control: recordings are client-scoped per 2.3.1 (a technician scoped
      to A cannot read a client-B recording), and **playing a recording is audited**
      like any other action.
- [ ] Recording survives a server restart mid-session (the file is append-only; the
      row records the gap, it does not pretend continuity).

**Acceptance criteria**
- A 3-minute control session with `RECORD_SESSIONS=control-only` produces a
  recording whose frame count and byte size are recorded, and playback shows the
  technician's input events at the frames they were sent.
- A scoped technician gets 404 on a client-B recording by ID, matching 2.3.1.
- Playing a recording writes an audit row naming the player and the recording.
- Retention sweep removes `retention_until` rows and their files; the sweep is
  observable in a counter (feeds 2.5.0's metrics surface).
- `RECORD_SESSIONS=off` (default) produces zero recording rows and zero files.

---

## Release gate for `v2.4.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/i18n-parity.sh
scripts/release.sh --check 2.4.0
```

Plus, non-negotiable for this release:

- [ ] The 2.4-3 headline test — asserting an existing admin's email via a permissive
      IdP lands in **pending**, grants nothing, and is audited — green in CI.
- [ ] OIDC accepted against Keycloak by issuer alone; SAML round trip green; the
      negative fixtures in 2.4-2 and 2.4-7 each refused.
- [ ] Threshold precedence suite green, including the `v2.3.x` byte-identical
      regression case for devices with no override.
- [ ] A scoped critical alert reaches mock SMTP + Slack + Teams in one engine pass,
      and out-of-scope/low-severity events create **no** delivery rows.
- [ ] Recordings are client-scoped, playback is audited, and the default is off.
- [ ] `docs/api.md` updated for every new route with the 2.2-7 route↔docs check
      green; new env vars documented in `docs/configuration.md` and `.env.example`.

**Wire contract:** new routes, new event types, new WS payload fields for the
recording indicator → **MINOR**. Agents are unaffected. Provider row fields are
additive; `client_secret` stays server-only.

---

## Deferred inside 2.4 (so the release stays shippable)

- SCIM provisioning and group sync beyond claim-based mapping.
- LDAP/AD auth, TOTP/2FA for local accounts, device-bound passkeys.
- PagerDuty / Telegram / SMS channels and on-call schedules (the channel abstraction
  from 2.4-6 is what makes these cheap later).
- Per-user notification preferences and digests (per-channel routing ships;
  "my inbox, my rules" is a 2.5+ conversation).
- SAML artifact binding, IdP-initiated SLO, and encrypted assertions.
- Recording compression/transcoding (JPEG sequence + manifest is the format; video
  export is not).
