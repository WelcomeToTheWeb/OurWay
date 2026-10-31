# Phase 2.4: Escalate to Humans, Own Your Identity — Task Breakdown

**Status:** All eight tasks delivered (2.4-1 … 2.4-8, last on 2026-10-30), and the
vendor interop pass (2.4-0) ran on 2026-10-31 against a live Keycloak 26 realm — **both
protocols**, ending in issued sessions, with a captured vendor document kept as a
permanent probe for each. It found three real SAML bugs; see 2.4-0 below. **Open:** a
second vendor (AD FS), and the release tag.
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

**15 (`notification_channels`) and 16 (`saml_single_sign_on`) are in `migrate.go`**,
consumed by 2.4-6 and 2.4-7. **Session recordings — the plan's 16 — is therefore 17**,
which is what 2.4-8 must use. The pattern is 2.3's: the plan numbers migrations by the
task list, and features arrive in the order they are built.

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
      a provider that can disagree. 2.4-7 shipped `server/sso/testsaml` for the
      **SAML** half. **Stated plainly: that is a deviation from this section's rule,
      not its fulfilment, and it is recorded as one.** `testsaml` is a *peer IdP* — it
      holds a real RSA signing key, mints assertions, signs them with real XML-DSig
      (enveloped RSA-SHA256 over the exclusive-c14n assertion), assembles the Response
      an IdP posts to an ACS, and sabotages itself on command (tampered, expired,
      wrong audience, wrong recipient, replayed, unsigned, signed by an unpinned key).
      That proves every refusal 2.4-7 names, because a **signer** produces the document
      first and the fixture breaks it afterwards — a stub of the verifier would agree
      with whatever the verifier does. What it cannot prove is **interop with a named
      vendor IdP**: the fixture signs with `goxmldsig`, the library OurWay verifies
      with, so canonicalization and digest agreement are proven against an independent
      implementation of the *same algorithm*, not against Keycloak, AD FS or
      Shibboleth. **The SAML half of that pass ran 2026-10-31, against a live Keycloak
      26 container** — realm `ourway` at `kc.sso.test` behind nginx, OurWay at
      `ow.sso.test`, the client built from **OurWay's own SP metadata** (fetched, parsed,
      POSTed to the admin API), `Sign on` + `Include assertion signature` on, realm
      certificate pinned, `nameid_format=persistent`, `attr_email=email`,
      `attr_groups=Role`. A browser ran the SP-initiated flow: `/authorize` → signed
      HTTP-Redirect → the realm login page → the posted assertion → the ACS `302` with a
      single-use code → `POST /api/auth/sso/exchange` → access + refresh tokens. It found
      **three real bugs**, each pinned by a test, and the first is exactly what this
      section said a fixture could not produce:
      1. **Exclusive canonicalization dropped the namespaces in scope from the enclosing
         `Response`.** Keycloak declares `xmlns:saml` on the `Response` and only the
         *default* namespace on the `Assertion`; exc-c14n of the signed subtree carries
         the in-scope declarations, so OurWay digested bytes Keycloak never signed and
         refused a valid assertion. `testsaml` declares its namespaces on the signed
         assertion, so our fixture agrees with our verifier — the vendor-shape case was
         invisible until a vendor produced it. Fixed in `saml.go`
         (`declareInScopeNamespaces` before `dsig.Validate`);
         `TestAnAssertionWhoseNamespacesComeFromTheResponseRootVerifies` proves it, and
         `testsaml` now mints both shapes (`SignAssertionInResponseNamespaces`).
      2. **`AuthnRequest.Destination` was our ACS URL.** The profile names the IdP's SSO
         location there; Keycloak answered `invalid_destination`.
      3. **The HTTP-Redirect signature covered the decoded parameters,** not the
         percent-encoded query string the browser transmits — the bytes every IdP
         verifies. Keycloak answered `invalid_signature`.
      The proof is kept as a probe, not a story: `server/sso/capture_vendor_saml_test.go`
      runs the vendor's **own captured document** through the verifier (shape guard: the
      assertion declares no prefixed namespaces; canonicalization method; expiry window),
      then refuses one tampered NameID byte (`assertion_signature_invalid`) and the
      stripped signature (`assertion_unsigned`). Fed by `OURWAY_SAML_CAPTURE`,
      `OURWAY_SAML_CERT`, `OURWAY_SAML_REQUEST_ID`, `OURWAY_SAML_AT`; it skips when
      unset, so the suite stays hermetic and the pass is re-runnable against any vendor
      that can produce a capture.
      **Still owed:** a second vendor (**AD FS**). The OIDC half ran 2026-10-31 the same
      day, and the thing it thought it needed turned out not to be needed: Keycloak
      publishes its issuer from the `Forwarded` headers nginx gives it, so
      `https://kc.sso.test/realms/ourway` is what its own
      `.well-known/openid-configuration` names, and the provider saved **by that issuer
      alone**. The issuer rule never refused it, because the rule's own remedy — publish
      the IdP under a name — is what a real installation does. The OIDC capture probe
      (`server/sso/capture_vendor_oidc_test.go`) is fed the IdP's **own minted
      `id_token`** and the **`jwks_uri` document it publishes**: verification against the
      vendor's key (`kid` lookup), the nonce binding, a tampered token refused, a token
      minted for another client refused. What a vendor puts in an `id_token` and our
      fixture did not: `azp`, `sid`, `at_hash`, `acr`, `auth_time`, `session_state`-derived
      `sub`, a boolean `email_verified`, and groups under `realm_access.roles`.
