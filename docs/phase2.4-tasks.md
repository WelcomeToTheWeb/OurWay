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
versions. 2.3.0 shipped **8** (`clients_and_user_client_scope`), **9**
(`audit_log`), **10** (`tenant_scoped_read_indexes`) and **11**
(`client_enrollment_tokens`) — the plan's "2.3.0 ships 7 and 8" was written before the
scoped read indexes and the enrollment tokens landed inside 2.3. So this release
starts at **12**: **12 (oidc provider discovery + identity policy)**,
**13 (SSO pending identities)**, **14 (alert threshold scopes)**,
**15 (notification channels)**, **16 (session recordings)**. Migrations 12
(`oidc_provider_discovery_and_identity_policy`) and 13 (`sso_pending_identities`)
are in `migrate.go` — 13 was consumed by 2.4-3's pending-identity columns, which the
plan listed with the identity tasks, not the threshold task.

---

## 2.4-0 · Fixtures this release needs before it needs code

**Do**
- [~] A test IdP in CI: Keycloak (OIDC **and** SAML from the same fixture), seeded
      with users covering every claim shape the mapping must handle — verified and
      unverified email, domain inside and outside the allow-list, a group claim that
      maps to admin, and a user with no email claim.
      **2.4-1 shipped `server/sso/testidp` instead** for the OIDC half: an OpenID
      Provider in Go — publishes `.well-known/openid-configuration`, serves a JWKS
      from a key it generated, mints RS256 `id_token`s with `iss`/`aud`/`exp`/`nonce`,
      runs the code exchange (single-use codes, redirect-URI enforcement,
      `invalid_grant`), rotates its key, and sabotages itself on command. A stub of
      the verifier agrees with whatever the verifier does; the tests that matter need
      a provider that can disagree. Keycloak-as-a-container is still owed for the
      **SAML** half (2.4-7): a Go assertion signer would be a mock of the layer under
      test, which is exactly what this section forbids.
- [ ] Mock SMTP (accepting, rejecting, and TLS-failing), Slack, and Teams webhook
      endpoints for channel tests.
- [x] JWKS fixtures: key rotation (kid changes), a token signed by the wrong key,
      a token with `aud` = a different client, an expired token. — `testidp.Rotate`,
      `PublishOnlyCurrentKey`, `MintIDTokenWithKey`, `MintIDToken(claims, clientID, alg)`
      and `MintExpiredIDToken`, exercised in `server/sso/oidc_test.go`.
- [ ] A macOS/Linux agent fixture emitting metric sequences that cross thresholds
      for the precedence tests.

**Acceptance criteria**
- The IdP fixture runs in CI (`tests` job) with no network dependency beyond the
  image, and the suite proves a **real authorization-code round trip**, not a stub.
  **Met for OIDC** (2.4-1): `TestSSOLoginRoundTripThroughADiscoveredProvider` walks
  authorize → IdP → callback → `sso_code` → exchange against the fixture, and
  `TestSSOTwoEnabledProvidersBothCompleteLogins` walks it twice, through two providers.
- Every 2.4 acceptance test below runs against these fixtures, not against a mock of
  the OIDC/SAML layer itself.

---

## 2.4-1 · Generic OIDC provider: issuer + discovery · ✅ delivered 2026-10-29

**Delivered (2.4-1)** — `server/sso/discovery.go`, `server/sso/jwks.go`,
`server/sso/testidp/`, `server/api/sso.go`, migration 12. Tests:
`server/sso/oidc_test.go` (discovery kinds, verification refusals, rotation) and
`server/api/sso_oidc_test.go` (issuer-only save, no-row-on-failure, public list,
test-connection states, the discovery preview, the login round trip, two providers
at once). Measured: `go test ./... -race -count=1` green across all server packages
on `ourway/2.4-1-oidc-discovery`.

**Context:** `applyProviderDefaults` (`server/api/sso.go:254-280`) hardcodes the
big-3 endpoints; `web/src/pages/SSO.tsx:14-16` hardcodes the dropdown to
`google`/`microsoft`/`apple`.

