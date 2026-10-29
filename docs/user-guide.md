# OurWay RMM — User Guide

A task-oriented guide to using the OurWay RMM dashboard.

---

## Table of Contents

- [Getting Started](#getting-started)
- [Dashboard](#dashboard)
- [Clients and Tenants](#clients-and-tenants)
- [Audit Log](#audit-log)
- [Device Management](#device-management)
- [Alerts](#alerts)
- [Alert Thresholds (2.4-5)](#alert-thresholds-24-5)
- [Remote Sessions](#remote-sessions)
- [Patch Management](#patch-management)
- [File Transfer](#file-transfer)
- [User Management](#user-management)
- [SSO Configuration](#sso-configuration)
- [SAML 2.0 Single Sign-On (2.4-7)](#saml-20-single-sign-on-24-7)
- [Notification Channels (2.4-6)](#notification-channels-24-6)
- [API Keys](#api-keys)
- [Settings](#settings)

---

## Getting Started

### Sign Up

1. Navigate to your OurWay instance (e.g., `http://localhost:3000`)
2. Click **Register** on the login page
3. Enter your username, email, and password
4. Click **Register** — you'll be logged in automatically

### Log In

1. Navigate to your OurWay instance
2. Enter your username and password
3. Click **Log In**
4. If you have SSO configured, you'll see SSO provider buttons

### First Steps

1. **Register a device**: On a target machine, run:
   ```bash
   curl -sL https://releases.ourway.io/agent/install.sh | bash -s \
     --server wss://ourway.example.com/ws \
     --key <device-key>
   ```
   (Add the `--register` flag to have the installer register the device with the server and obtain a key, or pass an existing key with `--key`.)

2. **View your device**: Once the agent connects, it appears on the Dashboard.

3. **View alerts**: Alerts are generated automatically when a device crosses a global threshold (CPU 80/90%, RAM 85/95%, disk 85/95%).

---

## Dashboard

The Dashboard provides an at-a-glance overview of your entire fleet.

### What You'll See

- **Device count**: Total, online, and offline devices
- **Alert count**: Unresolved alerts by severity
- **Device grid**: Quick status of all devices with real-time updates

### Real-Time Updates

The dashboard updates in real-time via WebSocket. You don't need to refresh the page — device status, metrics, and alerts update automatically.

### Per-Tenant Rows

The dashboard's "By tenant" block (2.3) lists every client this account can see,
with units awake, open alerts, patch compliance and last-seen, and each row opens
that tenant's filtered fleet. The numbers come from the same read the Clients
ledger computes, so a dashboard row and a ledger row cannot disagree.

---

## Clients and Tenants

A **client** is the tenant an MSP supports: its units, its technicians, and its
slice of the fleet. Clients are the axis of *who can see* — tags stay the axis of
*what a policy touches*.

Open it with **Clients** (`g c`).

### Creating a Client

1. **Clients** → **Add client**
2. Name it. The slug (the short name in URLs and filters) is derived from the name.
3. The new client owns no units yet, and the ledger says so.

A client can be **paused**: a paused tenant is not offered for assignment, and its
units keep their history.

### Assigning Units

Open the client's detail (**Open {{name}} detail**) and tick the units it owns, then
**Save units**.

- The list offers the units in your own fleet **plus** the unassigned pool — you
  can only hand over units you are allowed to see.
- A unit belongs to **exactly one** client: assigning it here removes it from the
  client it was in.
- Deleting a client leaves every unit intact and **unassigned**. It never deletes a
  unit.
- The rollup on the row — units, awake, alerts by severity, patch compliance,
  technicians, last seen — is the same arithmetic the device ledger does over the
  same rows.

### Enrolling a Unit Directly Into a Client (2.3-4c)

Assignment moves a unit that is already enrolled. A **token** skips the matching
game: mint one for the client, hand it to the technician running the installer, and
the machine that enrolls with it joins **that client**.

In the client's detail, under **Enrollment tokens**: name it (**Token label** —
"front desk image", "Q4 build"), choose a window (**Expiry**: 7 / 30 / 90 / 365
days, or no expiry), and press **New token**.

- Paste the value into the agent's enrollment secret, where the installation's
  secret would go. It is the same field: a **token places the unit in a tenant**,
  the installation secret enrolls it **unassigned** as before.
- The value is shown **once**. The server keeps only its hash, so there is no
  "show token" control — copy it when it appears, and mint a fresh one if it is
  lost. Minting is cheap; a lost token is revoked, not recovered.
- **Revoke** it when the job ends. Units already enrolled keep reporting and stay
  in the client; agents presenting the token are refused from the next request.
- A token cannot claim a machine that already belongs to another tenant. It claims
  an unassigned unit, and the tenant it names becomes that unit's owner.

Minting and revoking are `admin` acts, and both are written to the audit ledger
against the tenant — with the label, never the secret.

### Granting a Client to an Account

In the client's detail, tick the accounts under **access** and press **Save
access**. The grant list is non-admin accounts; granting a tenant is what makes
that account's fleet. The grant takes effect on the account's next request — no
restart, no re-login.

### What a Non-Admin Account Sees

- Administrators see every tenant and every unit. Everyone else sees **only the
  clients they are granted**, and unassigned units are invisible to them.
- A unit outside the account's clients is not findable: the unit's own URL answers
  the same response a unit that does not exist gets, on every route — detail,
  alerts, sessions, files, patch actions.
- Bulk commands refuse out-of-scope targets and **name them** (`device_ids`,
  `update_ids`). Every refusal is also an audit row.
- The shift bar prints the scope the server applied to the read, with the number of
  tenants it names. When a technician cannot find a unit, that stamp is the first
  thing to read: it is almost always a tenant they are not granted, not a missing
  unit.

### The Tenant Filter

Every ledger carries a **Filter by tenant** facet and a **Tenant** column:
Devices, Alerts, Patches, Sessions and Files, plus the per-tenant rows on the
dashboard. The filter is URL state — `?client=<slug>` — so a filtered view is a
link you can paste into a ticket, it survives a reload, and it composes with the
page's other filters. Administrators also get **Unassigned**, the bucket no tenant
owns (`?client=unassigned`); a scoped account is not offered it, because for them
that bucket is empty by definition.

---

## Audit Log

The audit log records every mutating act: who did it, to which tenant and unit,
and **what the server answered**. Open it with **Audit** (`g i`).

- **Administrators** read the whole installation. **Managers** read the rows of the
  clients they are granted. A **viewer** is refused the page: the log is an
  operator surface.
- **Refusals are rows.** A viewer pressing delete, a technician targeting another
  tenant's unit, a command that failed — each is recorded with its outcome
  (*done*, *refused*, *not found*, *failed*). A page full of *refused* entries
  after a permissions change is the log doing its job, not an outage.
- **Filters are URL state** — by action, by tenant, from a date. A filtered ledger
  is a link.
- **Two deep links.** A unit's page carries **This unit's record**, opening the
  ledger filtered to that machine; your own **Settings** page carries **My acts in
  the ledger**, filtered to you. Both arrive as an ordinary filter state, so the URL
  you land on is the URL you paste into a ticket, and clearing the chip is one click.
  They are shown to the roles that can read the ledger — **admin** and **manager** —
  and a technician's unit page does not offer them, because the ledger is a 403 for
  that role.
- **Export CSV** downloads the rows behind the current filters, through the
  authenticated request (a download link never carries the session token).
- **Retention**: rows older than `AUDIT_RETENTION_DAYS` (default 365) are pruned by
  the server's hourly sweep. The page prints the window, the last sweep and how
  many rows it removed.
- The log **starts empty on upgrade**: it is not a record of the past. History from
  before 2.3 is not recoverable.

Asking for a tenant the account is not granted answers an **empty** ledger — the
ledger's tenant list is a list of which tenants exist in the installation, and an
error naming one you may not read would leak exactly what the 404 rule closes. The
deep links obey the same rule: `?target=<unit>` is a filter over the rows the
caller may read, not a way past the grant.

---

## Device Management

### Viewing Devices

Navigate to **Devices** to see a list of all registered devices.

- **Online/Offline indicators**: Green = online, Red = offline
- **Last seen**: When the device last sent a heartbeat
- **OS and architecture**: Platform information

### Device Detail Page

Click a device to open its detail page.

#### Metrics Charts

- **CPU usage**: Line chart with historical data
- **Memory usage**: Line chart with used/total breakdown
- **Disk usage**: Per-partition usage bars
- **Network I/O**: Inbound/outbound traffic chart
- **Top processes**: Processes sorted by CPU usage

#### Remote Session

Click **Remote Session** on the device detail page to start a remote desktop
session. Frames stream to the browser over the server's frame relay; input travels
back over the device's WebSocket connection.

- **View mode**: Watch the screen without controlling
- **Control mode**: Click, type, and interact with the remote machine
- **Quality settings**: Adjust screen-capture quality (1-100%)

#### Device Actions

- **Reboot**: Restart the device
- **Delete**: Remove the device from OurWay

---

## Alerts

Navigate to **Alerts** to view all alerts across your fleet.

### Filtering

- **By severity**: Info, Warning, Critical
- **By status**: Open, Resolved
- **By device**: Filter to a specific device

### Alert Lifecycle

1. **Created**: Alert triggered by threshold breach
2. **Acknowledged**: A user has seen the alert
3. **Assigned**: Alert assigned to a specific user
4. **Resolved**: Alert cleared manually, or automatically when the metric returns to normal

### Actions

- **Acknowledge**: Mark as seen
- **Assign**: Assign to a user
- **Resolve**: Mark as resolved
- **View device**: Jump to the device detail page

### Alert Deduplication

Alerts are automatically deduplicated within a 5-minute window. If the same alert
condition persists, you won't get spam notifications. A rule can carry its own
window — see [Alert thresholds](#alert-thresholds-24-5) — so a lab unit that
reports every minute and a remote unit that reports every ten minutes are not one
alerting problem.

---

## Alert Thresholds (2.4-5)

Navigate to **Alert rules** (admin, manager) to set the levels a unit is measured
against. Before 2.4-5 the whole installation shared one set of numbers; a rule lets
a lab, a tenant, or a single noisy unit be measured on its own terms.

### Scopes, and which rule wins

| Scope | Applies to |
|-------|-----------|
| **Device** | the one unit you named |
| **Tag** | every unit carrying that tag |
| **Tenant** | every unit in that client |
| **Global** | every unit no narrower rule covers |

**Most specific wins, per metric.** A unit's own CPU rule answers its CPU even when
a tag rule covers the same unit's disk. There is no blending and no averaging, and
each metric is decided on its own — cpu 70/85 on the tenant, ram 50/65 on the unit.

A unit no rule covers is measured against the **built-in floor**: cpu 80/90,
ram 85/95, disk 85/95. Those are the numbers every installation alerted with before
rules existed, and a page with no rules behaves exactly as it did in v2.3.x.

### What the page shows

- **The rule ledger** — scope, what it points at by name, metric, warning/critical,
  the repeat window, and a toggle that turns a rule off without deleting it.
- **What applies to a unit** — pick a unit and each metric names the rule that
  answers it, then lists every rule that was in the running in the order they win,
  ending at the built-in floor. "Why did this unit alert at 62%?" is one read.
- **Ask a value** — type a percentage and get the message the alert would carry,
  and the scope that answered. Nothing is recorded. The wording is the engine's own,
  so the preview cannot promise a sentence the alert will not say.

### Rules, in the words the API enforces

- Warning must be **below** critical. A rule that warns above its critical can never
  alert, so it is refused.
- Percentages are 0–100, and the metric is cpu, memory or disk.
- One rule per (scope, metric). Adding a second answers which rule to edit.
- A saved rule's scope and metric are fixed. Moving a rule to another unit is a
  delete and an add, not an edit — so the rule you are looking at stays the rule it
  names.
- A rule whose repeat window is 0 uses the engine's 5 minutes.
- A disabled rule is skipped: the unit falls to the next rule, then the floor.
- Managers define rules for their own tenants' units and tags are admin-only,
  because a tag rule reaches every tenant's units.
- Creating, editing and deleting a rule is recorded in the audit ledger with the
  scope, the metric and the numbers.

### What a rule changes about an alert

An alert names the threshold that fired it — `CPU usage elevated: 62.0%
(threshold: 50%)` — and it auto-resolves when the metric drops back under **that**
rule's warning. A unit whose rule says 50 is considered recovered at 48; measuring
it against the installation's 80 would leave an alert open that your own rule says
is over.

---

## Remote Sessions

Remote sessions provide real-time screen sharing and control over any managed
device.

> **Platform reality check.** Screen capture works on Windows, Linux, and macOS,
> and input synthesis is implemented on all three. Two macOS conditions decide
> whether control on a Mac actually lands:
>
> - the unit must run the **cgo** agent build (`CGO_ENABLED=1` on a macOS
>   builder — a plain cross-compile cannot synthesize input), and
> - **OurWay Agent** must be granted **Accessibility** on that Mac: run
>   `ourway-agent --grant-input-permission`, then approve it in System Settings
>   → Privacy & Security → Accessibility.
>
> Without the grant, a macOS session streams and the pointer looks alive while
> every keystroke is discarded — so the unit view states the capability the agent
> reported (*Control ready*, *Needs Accessibility*, *Cannot control*, *Not
> reported*) and the session panel repeats it before you type. Windows machines
> can also be driven from the native viewer (`ourway://` launch) for
> multi-monitor and clipboard-aware control. See `docs/agent.md` for the grant,
> the fixed-path rule and code signing.

### Starting a Session

1. Open a device detail page
2. Click **Remote Session**
3. Choose **View** or **Control** mode
4. Adjust quality settings (optional)
5. Click **Start**

### Session Controls

- **Keyboard input**: Type directly into the remote session
- **Mouse input**: Click and drag to interact
- **End session**: Close the remote session

---

## Patch Management

Patch management automates software update scanning, approval, and deployment across your fleet.

### Scanning for Updates

1. Navigate to **Patches**
2. Select a device and click **Scan** (a fleet-wide scan also runs automatically every 24 hours)
3. Wait for scan results

### Viewing Available Updates

The Patches page shows:

- Devices with available updates
- Update details (package name, version, source)
- Total update count per device

### Creating Patch Policies

Click **New Policy** to create a patch policy:

- **Name**: Policy name
- **Scope**: All devices, by tag, or specific devices
- **Schedule**: Daily, weekly, or monthly
- **Auto-reboot**: Automatically reboot after deployment
- **Approval required**: Require manual approval before deploying
- **Batch size**: Maximum devices to patch simultaneously

### Deploying Patches

1. Select updates to deploy
2. Click **Deploy**
3. Monitor deployment progress
4. View results (success/failure per device)

### Deployment Rollback

There is no rollback button in the web UI. If a deployment fails, you can roll it back via the API:

```bash
curl -X POST https://ourway.example.com/api/patch/deployments/<deployment-id>/rollback \
-H "Authorization: Bearer <token>"
```

---

## File Transfer

Transfer files to and from managed devices.

### Pushing a File to a Device

1. Navigate to **Files**
2. Click **Push File**
3. Select a file from your computer
4. Choose the target device
5. Specify the destination path
6. Click **Send**

### Pulling a File from a Device

1. Navigate to **Files**
2. Click **Pull File**
3. Select the source device
4. Enter the file path on the device
5. Click **Download**

### Transfer History

The **Transfers** tab shows all recent file transfers with status, size, and progress.

---

## User Management

Manage users, roles, and permissions.

### Adding Users

1. Navigate to **Users**
2. Click **Add User**
3. Enter username, email, and password
4. Assign a role (Admin, Manager, Technician, Viewer)
5. Click **Save**

### Roles and Permissions

| Role | Can View | Can Modify | Can Manage Users | Can Configure |
|------|----------|------------|-----------------|---------------|
| Admin | Every tenant and every unit | Everything | Yes | Yes |
| Manager | The units of the clients granted to them, users, roles, alerts | Those units, alerts, user roles | No | No |
| Technician | The units of the clients granted to them, users, roles, alerts | Those units, alerts | No | No |
| Viewer | The units of the clients granted to them | Nothing | No | No |

"The units of the clients granted to them" is the whole of the scope rule (2.3):
the server answers every read by the account's granted clients, a unit outside
them answers as if it did not exist, and unassigned units belong to no
non-admin's fleet. Grants are edited on the client's detail page, not here.

### Managing Roles

1. Navigate to **Users** → **Roles**
2. Select a role or create a new custom role
3. Toggle permissions on/off
4. Click **Save**

---

## SSO Configuration

Configure single sign-on providers for your users.

### Adding an SSO Provider

1. Navigate to **Settings** → **SSO**
2. Click **Add Provider** — or press a preset (**Google**, **Microsoft**, **Apple**)
   to start from a pre-filled issuer
3. Name the provider (a slug: letters, digits, `-`, `_`, `.` — it is part of the
   callback URL), and give it a **client ID** and **client secret** from your IdP
4. Enter the **issuer** — the one field you have to know:
   - Keycloak: `https://idp.example/realms/<realm>`
   - Authentik: `https://idp.example/application/openid-client/<slug>`
   - Zitadel: `https://<instance>.zitadel.cloud`
   - Google: `https://accounts.google.com` (the `google` preset fills this in)
   - Microsoft (work and personal): `https://login.microsoftonline.com/common/v2.0`
5. Press **Ask the IdP what it publishes** before saving. The panel answers the
   endpoints, the signing algorithms, the scopes, the PKCE methods, the checks, and
   the **redirect URI you must register with the IdP**. Nothing is saved by that
   press, and it is recorded in the ledger as `sso.discover`.
6. Set the **identity contract** for this door (below), the **order**, and **Enabled**
7. **Save**

OurWay fetches `<issuer>/.well-known/openid-configuration` at save time and uses the
endpoints the IdP publishes. Any conformant OIDC provider works — the presets are a
convenience, not a whitelist. If the issuer does not answer, is not an OIDC provider,
describes a different issuer, or offers no authorization-code flow, the provider is
**not saved** and the answer names why (`discovery_unreachable`,
`discovery_document`, `issuer_mismatch`, `no_code_flow`).

The form asks which **protocol** the provider is — **OpenID Connect** (above) or
**SAML 2.0** ([below](#saml-20-single-sign-on-24-7)). The choice is fixed once the
provider is saved: a SAML door files its accounts by the NameID it asserts, an OIDC
door by the subject it signs.

Typing endpoints by hand still works as an override (a proxy, a private alias) through
the API: what you typed wins over what the IdP published, and discovery wins over a
preset.

### The provider ledger (2.4-4)

**Settings → SSO** lists the doors in the order the login page will show them, each
with its issuer, the identity contract that governs logins through it, its order, and
an enable toggle:

| Column | What it states |
|---|---|
| Provider | The name (the identity key stored on accounts that sign in through it — **not editable**) and the client ID |
| Issuer | The issuer the endpoints are discovered from, and whether PKCE is in force. A provider configured by hand shows `typed endpoints` |
| Identity contract | `pending` / `links by email`, the domain allow-list size (or `any domain`), `verified email`, `role mapping` |
| Order | `sort_order` — decides the login page's order |
| State | Enabled, with a toggle. **Disabling a door keeps the provider**: its accounts keep their identity key |
| Actions | **Test** (connection, as named states, audited `sso.test`), **Edit**, **Delete** (confirmed in the sheet) |

**Edit** opens the same form with the stored values, and never the client secret — the
field reads `Saved — type to replace it`, and the name field is locked with the reason.
An edit changes only what you touched: the enable toggle sends one field, so it cannot
wipe the domain allow-list you set earlier.

`PUT /api/sso/providers/:id` is the same shape for automation. Changing who the
installation trusts is recorded as `sso.update`.

### Test-connection

`POST /api/sso/providers/:id/test` (admin) reports, as named states, the things an
IdP admin can act on: discovery, the redirect URI this installation will send, the
JWKS endpoint, whether the IdP lists the `email` scope, whether it supports PKCE, and
the algorithm it signs `id_token`s with (OurWay verifies RS256). The probe is
recorded in the audit ledger as `sso.discover` / `sso.test`.

### Provider Details

You do not fill in endpoints. These are what discovery fetches for the three presets,
so you can check that your IdP is publishing the same shape:

#### Google

- **Issuer**: `https://accounts.google.com`
- **Scopes**: `openid profile email`

#### Microsoft

- **Issuer**: `https://login.microsoftonline.com/common/v2.0`
- **Scopes**: `openid profile email offline_access`

#### Apple

- **Issuer**: `https://appleid.apple.com`
- **Scopes**: `openid`

### Just-in-Time Provisioning

When a user logs in via SSO for the first time, an account is automatically created
with the user's email as the username, **at the `viewer` floor** — the role is
recorded on the account, and an admin assigns a higher one on the **Users** page.
The client secret never leaves the server: provider rows serialize without it, and
the login page's provider list carries only the name, label and order.

### What a login verifies

A provider configured by **issuer** is verified before it is trusted (2.4-2): the
`id_token` is checked against the keys that issuer publishes (`iss`, `aud` = this
provider's client id, `exp`, the signature), its `nonce` must be the one this login
sent, the code flow carries a **PKCE** challenge when the provider supports it, and the
userinfo call and the signed token must name the **same** subject. When a login is
refused, the browser gets a named reason (`subject_mismatch`,
`id_token_verification_failed`, `nonce_mismatch`, `missing_id_token`, …) and the audit
ledger records which rule refused it and the email the provider asserted.

A provider configured by **hand-typed endpoints** (no issuer) has no key set to verify
against. It works, and its login records say `id_token_verified: false`.

### The identity-linking contract (2.4-3)

**An SSO identity claims the account it already belongs to, never the account its
email names.** This is the rule that makes a permissive IdP safe to point at OurWay:
an IdP that lets users assert any email cannot take over an account, because asserting
an email is not what identifies an account here — the provider and the subject it
signs are.

Each provider carries four policy fields (set them on `POST /api/sso/providers`; the
console form for them is 2.4-4):

| Field | Effect |
|---|---|
| `allowed_domains` | Comma-separated list. An assertion outside it is refused (`domain_not_allowed`) and nothing is provisioned. Empty means the provider may assert any domain. |
| `require_verified_email` | When on, the provider's `email_verified` claim must be true, or the login is refused (`email_unverified`). |
| `link_policy` | `pending` (default): a new identity asserting an existing account's email becomes a **pending identity**. `link`: the operator's explicit choice to link, as 2.3 and earlier did — audited as `sso.link`. |
| `role_mapping` | JSON, e.g. `{"rmm-admins":"admin"}`. Empty (default) means the provider grants nothing and every SSO identity lands at the `viewer` floor. Configured, it **owns** the roles it names: the role is granted when the group claim asserts it and taken back when it stops asserting it. Roles outside the mapping are never touched. |

#### When an identity is pending

A provider asserted an email that belongs to an account that already exists. OurWay
created no session, changed no roles, and wrote a ledger row naming the attempt. On
**Users** (admin) the assertion appears with the account it points at, and you decide:

- **Approve as own account** — the identity becomes a new account of its own at the
  `viewer` floor. The account its email belongs to is untouched.
- **Merge into account** — the existing account now signs in through that provider,
  keeping its roles and client assignments. The provider's password no longer applies
  to it.
- **Nothing yet** — it stays pending. It cannot sign in, and it has no roles.

Both decisions are recorded in the ledger (`user.approve`, `user.merge`) with your
username, your roles at the time, the provider, the subject and the asserted email.

#### What the contract does not touch

Local password logins keep working, including for accounts with a pending identity
pointing at them. Existing sessions keep their scope. A manager never sees the pending
list. And an SSO login through a provider with no policy set completes — the contract
refuses identities that would take over an account, not SSO itself.

A SAML provider is governed by the same contract, with the same four fields — the
domain allow-list, the verified-email requirement (naming the attribute that states
it), `link_policy`, and the opt-in role mapping, which reads the group **attribute**
instead of the group claim. See [SAML 2.0 Single Sign-On](#saml-20-single-sign-on-24-7).

---

## SAML 2.0 Single Sign-On (2.4-7)

SAML 2.0 is the second door, for the IdPs that speak it — AD FS, a Shibboleth fed
campus, an enterprise SSO that publishes an entity ID rather than a discovery
document. It ships **after** generic OIDC deliberately, and it shares everything
that decides who may arrive: the [identity-linking contract](#the-identity-linking-contract-24-3)
is applied to a SAML assertion by the same code that applies it to an `id_token`.
One contract, two protocols. What SAML adds is how an identity is **proven**: an
assertion arrives on a posted form, and its authority is an XML signature OurWay
verifies against the certificate **you pinned** — not against a key set it fetched.

### Adding a SAML provider

1. **Settings** → **SSO** → **Add Provider**, and set the protocol to **SAML 2.0**.
2. Name the provider (a slug — it is a path segment of the ACS URL, and it is the
   identity key stored on every account that signs in through it, so it is not
   editable later).
3. Fill in the IdP's own identity, from its metadata document (**IdP entity ID**,
   **IdP sign-in URL**, **IdP logout URL**, **IdP signing certificate (PEM)**):
   - **IdP entity ID** — who must sign the assertion
   - **IdP sign-in URL** — where the browser is sent (`https`, published host, no port)
   - **IdP logout URL** — optional
   - **IdP signing certificate** — the PEM the IdP publishes. OurWay keeps it write-only
     and shows you its **SHA-256 fingerprint** and expiry, which is what you compare
     with what the IdP published
4. Choose the **NameID format** to ask for (`emailaddress` is the default and is what
   makes the asserted NameID the person's email) and, if your IdP uses non-standard
   attribute names, the **Email / Username / Display name / Groups attribute** fields,
   and — required when you turn on **Require a verified email** — the
   **Verified-email attribute**. SAML has no standard one and OurWay does not guess it.
5. **Sign our requests (generate a key)** when your IdP requires signed requests.
   OurWay makes the key pair; the private key never leaves the server and the public
   half is in the metadata. On a saved provider this replaces the key the IdP imported.
6. Set the identity contract, the order, and **Enabled**, as for any provider. A SAML
   provider needs **no client id**.
7. **Save**. A SAML provider that cannot validate an assertion is **not saved**, and
   the answer names the field: a missing entity ID, an `http` sign-in URL, a
   `localhost`/IP/port address, a certificate that does not parse or has already
   expired, an unknown NameID format, a verified-email requirement with no attribute
   named.

### The two things you hand the IdP admin

- **The metadata** — the **SP metadata** link on the provider's row
  (`GET /api/auth/sso/saml/<name>/metadata`), an `EntityDescriptor` naming this
  installation's **SP entity ID**, the ACS location, the NameID format,
  `WantAssertionsSigned="true"`, and the SP's signing certificate when it has one. It
  is public, like the login page, and it carries **no private key**.
- **The ACS URL** — `https://your-ourway/api/auth/sso/saml/<name>/acs`, the address
  shown on the row. The assertion's `Destination` must name it exactly.

The SP entity ID (`urn:ourway:<name>:<id>`) is **derived**, not typed: the assertion's
audience names this installation, and an operator who could type it could point two
installations' assertions at each other.

The ACS URL is built from **`WEB_URL`** (`WEB_URL` + `/api/auth/sso/saml/<name>/acs`),
with no per-provider override — the `Destination` and `Recipient` checks compare against
it exactly. Behind a reverse proxy, `WEB_URL` must be the URL the IdP and the browser
see, or every assertion is refused with `destination_mismatch`.

### What a SAML login verifies

The assertion is validated **as an identity** before any account is looked at, and the
identity is then run through the contract:

| Checked | Refused as |
|---|---|
| Signature over the assertion, against your pinned certificate (RSA-SHA256, exclusive canonicalization) | `assertion_unsigned`, `assertion_signature_invalid` |
| The response signature, when you require it | `response_signature_invalid` |
| The IdP's own answer (`StatusCode`) | `status_refused` |
| Issuer = your `idp_entity_id` | `issuer_mismatch` |
| `Destination` = this installation's ACS URL | `destination_mismatch` |
| `InResponseTo` = the request **this** login sent | `in_response_to_mismatch` |
| `AudienceRestriction` names this SP entity ID | `audience_mismatch` |
| `NotOnOrAfter` / `IssueInstant` (with 90 s of clock-skew tolerance) | `response_expired`, `response_not_yet_valid` |
| `SubjectConfirmationData.Recipient`, bearer confirmation, session window | `recipient_mismatch`, `subject_confirmation_method`, `session_start_in_future` |
| A second presentation of the same assertion | `assertion_replayed` |
| An assertion that asserts no email | `no_identity_claim` |

Then the contract decides, and it is the same list of refusals as OIDC —
`domain_not_allowed`, `email_unverified`, `identity_pending`, `account_pending` —
with the same split: **401** the assertion was not proven, **403** it was proven and
the contract refused it. Every refusal is a ledger row naming the reason; every
accepted login is audited as `auth.login` with `assertion_signature_verified: true`
and the protocol stated.

A completed login ends exactly like an OIDC login: a one-time `sso_code`, redeemed for
the token pair. Nothing a session is made from travels in a URL, and the posted
assertion is not logged.

### IdP-initiated login

A login that starts at the IdP (a portal tile, not a OurWay button) arrives at the ACS
with no state and is accepted on the audience, recipient, destination and expiry rules
alone. A login that **we** started must answer our request: an IdP-initiated document
that arrives with no `InResponseTo` while we are waiting for an answer to a request we
sent is refused.

### Test-connection for a SAML door

`POST /api/sso/providers/:id/test` asks the questions SAML can answer: the pinned
certificate's fingerprint and its expiry (with the days left — `fail` once it has
expired, the failure that arrives as a wave of `assertion_signature_invalid`), the
sign-in URL rule, the ACS URL and SP entity ID the IdP must be told about, whether the
metadata this installation serves parses, whether the SP signs its requests, and
whether a verified-email requirement has an attribute to read.

### What SAML in 2.4-7 does not include

Artifact binding (the IdP posts an `IDPList`/artifact reference instead of the
assertion), IdP-initiated single logout, and encrypted assertions. The POST binding,
the signing rules and the contract above are the shipped surface.

---

## Notification Channels (2.4-6)

A channel is how a fact about the fleet reaches a person. One ledger holds all four
kinds, one routing form applies to all of them, and one delivery history answers
"why didn't my phone buzz?" for every kind in the same shape.

| Kind | What arrives |
| --- | --- |
| **HTTP endpoint** | The event JSON, exactly as v2.3 sent it — same body, same headers, same HMAC signature. |
| **Slack** | A message with the alert's own message, its severity, and the unit, metric, value and threshold. |
| **Teams** | The same facts as a MessageCard, coloured by severity. |
| **Mailbox** | A localized mail (English, German, French), rendered from the same templates the console's locale files are checked against. |

A **webhook from an older release needs no work**: existing rows keep their URL,
events, custom headers, signing secret and enabled state, and deliver exactly as
before.

### Adding a channel

Open **Webhooks** in the sidebar (managers and admins) and click **Add channel**.
Pick the kind first: the form asks for a recipient list for a mailbox and an
endpoint for the other three, so the form cannot ask you for a column a mailbox
does not have.

Then the routing, which is the same for every kind:

- **Events** — the events this channel is asked about. Include *Test notification*
  if you want to be able to test the channel.
- **Severity floor** — `any event`, `warning and above`, `critical only`. Events
  below the floor are never routed here, and **no record is written for them**:
  a channel you did not ask about does not leave a line to explain.
- **Tenants** — empty reaches every tenant (an admin's choice); naming tenants
  narrows the channel to them, and a manager can only name tenants they administer.
- **Tags** — empty reaches every unit; named tags narrow it to units carrying one.

### Reading the ledger

Each row states its kind, its destination (a mailbox names the addresses, an
endpoint its URL), the tenants and floor it listens on, the events it subscribed to,
and whether it is open. Per row:

- **Disable / Enable** — opens or closes the line without touching its routing.
- **Send a test** — queues a test through the channel's own routing, and it appears
  in the delivery history like any other send. A channel that does not subscribe to
  the test event is refused, and the page says that, rather than reporting a send.
- **Delivery history** — event, when it was recorded, attempts, result, the peer's
  answer (and its status code), and when the next retry is scheduled. A refused
  delivery is retried at 1m, 2m, 4m, 8m; a destination that refused the request
  itself (a 4xx, a mailbox that does not exist, a mail server that is not
  configured) ends on the first attempt and says it was not retried.
- **Delete** — admin only, confirmed in a sheet that names the events that will stop
  arriving. The delivery history outlives the channel.

### Refusals you may see

| The console says | What to do |
| --- | --- |
| "url is required for a webhook, slack and teams channel" | The endpoint column is empty for a kind that posts to a URL. |
| "an email channel has no url: name the recipients in config.to" | The addresses belong in **Recipients**, not in the endpoint. |
| "this installation has no mail server: set `SMTP_HOST` and `SMTP_PORT`…" | Mail transport belongs to the installation; see [Configuration](configuration.md). |
| "only an admin may define a channel that reaches every tenant" | Name the tenants, or ask an admin. |
| "kind cannot be changed…" | A channel that delivers a different payload is a different channel: add the other kind beside it. |

### Mail transport

The server, its credentials, the sender address and STARTTLS are the installation's
configuration (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`,
`SMTP_FROM`, `SMTP_STARTTLS` — see [Configuration](configuration.md)). The channel
decides **who** it writes to, which is what makes "client A's criticals to the
on-call mailbox, everything else to the team chat" one row per destination.

The console states the mail transport the installation has — host, port and sender,
never credentials — under the ledger, because that is the fact that explains a
mailbox channel that cannot deliver.

---

## API Keys

Generate API keys for programmatic access to the OurWay API.

### Creating an API Key

1. Navigate to **Settings** → **API Keys**
2. Click **Add API Key**
3. Enter a name
4. Select scopes: read, write, or both
5. Choose expiration: never, 1h, 24h, 7d, 30d, 90d
6. Click **Create**
7. **Copy the key** — you'll need it for API requests

### Using an API Key

Include the key in API requests:

```bash
curl -H "X-API-Key: owk_<your-key>" https://ourway.example.com/api/devices
```

Or with the Bearer prefix:

```bash
curl -H "Authorization: Bearer owk_<your-key>" https://ourway.example.com/api/devices
```

### Managing API Keys

- **Rotate**: Generate a new key value (old key immediately invalid)
- **Revoke**: Deactivate the key
- **Delete**: Remove the key permanently

---

## Settings

### User Settings

1. Navigate to **Settings**
2. Update your profile information
3. Change your password

### Appearance

- **Theme**: Light, Dark, or System (auto-detect)
- **Language**: English, German, or French

### Keyboard Shortcuts

- `g + d`: Go to Devices
- `g + c`: Go to Clients
- `g + i`: Go to Audit
- `g + a`: Go to Alerts
- `g + f`: Go to Files
- `g + p`: Go to Patches
- `g + u`: Go to Users
- `g + s`: Go to Settings
- `g + h`: Go to Home

---

## Tips & Best Practices

### Monitoring

- Acknowledge alerts promptly to prevent alert fatigue
- Alert thresholds are global per metric (CPU, memory, disk) and apply to all devices

### Tenants (2.3)

- Assign a unit to a client at enrollment time, not later: an unassigned unit is
  invisible to every non-admin account, so "the technician can't see the machine"
  starts as an assignment gap, not a permissions one
- Grant clients to accounts, never units — there is no per-unit grant to keep
  straight, and the client detail page is where the fleet of an account is decided
- When a technician reports a missing unit, read the scope stamp in the shift bar
  first; it names the tenants the server applied to that read
- Paste the filtered URL (`?client=<slug>`) into the ticket: the tenant filter is
  URL state, so the colleague opening the link sees the same fleet

### Patching

- Test patches on a pilot group before fleet-wide deployment
- Use maintenance windows for reboot-required updates
- Monitor deployment success rates

### Security

- Use SSO for centralized identity management
- Enable MFA where possible
- Use API keys with minimal scopes
- Rotate API keys periodically
