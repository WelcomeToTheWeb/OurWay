# Changelog

All notable changes to the OurWay RMM project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.4.0] - Unreleased

### Added
- **Generic OIDC by issuer + discovery** (2.4-1) — an SSO provider is configured by
  naming its **issuer**; OurWay fetches
  `<issuer>/.well-known/openid-configuration` at save time and stores the endpoints
  the provider publishes (`authorization_endpoint`, `token_endpoint`,
  `userinfo_endpoint`, `jwks_uri`, `id_token_signing_alg_values_supported`).
  Precedence: what the operator typed > what the IdP published > the preset.
  Keycloak, Authentik, Zitadel, kanidm, Okta and a home IdP are now a form, not a
  code change: `google`/`microsoft`/`apple` stopped being six hardcoded URLs each and
  became **presets of the same path** (they supply an issuer). Migration 12
  (`oidc_provider_discovery_and_identity_policy`).
- **Discovery refuses loudly, at save time.** An issuer that does not answer, answers
  with a document that is not a usable OIDC configuration, describes a **different**
  issuer, or offers no authorization-code flow answers `400` with a named kind
  (`discovery_unreachable`, `discovery_document`, `issuer_mismatch`, `no_code_flow`)
  and **writes no provider row**. No half-saved provider, no login that 500s at 2 a.m.
- **`POST /api/sso/providers/discover`** (admin, audited `sso.discover`) — the
  discovery **preview**: the endpoints, signing algorithms, scopes, PKCE methods, the
  document verbatim, the redirect URI this installation will send, and the named
  checks, shown **before** anything is saved. Admin only: probing the installation's
  identity provider is not a tenant operation.
- **`POST /api/sso/providers/:id/test`** (admin, audited `sso.test`) — test-connection
  as named states, never a 500: discovery, the redirect URI the flow will send,
  the JWKS, the `email` scope, PKCE support, and the id_token signing algorithm
  (OurWay verifies RS256, so an ES256-only IdP is named as unusable).
- **Multiple providers, ordered** — `sort_order, name` decides the login page's order,
  and the public provider list carries `id`/`type`/`name`/`label`/`sort_order` and
  nothing else: no endpoints, no secret (`client_secret` stays `json:"-"`).

- **The `id_token` is verified before any identity is trusted** (2.4-2) — signature
  against the provider's published `jwks_uri` (RS256, `kid` lookup, a refresh on an
  unknown `kid` so a key rotation keeps logins working), `iss` = the configured issuer,
  `aud` = this provider's client id, `exp` required (60s leeway), and the `nonce` = the
  one this login sent. **PKCE** (`S256`) on the code flow when the provider publishes a
  challenge method, with the verifier and nonce held server-side against the CSRF state
  and consumed once.
- **userinfo and the signed token must agree on `sub`.** Disagreement is a refused
  login, audited — OurWay does not pick which of the two people a provider named.
- **SSO refusals answer a named reason and write a ledger row.** 401, never a 500, with
  `code_exchange_failed`, `missing_id_token`, `id_token_verification_failed`,
  `nonce_mismatch`, `subject_mismatch`, `userinfo_failed`, `no_identity_claim`,
  `state_mismatch`, `state_expired` — and an `auth.login_failed` row naming the reason
  **and the asserted email**. A refused login provisions no account.
- **A completed SSO login is audited as `auth.login`** with the account it arrived as,
  its roles at the time, and the verification state stated
  (`id_token_verified`, `email_verified`). No token material in the row.
- **`users.sso_attributes` is a claim summary, not a credential** — `provider`,
  `provider_id`, `name`, `email`, `email_verified`, mapped `groups`, as JSON. The raw
  `id_token` (an access credential to another system, in the table a backup copies) is
  gone.

- **The identity-linking contract** (2.4-3) — the release's gate test. **An SSO
  identity claims the account it already belongs to, never the account its email
  names.** `findOrCreateUser`'s "find by email" fallback is gone: a provider asserting
  the email of an **existing** account produces a **pending identity** (no roles, no
  login, an audit row) and an admin's decision, not a session as that account.
  - `allowed_domains` refuses assertions outside the list (`domain_not_allowed`);
    `require_verified_email` honours the `email_verified` claim (`email_unverified`).
  - JIT provisioning lands at the **`viewer` floor** and no role is inherited from an
    email match. Role mapping is **opt-in per provider** (`role_mapping`, e.g.
    `{"rmm-admins":"admin"}`): configured, it owns only the roles it names, granting
    them while the group claim asserts them and taking them back when it stops.
  - `link_policy`: `pending` (default) parks the assertion for an admin; `link` is the
    operator's explicit choice to link as 2.3 and earlier did, audited as `sso.link`.
  - Admin surface: `GET /api/users/pending`, `POST /api/users/:id/approve` (activates
    the identity as its own account at the floor) and `POST /api/users/:id/merge`
    (attaches it to the account its email belongs to, roles untouched). Audited as
    `user.approve` / `user.merge`. Migration 13 (`sso_pending_identities`) adds
    `users.status` (`active` / `pending`), `users.pending_provider` and
    `users.pending_link_user_id`; existing accounts are `active`.
  - Refusals split by which half refused: **401** = the identity was not proven,
    **403** = the identity was proven and the contract refused it
    (`identity_pending`, `account_pending`, `domain_not_allowed`, `email_unverified`).
    A pending account cannot sign in with a password, and is refused by name.
  - Console: the **Users** page (admin) shows each pending assertion beside the
    account it points at — asserted email, verified-by-IdP fact, provider, subject,
    target account and its roles — with Approve and Merge confirming in the sheet. A
    page-level alert now carries refusals; `formError` used to render only inside the
    modals, so a rejected act looked like nothing had happened.