**Do**
- [x] Migration 12 (numbering corrected above): `sso_providers.issuer`,
      `.discovery_url`, `.jwks_uri`, `.redirect_uri`, `.discovery_doc`,
      `.discovered_at`, `.claims_email`, `.claims_username`, `.claims_name`,
      `.claims_groups`, `.allowed_domains`, `.require_verified_email`,
      `.link_policy`, `.role_mapping`, `.sort_order`. The migration lands
      pre-existing rows on `link_policy='pending'`, so an install stops being able
      to have a local account claimed by asserting its email the moment 2.4 is
      applied.
- [x] Input **an issuer URL**, fetch `.well-known/openid-configuration`, store the
      discovered `authorization_endpoint` / `token_endpoint` /
      `userinfo_endpoint` / `jwks_uri` / `id_token_signing_alg_values_supported`,
      and show the operator exactly what was discovered before they save it —
      **`POST /api/sso/providers/discover`** (admin, audited `sso.discover`): the
      endpoints, the signing algorithms, the scopes, the PKCE methods, the document
      verbatim, the redirect URI this installation will send, and the named checks.
      It writes nothing; the row is written by the save.
      Precedence at save: **typed > discovered > preset**.
- [x] Preserve the big three as **presets of the generic provider** — one code path,
      one UI. No separate "custom" branch. `providerPreset` supplies an issuer and a
      scope and nothing else; Google/Microsoft/Apple endpoints are now discovered,
      so a provider that moves an endpoint stops being a OurWay release.
- [x] Multiple concurrent enabled providers, ordered (`sort_order, name`), with the
      login page listing them. Proven by
      `TestSSOTwoEnabledProvidersBothCompleteLogins`: two IdPs, two round trips, two
      JIT accounts, and no collapse onto one account.
- [x] `Test-connection` (`POST /api/sso/providers/:id/test`, admin, audited
      `sso.test`): discovery, the redirect URI the flow will send, the JWKS, the
      `email` scope, PKCE, and the id_token signing algorithm, each as a named
      state (`pass`/`warn`/`fail`/`skipped`) — never a 500.
- [x] `client_secret` stays `json:"-"` on the provider row as today; provider
      endpoints remain `RequireRole("admin")`.

**Acceptance criteria**
- Adding Keycloak with **only an issuer + client id/secret** yields a working login
  round trip in the fixture suite. ✅ `TestSSOProviderSavedFromIssuerAlone` +
  `TestSSOLoginRoundTripThroughADiscoveredProvider`.
- A provider whose issuer returns no `.well-known` document is refused at save time,
  with a named error, and no half-saved provider row (fails loudly,
  `CONTRIBUTING.md`). ✅ `TestSSODiscoveryFailureWritesNoProviderRow`.
- `GET /api/sso/providers` (admin) never serializes `client_secret`; the public
  provider list exposes id/name/label only. ✅
  `TestSSOPublicProviderListCarriesNoSecretAndNoEndpoints` (canary secrets, no
  endpoints, order).
- Two providers enabled at once both complete a login; `sso_code` exchange (C6 fix)
  is unchanged. ✅ `TestSSOTwoEnabledProvidersBothCompleteLogins`; the replay
  refusal is asserted in `TestSSOLoginRoundTripThroughADiscoveredProvider`.

**Not in 2.4-1** (the console half is 2.4-4, and the verification is 2.4-2): the
provider ledger UI — `web/src/pages/SSO.tsx` still offers the hardcoded big-3
dropdown, so an operator can add Keycloak by API today and not by UI.

---

## 2.4-2 · Verify the `id_token` before trusting an identity · ✅ delivered 2026-10-29

**Delivered (2.4-2)** — `server/sso/oauth.go` (the callback's verification path,
PKCE, claim mapping), `server/sso/jwks.go` (the verifier, landed with 2.4-1),
`server/sso/testidp` (PKCE enforcement, nonce capture, token sabotages), migration 12
(+ `pkce_supported`). Tests: `server/api/sso_verify_test.go`,
`server/store/sso_attributes_test.go`, plus the rotation and round-trip cases in
`server/sso/oidc_test.go` and `server/api/sso_oidc_test.go`.