- [~] Mock SMTP (accepting, rejecting, and TLS-failing), Slack, and Teams webhook
      endpoints for channel tests. **2.4-6 shipped `server/webhooks/testsmtp`** — a
      real SMTP server fixture (EHLO, MAIL/RCPT/DATA, recipient and data rejection,
      STARTTLS offered / not offered / required, AUTH PLAIN accepted / refused, the
      accepted messages readable) — because a stub of OurWay's SMTP client would agree
      with whatever the client does. Slack and Teams are exercised against real local
      HTTP endpoints that record the posted card and can refuse the first attempts; a
      real Slack/Teams endpoint is still owed if the card schema is to be validated
      against the product, not against our reading of it.
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
      becomes an account, and it is contract-gated. **Asserted for SAML by 2.4-7**
      (`IssueSAMLIdentity` calls `contractGate` then `resolveAccount`;
      `TestSAMLLoginCannotClaimAnExistingLocalAccount`,
      `TestSAMLRoleMappingOwnsTheRolesItNames`,
      `TestSAMLLoginFeedsTheContractTheProviderConfigures`).
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
- [x] Replace the hardcoded provider dropdown with the provider ledger: presets as
      a starting point, issuer + client id/secret + discovery preview, claim
      mapping, domain allow-list, verified-email toggle, link policy, enabled order.
      — `web/src/pages/SSO.tsx`; the edit path is `PUT /api/sso/providers/:id`
      (in place, field by field, name immutable).
- [x] Login page renders providers in configured order (server answers
      `sort_order, name`; `Login.tsx` preserves it — `web/src/pages/Login.test.tsx`),
      and a disabled provider disappears without a console rebuild
      (`TestProviderOrderAndEnableIsTheLoginPagesState`).
- [x] All strings in `en`/`de`/`fr` lockstep, with `scripts/i18n-parity.py`
      (2.2-5) green — 632 key paths.

**Acceptance criteria**
- [x] An operator can add Keycloak, Authentik, and Zitadel from the UI alone, with no
  server restart, and each appears on the login page in the configured order.
  — The form takes **any** issuer (the presets pre-fill one, they do not gate it:
  `SSO.test.tsx` asserts the name field is editable for a new provider), saving needs
  no restart, and the order is the ledger's order.
- [x] `Test-connection` output distinguishes discovery failure, redirect mismatch, and
  JWKS failure with named states — the checks panel renders `pass / warn / fail /
  skipped` with the server's detail (`sso.state.*`, `sso.check.*`), and the preview
  panel names a refused discovery by kind (`issuer_mismatch` asserted in
  `SSO.test.tsx`).
- [x] Secrets never round-trip to the browser after creation — `client_secret` is
  `json:"-"`; the edit form's secret field is empty with the placeholder
  "Saved — type to replace it" (asserted), an edit sends a secret only to replace it,
  and the public provider list carries no endpoints and no secret
  (`TestProviderOrderAndEnableIsTheLoginPagesState` greps for the leak).

**Shipped with 2.4-4**
- `PUT /api/sso/providers/:id` with field-presence semantics: an omitted field is
  left alone, an explicit empty string is a state. Create and update share one request
  shape and one validation path, so no door skips a rule the other applies.
- The name is immutable (`409 name_immutable`): it is `users.provider`, the identity
  key of every account signed in through the provider. Editing it is not a label
  change; it would orphan those identities.
- Discovery re-runs on an edit and refuses loudly: `provider not changed:
  issuer_mismatch …`, leaving the stored row working.
- `sso.update` audit rows (name, type, issuer, enabled, link policy, allowed domains,
  verified-email requirement, role mapping, order — never the secret), registered in
  the route↔action table the 2.3-6 coverage test checks.