- **Alert thresholds are rules, with precedence** (2.4-5) — `alert_thresholds`
  (migration 14, **no seeded rules**): a rule names a scope (`device` | `tag` |
  `client` | `global`), a metric (`cpu`/`ram`/`disk`), `warning`, `critical`, an
  optional `dedup_window_seconds`, and `enabled`. The engine measures each metric
  against the **most specific enabled rule** — device → tag → client → global,
  per metric independently, no blending — and a unit no rule covers is measured
  against exactly the numbers `v2.4.0` alerted with (cpu 80/90, ram 85/95,
  disk 85/95), stated on the API as `defaults`.
  - **Auto-resolution follows the resolved rule**: a unit whose warning is 50 is
    recovered at 48. Measuring recovery against the installation's 80 leaves an
    alert open that the operator's own rule says is over. The alert's `threshold`
    and its message name the rule that fired, and the rule's repeat window wins
    over the engine's 5-minute default.
  - **`GET/POST/PUT/DELETE /api/alerts/thresholds`** (admin, manager; audited
    `alert.threshold_create` / `_update` / `_delete` with scope, metric and the
    numbers). Managers act inside their tenants — a device/client rule for a tenant
    they cannot see answers the body an absent tenant gets, and `tag`/`global`
    rules are admin-only. Warning must sit below critical, percentages are 0–100,
    a scope value is required, one rule per (scope, metric) — a duplicate answers
    `409` naming the rule to edit — and a saved rule's **scope and metric are
    immutable** (`409`): moving a rule to another unit is a delete and an add.
  - **`GET /api/alerts/thresholds/resolve?device=`** — the precedence preview: per
    metric, the rule that applies plus the whole chain in the order it wins, ending
    at the built-in floor. **`GET /api/alerts/thresholds/simulate`** answers a value
    with the severity, the scope that answered and **the engine's own message**,
    recording nothing. Both walk the 2.3.0 scoped-device door: an out-of-scope unit
    answers the body an absent unit gets.
  - Console: **Alert rules** page (admin, manager) — the ledger (scope, the name of
    what it points at, metric, levels, repeat window, enable toggle), the built-in
    floor stated in numbers, the per-unit chain preview, the value simulation, and
    deletes carried in the confirm sheet.

- **The provider ledger** (2.4-4) — **Settings → SSO** stopped being a hardcoded
  dropdown of the big three. The page lists the doors in the order the login page will
  render them, each with its **issuer**, its identity contract (`pending` / `links by
  email`, the domain allow-list, `verified email`, `role mapping`), its order, an
  enable toggle, and **Test** / **Edit** / **Delete**. Presets pre-fill an issuer;
  **any** issuer is typed and previewed — the panel shows the published endpoints, the
  signing algorithms, scopes, PKCE methods, the named checks, and **the redirect URI to
  register with the IdP** (audited `sso.discover`).
- **`PUT /api/sso/providers/:id`** — edit a provider **in place**, field by field: an
  omitted field is left alone (the enable toggle sends one field and cannot wipe a
  policy), an explicit empty value is a real state, and `client_secret` is sent only to
  replace it. Discovery re-runs; an unreachable issuer answers the named kinds with
  `provider not changed` and leaves the stored row working. **The name is immutable**
  (`409`, `name_immutable`): it is the identity key stored on every account signed in
  through this provider. Audited as `sso.update` — never the secret.