**Context:** identity today comes from the userinfo call alone, and the raw
`id_token` is stuffed into `users.sso_attributes`
(`server/sso/oauth.go`, `fmt.Sprintf("{\"id_token\":\"%s\"}", idToken)`).

**Do**
- [x] Verify `iss`, `aud` (this provider's client id), `exp`/`iat` with leeway, and
      the signature against the discovered JWKS (`kid` lookup, rotation-tolerant),
      with a cached JWKS fetch and a refresh on unknown `kid`. RS256 only, checked on
      the JWS header before a key is consulted.
- [x] When `userinfo` and a verified `id_token` both exist, **require them to agree**
      on `sub`; disagreement is a refused login, audited (`subject_mismatch`).
- [x] Stop storing the raw `id_token`. `users.sso_attributes` is the claim summary —
      `provider`, `provider_id`, `name`, `email`, `email_verified`, mapped `groups` —
      as JSON. Pinned at the store boundary by
      `TestUserSSOAttributesStoreTheClaimSummaryNotTheToken` (field set closed, no
      token material).
- [x] PKCE on the code flow (`S256`, sent when the provider published a challenge
      method at save time), and `state`/`nonce` bound to a server-side single-use
      record. The C6/M13 contract — one-time `sso_code`, consumed state cookie —
      is unchanged.

**Acceptance criteria**
- Fixture suite proves: valid token accepted; wrong-key token, foreign `aud`,
  `iss` mismatch, and expired token each refused with a distinct audited outcome.
  ✅ `TestSSOCallbackRefusesEveryUnverifiableToken` (also `alg: none`, HS256 against
  the RSA set, no `id_token` at all, and a token minted for an earlier login). Each
  refusal is a 401 with a named `reason` and an `auth.login_failed` row naming the
  reason **and the asserted email**, and provisions no account.
- JWKS rotation mid-session keeps logins working (kid changes between issuances). ✅
  `TestSSOKeyRotationMidSessionKeepsLoginsWorking`.
- `users.sso_attributes` contains no raw `id_token` (asserted in a store test). ✅
- userinfo/`id_token` `sub` disagreement → refused, and the row is audited. ✅
  `TestSSORequiresUserinfoAndIDTokenToAgreeOnSubject`.

**What the tests caught in code.**

1. `auditOKOnce` / `auditRefusedOnce` called `markAudited(c)` **before** writing, and
   `recordAudit`'s duplicate-row guard then swallowed the row — so those two helpers
   wrote nothing. 2.4-2's login refusals are their first callers, and the ledger's
   silence is what found it. The helpers now let `recordAudit` do the write and the
   marking, which is the single-row rule as designed.
2. With 2.4-1's discovery precedence in place, a provider saved with **no issuer**
   kept the endpoints the operator typed only by accident: `applyDiscovery` returned
   before the typed endpoints were applied, so such a provider authorized to an empty
   URL. The precedence is now applied in one place, on both paths.
3. A login's identity facts are only trustworthy after verification, so userinfo is
   read **first** and trusted **after**: the order changes nothing about what is
   trusted, and it is what lets a refusal name who tried to arrive as whom.

**Stated, not hidden:** `id_token_verified` is recorded on every SSO login row. A
provider configured by hand-typed endpoints has no key set, and its logins say so.

---

## 2.4-3 · The identity-linking security contract

**Why this is its own task.** `findOrCreateUser` falls back to
`Users.FindByEmail(info.Email)` and stamps `Provider`/`ProviderID` onto that
existing local account (`server/sso/oauth.go:189-210`). A permissive IdP that lets
anyone assert `ops-admin@corp.example` therefore claims the local, password-
authenticated admin account. `UserInfo.EmailVerified` is parsed and never checked.
Generic OIDC widens exactly this surface, so it ships with the contract, not after.

**Do**
- [x] **Domain allow-list** enforced on the provider: an asserted email outside the
      list is refused (`domain_not_allowed`, nothing provisioned). `link_policy` is
      per provider. — `server/sso/contract.go:domainAllowed`, applied by
      `contractGate` in `server/sso/oauth.go`.
- [x] **Verified email required** when `require_verified_email` is set — the
      `email_verified` claim is honored, not parsed and ignored
      (`email_unverified`).
- [x] **No silent linking of existing accounts.** The `Users.FindByEmail` fallback in
      `findOrCreateUser` is gone. A provider asserting an existing account's email
      creates a **pending** identity — no roles, no login, an audit row — and an admin
      decides: `POST /api/users/:id/approve` (its own account, `viewer` floor) or
      `POST /api/users/:id/merge` (attached to the account its email belongs to,
      roles untouched), audited `user.approve` / `user.merge`. Migration 13
      (`sso_pending_identities`): `users.status` (`active`/`pending`),
      `users.pending_provider`, `users.pending_link_user_id`.
- [x] **Role mapping is opt-in**: `role_mapping` empty ⇒ every SSO identity lands at
      the `viewer` floor; configured ⇒ the mapping **owns** the roles it names
      (granted while the group claim asserts them, removed when it stops), and never
      touches roles outside itself. Admin-declared on the provider row, never a
      self-editable profile claim.
- [x] The JIT path for OIDC and SAML is **one** path, sharing this contract —
      `resolveAccount` in `server/sso/oauth.go` is the only place an SSO identity
      becomes an account, and it is contract-gated. 2.4-7 asserts the SAML half of
      this criterion.
- [x] Users page: the pending band (admin only) states provider, subject, asserted
      email, the verified-by-IdP fact, and the target account with its roles on one
      row; Approve and Merge confirm in the sheet (`confirm.approveIdentity.*`,
      `confirm.mergeIdentity.*`, `en`/`de`/`fr` lockstep).

**Acceptance criteria**
- [x] **The headline test:** with a permissive fixture IdP, asserting the email of an
  existing **admin** local account produces a **pending** identity and no access —
  the admin's roles are unchanged, the login is refused, and the attempt is audited.
  This test is the release's gate, and it must be visibly green in CI.
  — `TestSSOCannotClaimAnExistingAdminAccount` (`server/api/sso_contract_test.go`):
  refuses with 403 `identity_pending`, the admin keeps `admin` alone and its password,
  the pending row has no roles and names the target, the `auth.login_failed` row names
  reason + asserted email + target account, and a repeat assertion is
  `account_pending` on the same record.
- [x] An email outside the allow-list is refused at callback —
      `TestSSODomainAllowListRefusesAnOutsideEmail` (inside the list, both domains,
      still logs in).
- [x] Unverified email with `require_verified_email` set is refused —
      `TestSSOUnverifiedEmailIsRefusedWhenRequired`.
- [x] With no role mapping configured, a JIT user is `viewer` and cannot escalate;
      with mapping configured to `admin`, the login yields admin —
      `TestSSOJITFloorIsViewerAndCannotEscalate`,
      `TestSSORoleMappingIsOptInAndOwnsRoles` (including the role taken back when the
      group claim stops asserting it).
- [x] Local password auth for existing users is unaffected (the whole existing
      `server/api/auth` suite passes unchanged) — and stated directly by
      `TestContractLeavesLocalPasswordAuthWorking`,
      `TestContractLeavesManagerScopeAlone`,
      `TestContractLeavesASubjectSSOLoginWorking`
      (`server/api/sso_contract_scope_test.go`).

**Shipped with 2.4-3**
- Refusals split by which half refused: **401** = identity not proven (2.4-2),
  **403** = proven and refused by the contract. `HandleLogin` refuses a pending
  account by name rather than issuing a session for an account with no roles.
- `server/sso/contract.go` — the contract as one module, so 2.4-7's SAML callback
  reaches the same gate.
- The Users page now reports refusals at **page level**: `formError` rendered only
  inside the modals, so a rejected act looked like nothing had happened.
- **Found on the way:** `Users.Update` is a full `Save`, and `GetByID` preloads the
  role associations — saving a row fetched that way re-inserts the `user_roles` join
  rows (`[technician technician]`). Merge and approve clear the preloaded
  associations; the role change is made through `UserRoles` alone.

---

## 2.4-4 · Console: provider setup and login list

**Do**
- [ ] Replace the hardcoded provider dropdown with the provider ledger: presets as
      a starting point, issuer + client id/secret + discovery preview, claim
      mapping, domain allow-list, verified-email toggle, link policy, enabled order.
- [ ] Login page renders providers in configured order; a provider the operator
      disabled disappears without a console rebuild.
- [ ] All strings in `en`/`de`/`fr` lockstep, with `scripts/i18n-parity.py`
      (2.2-5) green.

**Acceptance criteria**
- An operator can add Keycloak, Authentik, and Zitadel from the UI alone, with no
  server restart, and each appears on the login page in the configured order.
- `Test-connection` output distinguishes discovery failure, redirect mismatch, and
  JWKS failure with named states.
- Secrets never round-trip to the browser after creation.

---

## 2.4-5 · Thresholds with precedence, and the rules UI · ✅ delivered 2026-10-29

**Context:** `server/alerts/engine.go` kept one `map[string]Thresholds`
(`cpu` 80/90, `ram` 85/95, `disk` 85/95) and `SetThresholds` replaced a metric
wholesale. `docs/phase1-tasks.md:50-56` declared per-device thresholds and an alert
rule UI; neither existed.

**Delivered (2.4-5)** — `server/models/alert_threshold.go`,
`server/store/thresholds.go`, `server/alerts/engine.go`, `server/api/thresholds.go`,
migration 14 (`alert_threshold_scopes`), console `web/src/pages/Thresholds.tsx`.

**Do**
- [x] Migration **14** (not 13: 13 was consumed by 2.4-3's pending identities):
      `alert_thresholds` rows — `(scope_type: device|tag|client|global, scope_value,
      metric, warning, critical, dedup_window_seconds, enabled)`. **No rules are
      seeded**: the built-in engine numbers stay the fallback, so an installation
      upgrades and keeps alerting exactly as it did.
- [x] Engine resolution **device → tag → client → global**, most specific wins per
      metric, each metric decided independently. A device with no applicable row is
      measured against exactly the numbers `v2.3.x` gave (`TestDeviceWithNoRuleProducesThe
      ShippedAlertExactly`). The tie between two rules of the **same** scope is
      deterministic — more sensitive pair, then scope value — and it is one function,
      `store.RuleWins`, which the console's preview sorts with too: a chain the
      operator reads and the resolver contradicts is worse than no chain.
- [x] Auto-resolution compares against the **resolved** warning, not the global one
      (`TestAutoResolveUsesTheResolvedWarning`): a unit whose rule says 50 stays
      open at 52 — where the global 80 would have auto-resolved it — and resolves at 48.
- [x] `GET/POST/PUT/DELETE /api/alerts/thresholds`, admin/manager only, managers
      scoped to their tenants — a device/client rule for a tenant the caller cannot
      see answers the body an absent tenant gets, `tag`/`global` rules are admin-only
      (`TestManagerCannotDefineAThresholdForAClientTheyCannotSee`). Audited
      `alert.threshold_create` / `_update` / `_delete` naming scope, metric and the
      numbers (`TestThresholdWritesAreAuditedWithTheirNumbers`).
- [x] `GET /api/alerts/thresholds/resolve?device=` (per-metric chain, winner first,
      built-in floor last) and `GET /api/alerts/thresholds/simulate` (severity, the
      scope that answered, and the engine's own message, recording nothing). Both
      walk `ScopedDevice`, so an out-of-scope unit answers the body an absent unit
      gets, byte for byte.
- [x] Console: **Alert rules** page (`/thresholds`, admin/manager) — ledger with the
      name of what each rule points at, the floor stated in numbers, the per-unit
      precedence preview, the value simulation, enable toggles, deletes carried in the
      confirm sheet (`confirm.deleteThreshold.*`), `en`/`de`/`fr` in lockstep.

**Acceptance criteria**
- Precedence: global 80/90, client 70/85, tag 60/75, device 50/65 — a metric
  sequence produces the **most specific** rule's alert, severity, `Alert.Threshold`
  and message, per metric independently. ✅
  `TestThresholdPrecedenceUsesTheMostSpecificRulePerMetric`,
  `TestThresholdRuleIsWhatTheEngineMeasuresTheAgentAgainst` (the 62% metric is
  **posted by an agent** through `POST /api/agent/metrics` with its device key: the
  alert is the one a real unit produces, and it names `threshold: 50`).
- Auto-resolution on the resolved warning. ✅
  `TestAutoResolveUsesTheResolvedWarning` — 52 leaves the alert open where the global
  80 would have auto-resolved it; 48 resolves it.
- A device with no override produces byte-identical alerts to `v2.3.x`. ✅
  `TestDeviceWithNoRuleProducesTheShippedAlertExactly` pins threshold, severity and
  the message string for cpu/ram/disk at the shipped floor.
- Dedup window honored per scope. ✅
  `TestRuleDedupWindowOverridesTheEngineDefault` (engine default 5 min; a rule's
  120 s window refills inside it, and the rule's own window still dedups).
- The simulation agrees with the engine on 20 fixture sequences. ✅
  `TestThresholdSimulationAgreesWithTheEngineOnTwentyValues` — 20 values from 0 to
  91, each simulated **and** posted at a unit of its own; severity, threshold and
  the message string are compared.

**What the tests caught**

1. `Enabled bool` carried a GORM `default:true`, so a rule saved **disabled** was
   stored **enabled** — GORM substitutes the column default for a zero-valued field
   on insert. `TestDisabledRuleIsStoredDisabled` pins the row, and `Create` now
   writes every column (`Select("*")`).
2. The preview sorted by its own copy of the precedence rule and missed the
   same-scope tie-break the resolver applies. The preview now sorts with
   `store.RuleWins`, and the fixture's chain is asserted in order.
3. `TestViewerSessionRelay`'s binary-frame hops kept a hard 5 s deadline while the
   message polls had the file's documented scheduler-aware budget: under
   `go -C server test -race ./...` with this suite in the parallel load, a hop was
   late and the relay was correct. Every hop in that test shares one budget now.
4. The rate limiter is real: a test that batches dozens of reads is the caller the
   limiter exists to shape, so the batch test seeds through the store and waits out
   refills (`doWithinBudget`) instead of asserting against a limiter switched off.

**Stated, not hidden:** the store caches the rule snapshot for 15 s and the engine
caches a unit's scope keys for 60 s — a tag change is visible within a minute, and a
metric post costs no database read. A unit whose scope cannot be read is measured
against the global floor, not skipped: losing the evaluation is worse than
evaluating it against the installation defaults.

---

## 2.4-6 · Channels that reach a person

**Design constraint:** reuse the webhook machinery, don't duplicate it. Generalize
`webhooks` with a `kind` column (`webhook` | `email` | `slack` | `teams`) and a
`config` JSON column, so the existing `Dispatcher` (HMAC sign, delivery rows,
`RetryPending`, `SendTestEvent`) delivers all of them, and the delivery ledger stays
one place an operator can read.

**Do**
- [ ] Migration **15** (14 was consumed by 2.4-5's threshold scopes):
      `webhooks.kind`, `webhooks.config`, `webhooks.scope_clients`,
      `webhooks.min_severity`, `webhooks.tags`. Note `webhooks.url` is `not null`
      today and is meaningless for an email channel — that migration drops the
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
  `scripts/i18n-parity.py`.

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
- [ ] Migration 15: `session_recordings` — `session_id`, `device_id`, `user_id`,
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
scripts/i18n-parity.py
scripts/release.sh --check 2.4.0
```

Plus, non-negotiable for this release:

- [x] The 2.4-3 headline test — asserting an existing admin's email via a permissive
      IdP lands in **pending**, grants nothing, and is audited — green in CI
      (`TestSSOCannotClaimAnExistingAdminAccount`).
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
