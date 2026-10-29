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
- [Remote Sessions](#remote-sessions)
- [Patch Management](#patch-management)
- [File Transfer](#file-transfer)
- [User Management](#user-management)
- [SSO Configuration](#sso-configuration)
- [Webhooks](#webhooks)
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

Alerts are automatically deduplicated within a 5-minute window. If the same alert condition persists, you won't get spam notifications.

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
2. Click **Add Provider**
3. Name the provider (a slug: letters, digits, `-`, `_`, `.` — it is part of the
   callback URL), and give it a **client ID** and **client secret** from your IdP
4. Enter the **issuer** — the one field you have to know:
   - Keycloak: `https://idp.example/realms/<realm>`
   - Authentik: `https://idp.example/application/openid-client/<slug>`
   - Zitadel: `https://<instance>.zitadel.cloud`
   - Google: `https://accounts.google.com` (the `google` preset fills this in)
   - Microsoft (work and personal): `https://login.microsoftonline.com/common/v2.0`
5. Ask OurWay what the IdP publishes **before** saving: `POST
   /api/sso/providers/discover` (admin) answers the endpoints, the signing
   algorithms, the scopes, the PKCE methods, the document verbatim, and the
   **redirect URI you must register with the IdP**. The console surface for this is
   2.4-4; today the form offers the three presets, and any other IdP is added
   through the API.
6. Enable the provider, set its order, and **Save**

OurWay fetches `<issuer>/.well-known/openid-configuration` at save time and uses the
endpoints the IdP publishes. Any conformant OIDC provider works. If the issuer does
not answer, is not an OIDC provider, describes a different issuer, or offers no
authorization-code flow, the provider is **not saved** and the answer names why
(`discovery_unreachable`, `discovery_document`, `issuer_mismatch`, `no_code_flow`).

Typing endpoints by hand still works as an override (a proxy, a private alias): what
you typed wins over what the IdP published, and discovery wins over a preset.

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

---

## Webhooks

Configure webhooks to send event notifications to external systems.

### Creating a Webhook

1. Navigate to **Settings** → **Webhooks**
2. Click **Add Webhook**
3. Enter:
   - **Name**: Descriptive name
   - **URL**: Target webhook URL
   - **Events**: Select which events to subscribe to
   - **Headers**: Custom HTTP headers (optional)
4. Click **Save**

### Event Types

- `device_registered`: New device registered
- `device_online`: Device comes online
- `device_offline`: Device goes offline
- `alert_created`: New alert created
- `alert_resolved`: Alert resolved
- `patch_deployed`: Patch deployed to device
- `session_started`: Remote session started
- `session_frame`: Remote session video frame captured

### Testing a Webhook

Click **Test** on a webhook to send a sample event and verify it works.

### Delivery History

Click a webhook to view recent delivery history, including status, status codes, attempts, and response bodies.

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