**Deferred (stated, not silent)**
- The form exposes the issuer, policy, order, claims (email/groups) and enable. The
  typed-endpoint overrides and the username/name claim overrides stay API-only: they
  are proxy workarounds, and 2.4-4's Do list is the ledger for the 2.4-1/2.4-3 fields.
  A provider with no issuer shows `typed endpoints` in the ledger rather than nothing.

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
- [x] Migration **15** (14 was consumed by 2.4-5's threshold scopes):
      `webhooks.kind`, `webhooks.config`, `webhooks.scope_clients`,
      `webhooks.min_severity`, `webhooks.tags`. `url`'s NOT-NULL is dropped on
      PostgreSQL, and kept on SQLite (where the column stays an empty string for a
      mail channel — SQLite cannot drop a column constraint). The API validates `url`
      **per kind** (required for webhook/slack/teams, refused for email). Event
      matching stays exact in Go (`store/webhooks.go:ListByEvent`, the M12 fix).
- [x] Senders: SMTP (STARTTLS, AUTH, `MAIL FROM`/`RCPT TO` from the channel config,
      templates for the alert/patch/device/session events in `en`/`de`/`fr`), Slack,
      and Microsoft Teams (MessageCard payload).
- [x] Routing: channel × client scope × minimum severity × tags, with per-channel
      event types.
- [x] `SMTP_HOST`/`SMTP_PORT`/`SMTP_USERNAME`/`SMTP_PASSWORD`/`SMTP_FROM`/
      `SMTP_STARTTLS` in `docs/configuration.md`, `.env.example` and
      `docker-compose.prod.yml` in the same change.
- [x] Console: the Webhooks page is a channel ledger with per-row routing, delivery
      history, test send, edit, and delete in the confirm sheet.
- [x] Every send outcome lands in the delivery ledger with the attempt count, the
      peer's status code and answer, and the scheduled retry.

**Acceptance criteria — and where each is proven**

- One scoped critical `alert_created` reaches mock SMTP, Slack and Teams in one
  dispatch pass, naming the unit, metric, value and threshold. ✅
  `server/webhooks/channels_test.go:TestOneAlertReachesEveryKindInOnePass` — the
  mail server is a **real SMTP server** (`server/webhooks/testsmtp`, EHLO/STARTTLS/
  AUTH/MAIL/RCPT/DATA), the chat endpoints are real HTTP servers, and the assertions
  are on the accepted message and the posted card text.
- Severity below `min_severity`, a channel scoped to an unlisted tenant, and a channel
  scoped to a tag the unit does not carry each produce **no delivery row**. ✅
  `TestRoutingRefusalsWriteNoDeliveryRow` — and the same channel is then shown
  delivering, so the refusals are proved to be the routing, not a broken sender.
- Retry keeps the webhook's record shape, and a channel that succeeds on the second
  attempt is delivered with `attempts: 2`. ✅ `TestRetryKeepsTheWebhookShapeForEveryKind`
  (first attempt recorded with the peer's answer and a scheduled backoff; the retry
  loop production runs, `RetryPending`, delivers it).
- A test send writes a pointable row; a channel that cannot be tested is named. ✅
  `TestTestSendWritesARowTheOperatorCanPointAt`, `TestTestSendNamesAChannelThatCannotBeTested`,
  and at the API `TestTestSendNamesAChannelThatCannotBeTested` (`server/api`).
- Email templates exist in all three locales, and the parity gate covers them. ✅
  `server/i18n/locales/{en,de,fr}.json` is now a **second bundle** in
  `scripts/i18n-parity.py` (30 key paths), and `server/i18n/i18n_test.go` checks what
  key comparison cannot see: every event template exists in every locale, templates
  reference only fields the renderer supplies, the three locales reference the **same**
  fields, and the embedded bundles equal the files on disk. `webhooks/mail_render_test.go`
  asserts the rendered mail states the unit/metric/value/threshold, is not English in
  `de`/`fr`, fills absent fields with an em dash, and falls back for an unknown event.
- Per-kind validation and refusals are named. ✅
  `server/api/channels_test.go` — `TestChannelKindRefusesWhatItsKindCannotDeliver`
  (missing URL per kind, email with a URL, email without recipients, email on an
  installation with no mail server, bad floor, unknown kind),
  `TestChannelKindIsFixedOnceBuilt` (`409`),
  `TestChannelTenantScopeNamesTheTenantTheCallerCannotSee` (`404` out of scope, `403`
  all-tenant, manager sees only their channels, technician/viewer have no door),
  `TestChannelWritesAreAuditedWithTheirRoutingAndNeverTheSecret`,
  `TestChannelToggleSendsOneFieldAndWipesNothing`,
  `TestDeliveryLedgerCarriesTheSameRecordForEveryKind`,
  `TestTheTwoNamesAddressOneLedger`,
  `TestAWebhookFromV23KeepsDeliveringAfterTheUpgrade`.
- STARTTLS required, server does not offer it → refused, no mail accepted. ✅
  `TestStartTLSRequirementRefusesAPlainServer`. An email channel on an installation
  with no mail server records the refusal by name and is **not** retried.
  `TestEmailChannelWithoutMailServerStatesTheRefusal`.
- The `webhook` kind's payload is v2.3's, signature and custom headers included. ✅
  `TestWebhookKindShipsTheV23EventShape`.

**What the tests caught**

1. **A refused delivery was recorded once and never retried.** `ClaimPending` moves the
   row to `in_flight`; `markFailed` set attempts, the peer's answer and `next_retry_at`
   and left the row there, while `FindPending` selects `pending` rows. The documented
   1m/2m/4m/8m backoff was dead code in every release before this one. A retryable
   failure now returns the row to `pending`.
2. Rebuilding the outbound payload at delivery time made a Slack/Teams retry send the
   *webhook* shape, because the stored payload was a chat card being re-decoded as an
   event. The sender now ships **the bytes the ledger holds**, which is also what makes
   a retry ship what the first attempt shipped.
3. Permanent refusals (a 4xx from an endpoint, a 5xx SMTP refusal, missing mail
   transport, STARTTLS required-but-not-offered) were scheduled for retry, filling the
   ledger with identical refusals. They now end on the first attempt and say so.
4. The footer mail template embeds `{{.To}}`; inserted raw, it left the literal
   placeholder in the body a person reads. The footer is rendered against the fields
   before it is placed.
5. `common.dismiss` was the visible label of three console buttons and existed in **no**
   locale file — every language showed the key path. `enable`/`disable` likewise.
6. The manager contract changed with the tenant scope (`POST /api/v2/webhooks` answers
   `403` for an unscoped channel): `TestRouteRoleMatrix` was updated to state the new
   rule, with the admin path and the `/v2/channels` alias asserted beside it.

**Stated, not hidden:** the mail transport belongs to the installation and the
recipients belong to the channel — that split is what makes "client A's criticals to
the on-call mailbox" one row. `/api/v2/webhooks` and `/api/v2/channels` are one ledger
through one handler set, so a v2.3 integration keeps its door. A channel's kind is
immutable: it decides the payload and what the delivery history means. The console
shows the mail transport's host, port and sender (never credentials) because that is
the fact that explains a mail channel that cannot deliver.

---

## 2.4-7 · SAML 2.0 (after OIDC, on purpose) · ✅ delivered 2026-10-30

**Delivered (2.4-7)** — `server/sso/saml.go` (the SP: parse, validate, deflate/sign
requests, metadata, identity extraction), `server/sso/testsaml/` (the peer-IdP
fixture), `server/api/sso.go` (ACS, metadata, the SAML half of authorize, the save-time
gate, the probe, the audit), `server/sso/oauth.go:IssueSAMLIdentity`,
`server/models/sso_provider.go`, `server/store/sso_providers.go` + `migrate.go`
(migration **16**, `saml_single_sign_on`), console `web/src/pages/SSO.tsx` and
`Login.tsx`, `web/src/api/sso.ts`. Dependencies added:
`github.com/russellhaering/goxmldsig` (canonicalization, RSA-SHA256 sign/verify) on the
`github.com/beevik/etree` the module already carried. Tests:
`server/sso/saml_test.go` (the SP against a real signed assertion) and
`server/api/saml_test.go` (routes, ledger, save gate, audit). Measured:
`go test ./sso/... ./api/... ./store/...` green; `npm run test:run --prefix web` green
for `SSO.test.tsx` / `Login.test.tsx`; `scripts/i18n-parity.py` green at 782 web key
paths.

**Do**
- [x] Replace the three rejection points (`server/api/sso.go` at create, authorize,
      and callback; `server/api/sso_test.go:25-26` asserts them) with an
      implementation — **and update those tests in the same change**, since they
      track the shipped contract (`CONTRIBUTING.md`).
      `TestSSOProviderTypeGuard` is now the type gate as shipped: `saml` (any case) is
      **accepted** when the row carries its IdP facts, while `openid-connect`,
      `single-sign-on-v3` and an empty type are still **400**. L8's surviving rule is
      "never store a selectable-but-broken provider", and 2.4-7 enforces it at save
      (`validateSAMLProvider`), not by refusing the protocol.
- [x] SP metadata endpoint per provider
      (`GET /api/auth/sso/saml/:name/metadata`, public, `application/samlmetadata+xml`),
      assertion consumer at `POST /api/auth/sso/saml/:name/acs`, SP signing key
      material per provider (cert + key, both `json:"-"`; `generate_sp_key` makes the
      pair, and a key with no certificate is refused). The metadata names the entity ID,
      `AuthnRequestsSigned`, `WantAssertionsSigned="true"`, the `NameIDFormat`, the ACS
      location and binding, and the SP **signing certificate** — and no key material,
      asserted by `TestSAMLSPMetadataNamesTheEntityAndCarriesNoKey`.
      `TestSAMLProviderIsBuiltFromTheIdPFacts` greps the admin provider list for
      `BEGIN CERTIFICATE` / `PRIVATE KEY` and fails on a leak. The SP entity ID is
      **derived** (`urn:ourway:<name>:<id>`) from the provider's immutable name and id,
      never typed — the assertion's audience names this installation.
- [x] Validate: signature on the assertion (and optionally the response),
      `Audience`/`AudienceRestriction`, `Recipient`, `Destination`, `NotOnOrAfter`
      /`SessionNotOnOrAfter`, `SubjectConfirmationData` `Recipient` +
      `NotOnOrAfter`, and **replay protection** on assertion `ID` (a seen-ID store
      with a TTL). All of it, with 90 s of clock-skew tolerance, plus
      `InResponseTo` against the request this login sent. `IssueSAMLIdentity` is reached
      only after validation — signature verification is not in the identity path.
- [x] Identity comes through the **2.4-3 contract**, unchanged: domain allow-list,
      verified-email equivalent, pending-identity default, opt-in role mapping from
      the group attribute. `IssueSAMLIdentity` calls `contractGate` then
      `resolveAccount` — the same functions the OIDC callback calls; nothing about the
      identity decision is duplicated in `saml.go`.
- [x] Console: the provider row exposes SP metadata for download and the IdP
      entity ID / SSO location / certificate fields (`SSO.tsx`: protocol choice at
      create and fixed on edit with the reason, the SAML field set, the **SP metadata**
      link, the row stamped `SAML 2.0` with the pinned certificate's fingerprint,
      expiry, and the signed/unsigned-request stamp; `Login.tsx` renders the public
      list's `label`, which for a SAML provider is the name the operator chose — an
      entity ID is not a thing a person can read on a login button). `en`/`de`/`fr` in
      lockstep (the SAML copy is asserted in `SSO.test.tsx`: "carries the SAML copy in
      all three locales, in the locale's own words").
- [x] **The save gate** (not in the Do list, and the reason the row is trustworthy): a
      SAML provider that cannot validate an assertion is **not saved** — missing
      `idp_entity_id`; missing, `http`, `localhost`, port-named or IP-literal
      `idp_sso_url` (2.4-1's issuer SSRF rule, applied to every endpoint a provider row
      names); a certificate that does not parse or whose window has closed; an unknown
      `nameid_format`; `require_verified_email` with no `verified_email_attribute`; a
      signing key with no certificate.
      `TestSAMLProviderRefusesAFormThatCannotDeliver` asserts each refusal names its
      field **and** that no row was written.
- [x] **The protocol is immutable** (`409 type_immutable`), proven in both directions in
      the same test (SAML→OIDC and OIDC→SAML): a SAML provider files its accounts by
      asserted **NameID**, an OIDC provider by **subject**.

**Acceptance criteria — and where each is proven**
- Full round trip against the fixture: metadata → IdP-configured → SP-initiated login →
  assertion accepted → session issued through the existing `sso_code` exchange (no
  token in a URL, per the C6 fix). ✅
  `TestSAMLLoginRoundTripIsARealAssertionTheSPAccepts` (`server/sso`) and
  `TestSAMLLoginIssuesASessionThroughTheExchange` (`server/api`): authorize → signed
  redirect → ACS `302` → `sso_code` → `POST /api/auth/sso/exchange` → a session naming
  the asserted subject, and the `auth.login` row states `protocol: saml` and
  `assertion_signature_verified: true`.
  **Done 2026-10-31 (2.4-0):** the same walk against **Keycloak 26** — metadata →
  client imported from it → SP-initiated login → assertion accepted → session issued
  through the `sso_code` exchange → tokens. Three vendor-shape bugs found and fixed
  (in-scope-namespace canonicalization, request `Destination`, request-signature bytes);
  the captured assertion is a permanent probe, `TestAVendorCaptureValidatesRefusesTamperingAndRefusesAnUnsignedAssertion`.
  **AD FS is not covered.**
- Negative fixtures each refused with a distinct audited outcome: tampered assertion,
  wrong audience, expired `NotOnOrAfter`, wrong recipient, a replayed assertion, an
  unsigned assertion. ✅ `TestSAMLRefusalsAreNamedInTheLedger` (all five: each a 401
  with its own `reason` and an `auth.login_failed` row, provisioning nothing) plus
  `TestSAMLAssertionsAreRefusedForTheNamedReason` (the SP-level set —
  `status_refused`, `issuer_mismatch`, `destination_mismatch`,
  `in_response_to_mismatch`, `no_assertion`, `subject_confirmation_method`,
  `response_expired`, `response_not_yet_valid`, `audience_mismatch`,
  `recipient_mismatch`, `session_start_in_future`),
  `TestSAMLAssertionSignedByAnUnpinnedKeyIsRefused`,
  `TestSAMLReplayedAssertionIsRefused` (a captured valid POST is refused as
  `assertion_replayed`) and `TestSAMLResponseSignatureIsRequiredWhenTheProviderAsks`.
- The identity-linking tests from 2.4-3 pass unchanged for SAML — one contract, two
  protocols. ✅ `TestSAMLLoginCannotClaimAnExistingLocalAccount` (403
  `identity_pending`, the admin's roles and password untouched, the pending row naming
  the target), `TestSAMLRoleMappingOwnsTheRolesItNames`,
  `TestSAMLLoginFeedsTheContractTheProviderConfigures` (`domain_not_allowed`,
  `email_unverified`, and a provider with no policy completing),
  `TestSAMLLoginIsScopedToTheProviderItNames`. The 2.4-3 suite itself is unmodified.
- `server/api/sso_test.go` passes with the rejection tests replaced by acceptance tests
  for SAML and kept as rejections for any *other* unsupported type. ✅
  `TestSSOProviderTypeGuard`, updated in the same change.
- **Added by 2.4-7** (the shape of the door, asserted): an IdP-initiated login is
  accepted on the audience/recipient/destination/expiry rules alone, and a document
  with no `InResponseTo` arriving while our request is outstanding is refused
  (`TestSAMLIdPInitiatedLoginIsAcceptedOnTheAssertionRules`); the OIDC callback answers
  a SAML provider with `saml_uses_acs` and the ACS URL;
  `TestSAMLDisabledProviderDoesNotAnswer` (neither route answers);
  `TestSAMLTestProbeAnswersTheQuestionsSAMLCanAnswer` (an expired certificate named as
  `fail`); `TestSAMLWritesAreAuditedWithTheFingerprintAndNeverTheKey`;
  `TestAProviderFromBeforeSAMLKeepsLoggingPeopleIn` (migration 16's regression case).

**What the tests caught**

1. `CreateProvider` required `client_id` **unconditionally**. A SAML provider has no
   client id — its identity is the SP entity ID the audience names — so the gate had to
   become protocol-aware, and the row is asserted to carry none.
2. `applyDiscovery` ran for every provider at save. On a SAML row it fetches an issuer
   the row does not have and refuses a **correctly configured** provider with
   `discovery_unreachable`; the SAML path skips discovery, because its endpoints are the
   IdP's entity ID and SSO location, which `validateSAMLProvider` checked.
3. Migration 12's lesson repeated: `AutoMigrate` adds nothing to an existing SQLite
   table, and `AddColumn` does **not** apply the model default to rows that already
   exist. An empty `nameid_format` is a state no code path reads — it asks every IdP for
   an unspecified format — so migration 16 backfills the value SAML's default is.
4. `require_verified_email` had no SAML equivalent: SAML has no standard "this email is
   verified" attribute. The email-format NameID is the protocol's best reading, and the
   shipped rule is stricter than a guess — a provider that **requires** a verified email
   must name the attribute it reads, refused at save and reported by the probe.

**Stated, not hidden:** the assertion is a bearer artifact valid for its whole window,
so a consumed assertion `ID` is remembered for the window in which it would still
validate (12 min, derived from the accepted lifetime + skew): a captured POST is
refused, not re-accepted. The posted form is size-capped (1 MiB) before anything is
parsed and its body is never logged. `assertion_unsigned` and
`assertion_signature_invalid` are separate reasons because they are separate fixes on
the IdP side. Key material is write-only in both directions: the console sees the
certificate's fingerprint and expiry, the ledger records the fingerprint, and the only
copy of the SP private key is the server's row. **Deferred** (in "Deferred inside
2.4" below): artifact binding, IdP-initiated SLO, encrypted assertions.

---

## 2.4-8 · Session recording, so the audit log has an answer ✅ delivered 2026-10-30

**Context:** `server/models/session.go` records tokens and status, no media;
`POST /api/sessions/:id/frame` relays frames to the owner and stores nothing.
`server/files/service.go` is the existing shape for server-side blob storage
(directory, per-transfer file, cleanup sweep) — recordings follow it.

**Delivered**
- [x] Migration **17** (`session_recordings`): `server/models/session_recording.go` —
      `session_id` (unique: a session has one recording), `device_id`, `user_id`,
      `client_id`, `started_at`, `ended_at`, `frame_count`, `bytes`, `path`, `checksum`,
      `interruptions`, `dropped_frames`, `reason`, `policy`, `retention_until`.
      `server/store/recordings.go` is the manifest store (create, get by session,
      own/scoped lists, flush, end, open rows, prune-past-retention, count).
- [x] Append-only JPEG sequence under `RECORDINGS_DIR` (`server/recordings/writer.go`):
      `OWRS1` header + length-prefixed records (kind, unix-ms timestamp, length,
      payload), each record synced; 25 MiB per frame (the frame endpoint's own cap),
      2 GiB per recording. No container format, no re-encode, no ffmpeg: a crash leaves
      every earlier frame readable, and a tail cut mid-record is read as
      `truncated`. Frames are appended **from the frame path the live session already
      uses** (`ReportFrame` → `appendFrame`), so the recording exists for the session
      the console watched.
- [x] Retention sweep on the sweep that already exists (`server/recordings/sweep.go`,
      attached to `metrics.RetentionManager.WithRecordings`): closes recordings the
      database says are open while no session is producing frames (never one this
      process is appending to), prunes rows past `retention_until`, then removes the
      files they name. Counters: `RecordingsDeleted`, `RecordingFiles`,
      `RecordingsClosed`, plus the run timestamp, on `RetentionCounters`.
- [x] Policy in `server/config/config.go` (`RECORD_SESSIONS`, `RECORDINGS_DIR`,
      `RECORDING_RETENTION_DAYS`; default `off`, `./dist/recordings`, 30). An
      unparseable mode prints what it received and runs `off`. `control_only` is
      normalized to `control-only`. Documented in `docs/configuration.md` and
      `.env.example`.
- [x] The **toggle starts a recording and nothing stops one**:
      `POST /api/sessions/:id/recording` (the session's operator, or admin for a
      session in scope), `409 recording_disabled` under `off`, `409 session_over` for a
      finished session, and the existing row returned when asked twice. The only end is
      the session's end (`EndSession` → `finishRecording`). Under `control-only` the
      first control input starts the recording (`noteControlInput`), and the row's
      `reason` says which rule made it.
- [x] The console **states the promise**: the live session panel carries
      `Recording · N frames` / `Recorded` / `Not recorded` and a *Record session*
      button; the sessions ledger has the Recording column and button; `/recordings` is
      the ledger (unit, account, tenant, window, frames + dropped + resumes, size,
      state, **why it exists**) and opens playback.
- [x] Playback (`web/src/components/RecordingPlayer.tsx`): manifest + frame index with
      the time of each frame, frames fetched one at a time through the authenticated
      client (an `<img src>` carries no bearer token, and a token in a URL is a token in
      the access log), scrub, 1 frame/s play, the input events the ledger recorded for
      that session laid over the frame they produced, and the manifest/sequence exports.
      Interruptions, cap, truncation and checksum mismatch are printed, not smoothed.
- [x] Access control (2.3.1) and audit: `admin` all, manager/technician their tenants'
      recordings **plus their own always** (a technician's record of what they did does
      not become unreadable when their grants change); an out-of-scope recording answers
      the byte-identical 404. `GET /api/recordings/:id` writes
      `session.recording_play` naming the player; the manifest and sequence exports
      write `session.recording_fetch`; frames are the stream and are not audited per
      frame. Starting a recording writes `session.record_start`.
- [x] Restart continuity: the writer resumes the same file (row → `path`),
      `interruptions` +1, a `resume` marker in the sequence, and the resumed writer
      counts the frames already in the file, so the manifest never reads as a recording
      that started over.

**Acceptance criteria → the tests that prove them**

| Criterion | Proof |
| --- | --- |
| A control session under `control-only` produces a recording with frame count and byte size, and playback shows the input events at the frames they were sent | `TestControlOnlyStartsAtTheFirstControlInput`, `TestInputIsItsOwnActInTheLedgerAndTheOverlayReadsIt`, `TestRecordedFramesPlayBackInOrderWithTheManifestsChecksum` (`server/api/recordings_test.go`) |
| A scoped technician gets 404 on a client-B recording by ID, byte-identical to a recording that does not exist | `TestRecordingsAreScopedToTheTechniciansClients` |
| Playing a recording writes an audit row naming the player and the recording | `TestPlayingARecordingIsAuditedAndFetchingFramesIsNot`, `TestPolicyOnRecordsEverySessionFromItsStart` |
| Retention sweep removes `retention_until` rows **and their files**, observably | `TestRetentionSweepPrunesRecordingsAndTheirFiles`, `TestSweepClosesAbandonedRecordingsAndLeavesLiveOnes`, `TestRetentionSweepsRecordingsOnTheSameTick` (`server/metrics`) |
| `RECORD_SESSIONS=off` (default) produces zero rows and zero files | `TestRecordingIsOffUnlessTheInstallationSaysSo`, `TestLoadRecordingPolicyDefaultsToOff` (`server/config`) |
| The file format's promises: append-only, crash-safe, resumable, capped, addressable by index | `server/recordings/writer_test.go` — `TestSequenceIsAppendOnlyFramesWithTimestamps`, `TestResumeAppendsToTheSameRecordingAndCountsTheInterruption`, `TestTruncatedTailKeepsEarlierFramesAndSaysItIsTruncated`, `TestWriterCapDropsFramesAndCountsThem`, `TestARestartResumesTheSameRecordingAndStatesTheInterruption` (API level) |
| The live session panel states which sessions are recorded, and a view-only session under `control-only` is not | `TestTheSessionListStatesWhichSessionsAreRecorded`; console: `web/src/pages/Recordings.test.tsx` (8 tests), `SessionView` stamps |
| The recording is capped and the session survives it | `TestARecordingThatReachesItsCapSaysSoAndKeepsTheSessionAlive` |
| A person always reads their own recording | `TestATechnicianAlwaysReadsTheirOwnRecording` |

**Gates measured with 2.4-8 in:** `go -C server test -p 1 -race -count=1 ./...` green
(14 packages), `npm run test:run --prefix web` 193 tests (26 files), `npm run typecheck`,
`npm run build`, `scripts/i18n-parity.py` (no new keys: 782 web + 30 server paths in
lockstep), `scripts/browser-dialogs-check.sh` (0 native dialogs, 111 sources).

**Deviation from the plan, stated plainly:** the plan's criterion is *"a 3-minute control
session"*; the tests run seconds-long sessions with 2–25 frames, because the assertions
are the counters, the scoping and the audit rows, not wall-clock length. The manifest's
live counters are a **sample** (the row is written every fifth frame of the file; the
read that shows a recording refreshes it from the writer, and the session's end writes
the final numbers) — a per-frame manifest write would put a database write in the live
session's hot loop for a number nobody reads mid-frame.

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
- [x] OIDC accepted against Keycloak by issuer alone; SAML round trip green; the
      negative fixtures in 2.4-2 and 2.4-7 each refused. **Both halves of the vendor pass
      ran, against a live Keycloak 26 realm** (2.4-0 above): SAML over the metadata +
      pinned-certificate path, ending in an exchanged session; OIDC **by issuer alone**
      (`https://kc.sso.test/realms/ourway` read through the IdP's own published
      `*.well-known/openid-configuration`, endpoints stored at save), ending in a
      session through the `sso_code` exchange — and the 2.4-3 contract fired on the
      vendor's identity on the way there (a first login by a brand-new subject landed
      `identity_pending`, was listed for an admin, approved, and only then issued a
      session). Every negative fixture 2.4-2 and 2.4-7 names is green, and **both
      protocols keep a permanent probe fed by a captured vendor document**:
      `TestAVendorCaptureValidatesRefusesTamperingAndRefusesAnUnsignedAssertion` (the
      SAML assertion) and
      `TestAVendorIDTokenVerifiesRefusesTamperingAndRefusesAnExpiredToken` (the
      IdP-minted `id_token` verified against the IdP's own published JWKS, with the
      tampered token and a foreign audience refused). **Not covered:** a second vendor
      (AD FS).
- [x] Threshold precedence suite green, including the `v2.3.x` byte-identical
      regression case for devices with no override. **Green since 2.4-5** —
      `TestThresholdPrecedenceUsesTheMostSpecificRulePerMetric`,
      `TestDeviceWithNoRuleProducesTheShippedAlertExactly`,
      `TestAutoResolveUsesTheResolvedWarning`,
      `TestRuleDedupWindowOverridesTheEngineDefault`,
      `TestThresholdSimulationAgreesWithTheEngineOnTwentyValues`.
- [x] A scoped critical alert reaches mock SMTP + Slack + Teams in one engine pass,
      and out-of-scope/low-severity events create **no** delivery rows. **Green since
      2.4-6** — `TestOneAlertReachesEveryKindInOnePass`,
      `TestRoutingRefusalsWriteNoDeliveryRow` (`server/webhooks`), against a real SMTP
      server (`server/webhooks/testsmtp`).
- [x] Recordings are client-scoped, playback is audited, and the default is off.
      **Green since 2.4-8** — `TestRecordingsAreScopedToTheTechniciansClients` (the
      byte-identical 404, the scoped list, the admin's and manager's doors),
      `TestPlayingARecordingIsAuditedAndFetchingFramesIsNot` (the manifest read names
      the player, the frame stream writes nothing),
      `TestRecordingIsOffUnlessTheInstallationSaysSo` +
      `TestLoadRecordingPolicyDefaultsToOff` (off records nothing, and says so),
      `TestATechnicianAlwaysReadsTheirOwnRecording`.
- [x] `docs/api.md` updated for every new route with the 2.2-7 route↔docs check
      green; new env vars documented in `docs/configuration.md` and `.env.example`.
      **Every route this release added is documented** — SSO discovery/test/edit
      (2.4-1/2.4-4), pending-identity approve and merge (2.4-3), the threshold rules
      and the resolve/simulate reads (2.4-5), the channel ledger and its `/v2/webhooks`
      alias (2.4-6), the SAML metadata + assertion consumer routes (2.4-7), and the
      recording ledger — `POST/GET /api/sessions/:id/recording`,
      `GET /api/recordings`, `/recordings/:id`, its `/frames`, `/frames/:index`,
      `/events`, `/manifest` and `/file` (2.4-8), plus the session list's
      `recordings` / `recording_policy` fields and the input route's audit action.
      New env vars: the SMTP set (2.4-6) in `configuration.md`, `.env.example` and
      `docker-compose.prod.yml`; 2.4-7 adds none, and `configuration.md` now states that
      `WEB_URL` is the base of the SAML ACS URL; 2.4-8 adds `RECORD_SESSIONS`,
      `RECORDINGS_DIR` and `RECORDING_RETENTION_DAYS` to all three, with
      `recordings_data` mounted as a volume in the production compose file. **Stated:** the "route↔docs check" has
      no script behind it — `scripts/` checks i18n parity, native dialogs, coverage and
      the release gate, and `TestEveryMutatingRouteIsAudited` is the executable route
      check on the audit side, not the docs side. The route↔docs comparison is made by
      reading `server/api/handlers.go` against `docs/api.md`, which is how the list
      above was built.

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