- **Notification channels: the webhook generalized to mail and chat** (2.4-6) — one
  row is now a **channel** of four kinds: `webhook` (the v2.3 HTTP endpoint, payload
  and HMAC signature byte-identical), `slack`, `teams` and `email`. Migration 15
  (`notification_channels`) adds `kind`, `config`, `scope_clients`, `min_severity`
  and `tags` to `webhooks`; an installation that already had webhooks delivers
  exactly what it delivered before — same URL, events, custom headers, signing
  secret and enabled state — with no operator step.
  - **Routing is stated on the row and applied before a delivery row exists.** A
    channel listens on its events, a **severity floor** (`info`/`warning`/`critical`),
    the **tenants** it reaches and the **tags** its units carry. An event below the
    floor, from a tenant the channel does not list, or from a unit carrying none of
    its tags produces **no record at all** — not a failed row an operator has to
    explain.
  - **One ledger, one retry loop, one console for every kind.** A Slack post, a
    Teams MessageCard, a rendered mail and an event JSON all write the same delivery
    row (event, attempts, peer status code, peer answer, next retry), retry on the
    same 1m/2m/4m/8m backoff, are tested through `POST …/test` (which queues through
    the channel's own routing, so the test is pointable), and answer `400`/`403`/
    `404`/`409`/`422` with a `reason` and words that name the requirement. A test
    send on a channel that does not subscribe to the `test` event is refused, and the
    page says so — never a reported send with no record.
  - **SMTP is a transport, not a feature toggle** (`SMTP_HOST`, `SMTP_PORT`,
    `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_STARTTLS`): the installation
    owns the server and the credentials, the **channel** owns the recipients
    (`config.to`), which is what makes "client A's criticals to the on-call mailbox"
    one row. `SMTP_STARTTLS` refuses to send in clear when the server does not offer
    STARTTLS. Defining an email channel on an installation with no mail server is
    refused at save time (`mail_not_configured`), and the console states the
    transport it has (host, port, sender — never credentials).
  - **The mail an operator sends is as localizable as the console.** Subjects and
    bodies are templates in `server/i18n/locales/{en,de,fr}.json`, checked by
    `scripts/i18n-parity.py` as a **second locale bundle**, and by tests that the
    three locales reference the same fields, that every field the renderer supplies
    is a field a template may name, and that a German mail is not the English mail.
  - **`/api/v2/channels` is the same ledger as `/api/v2/webhooks`** — both names
    address the same handlers, so a v2.3 integration keeps its door and the console
    has a name that describes the row. Create and update share one validation path;
    field presence is respected (the enable toggle sends one field and cannot wipe
    the routing); **kind is immutable** (`409`) because it decides what the payload
    and the delivery history mean; an email channel rejects a `url` and names
    `config.to`; a tenant the caller cannot see in `scope_clients` answers `404`;
    a channel reaching **every** tenant is admin-only. Writes are audited
    (`webhook.create` / `_update` / `_delete` / `_test`) with kind, destination,
    events, floor, tenant scope, tags and the mail addresses — never the signing
    secret and never the SMTP credentials.
  - Console: **Webhooks** is a channel ledger (kind, destination, tenants and floor,
    events, open/closed) with per-row enable toggle, test send, delivery history
    (peer answer, status code, next retry, retry now), edit, and delete carried in
    the confirm sheet naming what stops arriving. The add form asks a mailbox for
    recipients and the other kinds for an endpoint, so it cannot ask for a column a
    mailbox does not have.

- **SAML 2.0 single sign-on** (2.4-7) — the second protocol the identity contract
  serves. An SSO provider row is now `oidc` **or** `saml`: a SAML door is configured by
  the IdP's own published identity — `idp_entity_id`, `idp_sso_url`, an optional
  `idp_logout_url`, and the **pinned IdP signing certificate** — plus the attribute
  names the claims travel in and the NameID format to ask for. No discovery document to
  fetch, no client id: the SP's identity is `urn:ourway:<name>:<id>`, derived from the
  provider's immutable name and id, because the assertion's audience must name **this**
  installation. Migration 16 (`saml_single_sign_on`) adds the columns to
  `sso_providers` and backfills `nameid_format`; **2.4-8's session recordings move to
  migration 17** (2.4-6 and 2.4-7 consumed 15 and 16).
  - **SP metadata + assertion consumer service.**
    `GET /api/auth/sso/saml/:name/metadata` (public) serves the `EntityDescriptor` an
    operator uploads to the IdP — entity ID, ACS location, NameID format,
    `AuthnRequestsSigned`, `WantAssertionsSigned="true"`, the SP signing certificate
    when there is one, and **no key material**, ever.
    `POST /api/auth/sso/saml/:name/acs` is the only door that accepts an assertion: the
    IdP's posted `SAMLResponse` (base64, capped at 1 MiB before anything is parsed),
    validated **as an identity** before any account is looked at. SP-initiated logins
    answer **this** login's `AuthnRequest` (deflated, RSA-SHA256-signed when the
    provider has an SP key, its request ID held server-side against the CSRF state);
    an IdP-initiated post is accepted on the audience, recipient, destination and
    expiry rules alone.
  - **What validation means.** RSA-SHA256 signature over the assertion (exclusive
    canonicalization) against the certificate **the operator pinned**; the response
    signature when the provider asks for it; issuer = `idp_entity_id`;
    `Destination` = this installation's ACS URL; `AudienceRestriction` naming this SP
    entity ID; `SubjectConfirmationData.Recipient` and bearer confirmation;
    `NotOnOrAfter` / `IssueInstant` / the session window with 90 s of clock-skew
    tolerance; and **replay protection** — a consumed assertion `ID` is remembered for
    the whole window in which it would still validate. An unsigned, misaddressed,
    expired, foreign-audience, wrapped or replayed assertion provisions no account.
  - **One contract, two protocols.** The asserted identity goes through the 2.4-3 gate
    unchanged — `contractGate` then `resolveAccount`, the same functions the OIDC path
    uses: domain allow-list, verified email (`verified_email_attribute` names the
    attribute, because SAML has no standard one and OurWay does not guess),
    pending-by-default, the `viewer` floor, role mapping from the group **attribute**.
    Refusals keep 2.4-2's split — **401** the assertion was not proven, **403** proven
    and refused by the contract — with 17 named SAML reasons (`assertion_unsigned` and
    `assertion_signature_invalid` are deliberately separate: “not signing” and “not
    verifying against your pinned certificate” are different fixes on the IdP side),
    each written to the ledger. A completed login ends at the same one-time `sso_code`
    exchange (C6): no token in a URL, and the posted assertion is never logged. The
    `auth.login` row names the `protocol` and states
    `assertion_signature_verified`.
  - **Refused at save, loudly, exactly like an unreachable issuer** — and no row is
    written: a missing `idp_entity_id`, an `http`, `localhost`, port-named or
    IP-literal SSO location (2.4-1's issuer SSRF rule, applied to every endpoint a
    provider row names), a certificate that does not parse or whose validity window has
    closed, an unknown `nameid_format`, a verified-email requirement with no attribute
    named, a signing key with no certificate. The IdP certificate and the SP's private
    key are `json:"-"`: the console sees the certificate's **SHA-256 fingerprint** and
    expiry, and the audit row records the fingerprint, never the PEM.
  - **The protocol is immutable** once the provider is saved — `409 type_immutable`:
    a SAML provider files its accounts by asserted **NameID**, an OIDC provider by
    **subject**, and a row switched under an operator's feet would answer logins with
    rules it never had. The OIDC callback answers a SAML provider with `saml_uses_acs`
    and the URL that works, and `authorize` starts the SAML login from the same route.
  - Console: the provider form asks **OpenID Connect or SAML 2.0**, takes the IdP's
    facts, can generate the SP signing key pair, puts **Metadata** on the row for
    download, and stamps every row `OIDC` / `SAML 2.0` with the SP entity ID, the
    pinned certificate's fingerprint and its expiry. `Test-connection` asks the
    questions SAML can answer — certificate fingerprint, expiry and days left (`fail`
    once expired, the failure that otherwise arrives as a wave of refused logins), the
    sign-in URL rule, the ACS URL and entity ID the IdP must be told about, whether the
    metadata parses, whether the SP signs its requests, and whether a verified-email
    requirement has an attribute to read. `en`/`de`/`fr` in lockstep (782 key paths).
  - **Not in 2.4-7** (stated, not silent): artifact binding, IdP-initiated single
    logout, and encrypted assertions.
- Login page: the provider list is the operator's `sort_order` (server-side ordering,
  page preserves it), and a disabled door disappears without a console rebuild.

- **Remote-control session recording and playback** (2.4-8) — a session becomes a
  record, so the audit log can answer **who was controlling that machine at 14:00**
  with the screen and the acts in one view. Migration 17 (`session_recordings`) — the
  number the plan reserved for recordings, which 2.4-6 and 2.4-7 consumed as 15 and 16.
  - **The recording policy is the installation's**, not the participant's:
    `RECORD_SESSIONS=off|on|control-only` (**off is the default**), `RECORDINGS_DIR`,
    `RECORDING_RETENTION_DAYS` (30). An unparseable value is never read as an enabled
    policy: the boot log prints what it received and the server records nothing.
    `control_only` is accepted as the spelling people type and normalized.
  - **Frames come from the path the live session already uses.** `POST
    /api/sessions/:id/frame` relays the agent's JPEG to the console **and** appends it
    to the session's recording: a file with no re-encode step, no trailer to lose, and
    no ffmpeg in the product. `OWRS1` + length-prefixed records (kind, timestamp,
    length, payload), 25 MiB per frame, 2 GiB per recording, each record synced.
  - **A restart resumes the same recording** — the row names the file, the writer
    appends to it, `interruptions` increments and a `resume` marker states the break, so
    playback shows the minutes nobody recorded. A file cut mid-record reads with its
    earlier frames and says it is truncated.
  - **The toggle starts a recording; the product has no stop.**
    `POST /api/sessions/:id/recording` (audited `session.record_start`) is the
    technician's own request (`technician:self`) or an admin's order
    (`operator:ordered`); a session has one recording (unique index), and the only end a
    recording knows is its session's end. Under `off` the toggle refuses
    (`409 recording_disabled`): a technician cannot make an installation capture screens
    it declined to.
  - **Playback in the console** (`/recordings`): the manifest, the frame index with the
    time of each frame, frames served one at a time as `image/jpeg`, and **the input
    events the ledger recorded for that session laid over the frame they produced** —
    read from the audit ledger, not copied into a media file.
  - **Reading a recording is scoped and audited.** `admin` reads the installation;
    manager/technician read their tenants' recordings **and always their own** (the
    record of what you did does not disappear when your grants change); an out-of-scope
    recording answers the byte-identical `404` of a recording that never existed
    (2.3.1). Opening a recording writes `session.recording_play` naming the player;
    exporting writes `session.recording_fetch`. Frames are the playback stream and are
    not audited per frame.
  - **Retention runs on the sweep that already exists** (2.3-6's rule, applied to a
    second table): the metric retention manager prunes recordings past
    `retention_until` hourly — rows first, then the files they name — closes recordings
    the database says are open while no session produces frames, and reports
    `RecordingsDeleted` / `RecordingFiles` / `RecordingsClosed`.
  - **`session.input` is its own audit act.** 2.4-7 shipped
    `POST /api/sessions/:id/input` with the row labelled `session.start`; recording
    playback and "what did this person do in that session" both select on input, so the
    action is `session.input`. The audit registry now names it, and the frame route
    stays on the documented skip list.
  - Console: the live session panel and the sessions ledger state which sessions **are**
    being recorded (`Recording · 42 frames` / `Recorded` / `Not recorded`), the recording
    ledger names the unit, the account, the window and **why the recording exists**, and
    the playback panel shows the interruptions, the cap, the truncation and the checksum
    mismatch instead of smoothing them over.
  - **Not in 2.4-8** (stated, not silent): audio, cursor-shape fidelity, per-frame
    input capture (keystrokes are recorded as *types* in the ledger, never as text), and
    server-side backup of `RECORDINGS_DIR` (the directory is media storage; exports are
    audited).

### Fixed
- `auditOKOnce` / `auditRefusedOnce` marked the request as recorded **before** writing,
  so `recordAudit`'s duplicate-row guard swallowed the row: those helpers wrote nothing.
  Found by 2.4-2's login refusals, their first callers.
- A provider saved with **no issuer** (endpoints typed by hand, the pre-2.4 shape)
  lost the typed endpoints in 2.4-1's discovery refactor and authorized to an empty
  URL. The typed > discovered > preset precedence is now applied on both paths.
- `models.AlertThreshold.Enabled` carried a GORM `default:true`, so a rule saved
  **disabled** was stored enabled (GORM substitutes the column default for a
  zero-valued field on insert): the toggle lied. The default is gone and `Create`
  writes every field explicitly.
- `TestViewerSessionRelay`'s hop deadlines: the message polls were given a
  scheduler-aware 20 s budget, the binary-frame hops kept a hard 5 s. Under
  `go test -race ./...` with the 2.4-5 suite in the parallel load, a hop was late
  and the relay was correct; every hop in that test now shares one budget.
- **A refused webhook delivery was recorded once and never retried.** `ClaimPending`
  moves a row to `in_flight`; `markFailed` set the attempts, the peer's answer and the
  next retry time, but left the row `in_flight` — and `FindPending` only looks for
  `pending` rows. The documented 1m/2m/4m/8m backoff was therefore dead code in
  production, and 2.4-6's retry criterion (a channel that succeeds on attempt 2 is
  delivered with `attempts: 2`) made it visible. A failed send now returns to
  `pending` when a retry is scheduled, and ends `failed` when it is not.
- Failures that **retrying cannot fix** are recorded once, with the reason: a 4xx from
  an endpoint, a 5xx SMTP refusal (bad address, refused sender), a missing mail
  transport, a channel with no recipient, and STARTTLS required-but-not-offered. Their
  ledger row says `not retried`, rather than filling the ledger with identical
  refusals.
- `common.dismiss` was the label of three console buttons (the alert detail banner and
  the channel answer) and existed in **no locale file**, so the button rendered the
  literal key path `common.dismiss` in every language. `enable`/`disable` were used
  with no locale entries too. All three keys now exist in `en`/`de`/`fr`, and the i18n
  gate keeps them in lockstep.

---

## [2.3.0] - 2026-10-28

### Added
- **Clients (tenants) in the data model** — `Client` (name, slug, notes, paused),
  `Device.ClientID` (nullable, indexed), and the `user_clients` junction that
  decides what a non-admin account can see. Migration 8
  (`clients_and_user_client_scope`). Unassigned is `client_id IS NULL`: no row to
  create, rename, collide with, or delete; the console presents it as the
  `unassigned` sentinel for the one account that can see those units.
- **The scope resolver** (`server/api/scope.go`) — one place answers "may this
  caller see it", from the user row and the junction, never from a token claim.
  Every read that reaches a unit respects it, enforced server-side; a unit outside
  the caller's clients answers the **same body a unit that does not exist gets**,
  on the list, the detail and every action route. Bulk commands refuse
  out-of-scope targets and name them (`device_ids`, `update_ids`).
- **`X-Ourway-Client-Scope`** on every scoped response — the client ids the server
  applied, or `all`. "The technician cannot find the unit" is the support ticket
  this release is measured by, and the header is the checkable answer.
- **Client surface**: `GET/POST /api/clients`, `PUT/DELETE /api/clients/:id`,
  `PUT /api/clients/:id/devices`, `GET/PUT /api/clients/:id/users`,
  `GET /api/clients/:id/rollup` and `GET /api/clients/rollups` (the dashboard's
  per-tenant rows, one read, the same `rollupOf` arithmetic as the ledger).
- Audit ledger deep links: a unit's page opens *this unit's record* in the ledger
  (`GET /api/audit?target=<unit id>`), and your own Settings opens *my acts*
  (`?actor=<user id>`). The filters are URL state, so the landing URL is a link you
  can paste into a ticket, each arriving as a stated chip with a one-click clear. The
  tenant grant is applied on top of them, so a target outside your clients reads an
  empty ledger. The links are offered to the roles that can read the ledger —
  `admin` and `manager`.
- **Per-client enrollment tokens** (2.3-4c) — `client_enrollment_tokens`
  (migration 11), `GET/POST /api/clients/:id/tokens` and
  `DELETE /api/clients/:id/tokens/:tokenId` (admin-only, reads included), the
  Clients panel's token band, and registration accepting the token in the
  `X-Enrollment-Secret` header the agent already sends: the unit joins **that
  tenant**. `ENROLLMENT_SECRET` keeps its pre-2.3 meaning byte for byte — agents
  are unaffected. Stored as a **hash only** (no plaintext column, unlike
  `models.APIKey`) and returned once as `token_value`; `client.token_create` and
  `client.token_revoke` are audit rows naming the tenant and never the secret. A
  revoked, expired, or unknown token answers the identical 401, and a tenant's
  token cannot claim a unit that belongs to another tenant.
- **The audit ledger** — `audit_log` (migration 9) with actor, actor roles, action,
  target, tenant, source IP, details and the **outcome the caller received**,
  written by a `defer auditForAnswer` at the top of each mutating handler and by
  `auditRoleDenial` in the RBAC middlewares (which abort before any handler runs,
  so the viewer's refused delete is a row). `TestEveryMutatingRouteIsAudited`
  compares the registry against the live router and fails the build on a mutating
  route with no action and no written reason. Retention is
  `AUDIT_RETENTION_DAYS` (365), pruned by the metric retention sweep, with the
  counters printed on the page. `GET /api/audit` scopes like the fleet, filters by
  action/tenant/actor/date, caps `limit`, and exports CSV.
- **Console**: the Clients ledger (units, awake, alerts by severity, patch
  compliance, technicians, last seen; assignment and grants in the detail panel;
  deletion through `ConfirmSheet`), the Audit page (filters are URL state, CSV
  export, scope and retention stated), the tenant facet and column on Devices,
  Alerts, Patches, Sessions and Files (`?client=<slug>`, so a filtered view is a
  link), the tenant line on the unit record, per-tenant rows on the dashboard, and
  the scope stamp in the shift bar.
- **Tenancy at scale** (2.3-8): scoping is a statement over the caller's **clients**
  — `device_id IN (SELECT id FROM devices WHERE client_id IN (…))` — so a 2,000-unit
  tenant's list carries the same two bind parameters as a two-unit tenant's, and the
  scope resolves once per request. Migration 10
  (`tenant_scoped_read_indexes`) adds the composite indexes the semi-join walks;
  `TestPostgresScopedReadUsesTheIndexes` asks the Postgres planner to prove it, and
  three query-count tests fail the build if a check scales with the target count.
  Measured with `scripts/scale-tenancy.sh`: 10,000 units, technician scoped to
  2,000, 100 req/s, zero failures, scoped p95 124/64/137 ms against an unscoped
  baseline of 830/122/746 ms.
- **Tests**: the scoping matrix over every route × role, the bulk atomicity tests
  (a technician scoped to A deploying to B changes nothing and names the target),
  the 2.2 → 2.3 Postgres upgrade test with devices preserved, the
  `web/e2e/tenancy.spec.ts` flow (scoped sign-in → filtered ledger → unit → act →
  audit row), and the tenancy scale run.

### Changed
- **A technician's visible set shrinks.** That is the point of the release: an
  unassigned unit is visible to administrators and invisible to every other
  account, so an installation's fleet has to be assigned before non-admins see it.
  `docs/upgrade-guide.md` states it in its first section.
- Device writes never touch `device_key`. `DeviceStore.Update` omits the column:
  the Redis device cache answers a cached row with an empty key (the key is
  `json:"-"`), so a handler that loaded a unit and saved the whole row — tags,
  reboot, client assignment, the WS hub — used to blank the unit's credential,
  locking its agent out, and the next such write failed on the unique index as a
  500. Enrollment (`Create`) is the only place the key is minted.
- Agent-facing behaviour, the wire pairing and the installer are untouched: an
  agent's device simply carries a client assignment.

## [2.1.0] - 2026-10-26

### Changed
- **The web console is rebuilt as an MSP operating interface** ("fleet service
  decal sheet"): an ink nav rail grouped Monitor / Manage / Admin with printed
  keyboard hints, a shift bar answering link state, operator, UTC clock, rate
  limit and the next maintenance window, states as bordered stamps instead of
  coloured dots, tabular monospaced numerals wherever a figure is scanned,
  ruled field boxes, ledgers with guide words, and a per-device service record
  (last seen, next due, interval). No incumbent visual identity was preserved;
  `PRODUCT.md` and `DESIGN.md` carry the contract.
- **Agent self-update follows version ordering** (docs/versioning.md): both
  sides must parse as semver, the server must be strictly newer, stable agents
  ignore prereleases, and `OURWAY_UPDATE_CHANNEL=edge` opts a device into
  `main` builds. Previously any string difference triggered an update, so a
  per-commit server stamp re-updated the whole fleet on every push.
- **Images are published per channel**: tags publish `vX.Y.Z`, the floating
  `X.Y` and `latest`; `main` publishes `edge` and `main-<sha>` only. `latest`
  is now only ever a stable release. The server image is stamped with the
  release version instead of the commit sha.

### Added
- **Version scheme** (`docs/versioning.md`) with the git tag as the single
  source of version, `scripts/version.sh` as the one derivation, and
  `scripts/release.sh` to cut a release (refuses a dirty tree, a non-`main`
  branch, a HEAD that is not the pushed tip, a version not greater than the
  latest tag, and a red gate).
- Console build and server version are visible: the shift bar prints the
  server's stamped version from `GET /api/agent/version`, the same endpoint
  agents poll.
- **Console read throttle**: GET requests queue 4-at-a-time, a 429 becomes a
  named console state with a 60 s backoff printed in the shift bar, and
  polling pauses while the server's minute budget refills.
- Device detail charts seed from the server's recorded 24 h history, so a
  unit is legible before the first live metric sample arrives.
- Self-hosted Barlow, Archivo Narrow and JetBrains Mono subsets (SIL OFL 1.1,
  license text at `web/src/assets/fonts/OFL.txt`): the console makes no
  external font request.
- Device list view lives in the URL (`?view=table|cards`), so a grid view is
  linkable; card grid virtualizes past 20 devices.

### Fixed
- Dashboard mount fired 14 reads and burned the server's per-minute rate
  budget (429 on load).
- Ledger times rendered with `toLocaleString()` / `toLocaleDateString()`
  (US locale) instead of ISO day plus 24-hour UTC.
- Guide words reported a stale visible range in long lists.
- Panel heads pushed titles to the right edge.

## [2.0.0] - 2026-09-25

### Added
- **macOS Agent Support** (arm64 + x64) with native metrics collectors
- **Role-Based Access Control (RBAC)** with 4 built-in roles (Admin, Manager, Technician, Viewer)
- **WebRTC Remote Sessions** with view/control modes and capture quality settings
- **Software Patch Management** with scanning, policies, deployment, and rollback
- **File Transfer** with server-mediated push/pull and drag-and-drop upload
- **SSO Integration** (OAuth 2.0 / OpenID Connect) for Google, Microsoft, and Apple
- **API v2** endpoints for webhooks and API keys at `/api/v2/`, with rate limiting (100 req/min per user)
- **Webhook System** with 8 event types, retry logic, and delivery tracking
- **API Key Management** with scoped keys, rotation, and revocation
- **Alert Enhancements**: deduplication, acknowledgment, assignment
- **Dark Mode** with light/dark/system options
- **Responsive Mobile Layout** with hamburger menu
- **Keyboard Shortcuts** with chord-based navigation
- **Internationalization (i18n)** with English, German, and French support
- **Virtual Scrolling** for large device lists
- **Redis Integration** for rate limiting, caching, and WebSocket state sharing
- **Time-Series Metrics Storage** with retention policies
- **Load Testing** validated for 10,000+ device simulation

**API Key Management**
- Scoped API keys with `owk_` prefix (SHA-256 hashed storage)
- API key authentication via `X-API-Key` header or `Bearer owk_...` format
- Key scopes: read, write (configurable per key)
- Key expiration: 1h, 24h, 7d, 30d, 90d, or never
- Key rotation (generates new key, invalidates old)
- Key revocation
- Frontend UI in Settings page for key management
- API endpoints: create, list, get, update, delete, revoke, rotate
- 6 new API key tests

**Testing Enhancements**
- Webhook API tests (create, list, get, update, delete)
- User management API tests (list, update, delete)
- Total test count: 36 (up from 23)
- Test coverage: 22.3% (up from 19%)

### Phase 4.2 - Frontend Enhancements
**Dark Mode**
- Full dark mode support with light/dark/system options
- Theme persistence via localStorage
- System preference detection (prefers-color-scheme)
- Smooth theme transitions

**Responsive Layout**
- Mobile sidebar with hamburger menu toggle
- Responsive grids on Dashboard and Devices pages
- Responsive tables on Alerts and Users pages
- Mobile-friendly filter controls

**Keyboard Shortcuts**
- Chord-based navigation: g+h (Home), g+d (Devices), g+a (Alerts), g+f (Files), g+p (Patches), g+u (Users), g+s (Settings)
- Shortcut documentation in Settings page

**Accessibility (WCAG 2.1 AA)**
- Skip-to-content link
- Focus-visible indicators on all interactive elements
- ARIA labels on key elements
- Semantic HTML structure

**Internationalization**
- i18next framework with react-i18next
- 3 locales: English (en), German (de), French (fr)
- Language selector in Settings with persistence
- Browser language detection

**Performance**
- Virtual scrolling for large device lists (react-window Grid)
- Optimistic UI updates for alert acknowledgment and device actions

### Phase 3
**SSO Integration**
- OAuth 2.0 authentication with Google, Microsoft, and Apple
- OpenID Connect support
- Just-in-time (JIT) user provisioning on first SSO login
- SSO provider configuration API and admin UI
- SSO sign-in buttons on login page
- CSRF protection with state parameter

**API v2 & Webhooks**
- Versioned API endpoints for webhooks and API keys at `/api/v2/`
- Webhook system with 8 event types: device_registered, device_online, device_offline, alert_created, alert_resolved, patch_deployed, session_started, session_frame
- Webhook delivery with exponential backoff retry (max 5 attempts)
- Delivery tracking and history
- Webhook test delivery
- Webhook management API and admin UI
- Per-user rate limiting (100 requests/minute)

### Phase 1
**RBAC**
- Role-based access control system with 4 built-in roles (Admin, Manager, Technician, Viewer)
- JWT tokens now include user roles
- RBAC middleware for route protection
- Role and user management API endpoints
- New local users are automatically assigned the viewer role at registration

**macOS Agent Support**
- Complete macOS agent support (arm64 and x64)
- macOS-specific collectors: energy/battery, disk encryption (FileVault), software inventory
- Platform-specific collector injection system
- Docker build configuration for macOS agents

**Alert System Enhancements**
- Alert deduplication (5-minute configurable window)
- Alert acknowledgment and assignment
- Configurable thresholds per metric type
- New API endpoints: acknowledge, assign alerts

### Phase 2
**Remote Sessions**
- WebRTC-based remote screen sharing and control
- Platform-specific screen capture (Linux, macOS, Windows)
- Real-time remote input (mouse/keyboard)
- Quality controls (capture quality)
- View/control mode switching
- Session management API

**Software Patching**
- Automated update scanning via native package managers
- Platform support: apt/yum (Linux), softwareupdate/brew (macOS), Windows Update
- Patch policies with scheduling and batching
- Deployment management with progress tracking
- Reboot management
- Device and deployment rollback support
- API endpoints: scan, deploy, rollback, reboot

**File Transfer**
- Server-mediated file push and pull
- Progress reporting from agent
- Drag-and-drop file upload
- Transfer history with status
- Multipart file upload support
- API endpoints: upload, push, pull, download, status

**Testing**
- Unit tests for patching APIs (scan, deploy, rollback, policies)
- Unit tests for file transfer APIs (upload, status, transfers)
- 11 new API tests added

## [1.0.0] - 2026-09-24

### Added

**Server**
- REST API with JWT authentication (access tokens: 24h, refresh tokens: 7d)
- Device management endpoints (register, list, query, update, remove)
- Metrics collection and storage (CPU, memory, disk, network)
- Alert engine with configurable thresholds (CPU: 80/90%, RAM: 85/95%, Disk: 85/95%)
- Alert severity levels: info, warning, critical
- WebSocket hub for real-time communication (`/ws` endpoint)
- Health check endpoint
- PostgreSQL 16 database with GORM ORM
- Graceful shutdown handling
- Environment-based configuration

**Agent**
- Cross-platform support (Linux x64/arm64, Windows x64)
- Heartbeat monitoring (15s interval)
- Metrics collection (60s interval)
- Platform-specific collectors (procfs for Linux, WMI for Windows)
- Service installation (systemd on Linux, Windows service on Windows)
- Logging with file rotation
- Auto-reconnect with backoff
- Graceful shutdown

**CLI**
- `ourway-cli register` - Register a new device with the server
- `ourway-cli install` - Install the OurWay agent on this machine
- `ourway-cli uninstall` - Remove the OurWay agent
- `ourway-cli status` - Show the status of the OurWay agent
- `ourway-cli logs` - Show OurWay agent logs
- `ourway-cli version` - Display version information
- `--server` flag to select the server URL (default http://localhost:8081)

**Frontend**
- Device dashboard with real-time status
- Device details page with metrics charts
- Alerts page with filtering
- Authentication pages (login/register)
- User settings
- Built with React 18, TypeScript, and Vite

**Docker**
- Multi-stage Dockerfile for server
- Multi-stage Dockerfile for each agent platform (linux-x64, linux-arm64, windows-x64)
- Docker Compose configuration with PostgreSQL 16
- Build script for all Docker images

### Fixed
- (No fixes in initial release)

### Security
- JWT-based authentication
- Password hashing
- CORS configuration
- Secure cookie options for frontend
- TLS support for agent communication (configurable)

[unreleased]: https://github.com/your-org/ourway-rmm/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/your-org/ourway-rmm/releases/tag/v1.0.0
