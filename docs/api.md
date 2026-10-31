# OurWay RMM API Reference

Complete reference for the OurWay REST API and WebSocket endpoint.

**Base URL**: `http://localhost:8080` (default)
**Content-Type**: `application/json` (for all request/response bodies)

---

## Table of Contents

- [Authentication](#authentication)
  - [User Authentication (JWT)](#user-authentication-jwt)
  - [Device Authentication (API Key)](#device-authentication-api-key)
- [Authentication Endpoints](#authentication-endpoints)
- [SSO Endpoints](#sso-endpoints)
- [Device Endpoints](#device-endpoints)
- [Client Endpoints (API v2.3)](#client-endpoints-api-v23)
- [Metric Streaming Endpoints](#metric-streaming-endpoints)
- [Agent Endpoints](#agent-endpoints)
- [Alert Endpoints](#alert-endpoints)
- [Remote Session Endpoints](#remote-session-endpoints)
- [Patch Management Endpoints](#patch-management-endpoints)
- [File Transfer Endpoints](#file-transfer-endpoints)
- [Notification Channels (API v2)](#notification-channels-api-v2)
- [API Key Endpoints (API v2)](#api-key-endpoints-api-v2)
- [Health, Readiness and Version](#health-readiness-and-version)
- [WebSocket Endpoint](#websocket-endpoint)
- [Rate Limiting](#rate-limiting)
- [Channel Payload Format](#channel-payload-format)
- [Error Format](#error-format)
- [Data Models](#data-models)

---

## Authentication

OurWay supports two authentication methods:

1. **JWT Bearer Tokens** — for user-facing API calls (web dashboard, CLI)
2. **Device Keys** — for agent-to-server communication

### User Authentication (JWT)

Users authenticate with username and password to receive JWT tokens.

- **Access Token**: Valid for 24 hours, used for API requests
- **Refresh Token**: Valid for 7 days, used to obtain new access tokens

Include the access token in the `Authorization` header:

```
Authorization: Bearer <access-token>
```

### Device Authentication (API Key)

Agents authenticate using a device-specific key returned during registration.

Include the device key in the `X-Device-Key` header:

```
X-Device-Key: <device-key>
```

### User API Key Authentication

Users can create API keys for programmatic access to the API. Keys are prefixed with `owk_` and can be scoped and have expiration dates.

Include the API key using either the `X-API-Key` header or `Bearer` format:

```
X-API-Key: owk_<32-hex-chars>
```

Or:

```
Authorization: Bearer owk_<32-hex-chars>
```

API keys support the following scopes:
- `read` — read-only access to resources
- `write` — read and write access to resources

---

## Authentication Endpoints

### POST /api/auth/register

Register a new user account.

**Request:**

```json
{
  "username": "admin",
  "email": "admin@example.com",
  "password": "password123"
}
```

**Response (201 Created):**

```json
{
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "email": "admin@example.com",
    "created_at": "2026-01-15T10:30:00Z"
  }
}
```

**Error Responses:**

- `400 Bad Request` — Missing required fields
- `500 Internal Server Error` — Database error

---

### POST /api/auth/login

Authenticate a user and receive JWT tokens.

**Request:**

```json
{
  "username": "admin",
  "password": "password123"
}
```

**Response (200 OK):**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "email": "admin@example.com",
    "created_at": "2026-01-15T10:30:00Z"
  }
}
```

**Error Responses:**

- `401 Unauthorized` — Invalid credentials
- `403 Forbidden` — The credentials are correct and the account cannot sign in: a
  **pending SSO identity** (2.4-3) has no password and no roles, so it is refused by
  name (`{"error":"this identity is awaiting an administrator's decision","status":"pending"}`)
  rather than authenticating as an account with no roles
- `500 Internal Server Error` — Server error

---

## Pending SSO Identities (2.4-3)

When an identity provider asserts an email that belongs to an **existing** account,
OurWay does not link the two by itself. The assertion is stored as a *pending
identity* — no roles, no login — and an admin decides. Admin only: a tenant's manager
has no business seeing another tenant's account being claimed.

### List Pending Identities (admin)
```
GET /api/users/pending
```

**Response:**
```json
{
  "identities": [
    {
      "id": "uuid",
      "provider": "permissive-corp",
      "subject": "idp-subject-1",
      "email": "ops-admin@corp.example",
      "name": "Ops Admin",
      "email_verified": true,
      "created_at": "2026-11-20T10:00:00Z",
      "link_target_id": "uuid-of-the-existing-account",
      "link_target_username": "ops-admin",
      "link_target_roles": ["admin"]
    }
  ]
}
```

The list states the facts the decision needs: the asserted email, whether the provider
verified it, and the account (with its roles) the assertion points at.

### Approve A Pending Identity (admin)
```
POST /api/users/:id/approve
```

Activates the identity as **its own account at the `viewer` floor**. The account its
email belongs to is not touched. Audited as `user.approve`, naming the provider, the
subject, the asserted email, the role granted, and the account that was proposed as a
target. A second approval of the same identity answers `409`.

### Merge A Pending Identity Into Its Target (admin)
```
POST /api/users/:id/merge
```

Attaches the identity to the account its email belongs to (that account now signs in
through the provider, keeping its own roles and client assignments), and removes the
pending record. The account's roles are never changed by a merge. Audited as
`user.merge` with the target account as the row's target. Answers `409` when there is
no target — approve it as its own account instead.

Both acts are **logged decisions**: the ledger records the admin's user id, username
and roles as the actor, and the identity as the target.

---

## Device Endpoints

All device endpoints require JWT authentication.

### GET /api/devices

List the devices the caller may see.

**Headers:**

```
Authorization: Bearer <access-token>
```

**Scoping (v2.3.0+).** A caller's fleet is the devices of the clients they are
granted; `admin` is scope-all by role. Every scoped read answers
`X-OurWay-Client-Scope: <client-id>,<client-id>` (sorted, comma-joined), `all`
for admin, `none` for a caller with no grants. A caller with **no** grants gets
an empty list — "no rows granted" never reads as "sees everything" — and devices
with no client (unassigned) are visible to `admin` only.

`GET /api/devices/:id`, `/metrics` and `/metrics/history` follow the same scope,
and a device outside the caller's scope answers **404 with the body a
nonexistent device gets**, byte for byte. A 403 (or a 404 that says "not your
client") would tell the caller which unit IDs exist in someone else's client.

**Response (200 OK):**

```json
{
  "devices": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "web-server-01",
      "hostname": "web-server-01",
      "os": "linux",
      "arch": "amd64",
      "agent_version": "1.0.0",
      "status": "online",
      "last_seen": "2026-01-15T10:30:45Z",
      "public_ip": "203.0.113.50",
      "private_ip": "192.168.1.100",
      "input_capability": "granted",
      "capture_state": "ok",
      "created_at": "2026-01-10T09:00:00Z",
      "updated_at": "2026-01-15T10:30:45Z"
    }
  ]
}
```

`input_capability` (`granted` / `denied` / `unavailable` / `unknown`) and
`capture_state` (`ok` / `error` / `unknown`) are reported by the agent itself with
each metrics sample (v2.2.0+). They describe what remote control can do on the
unit: `denied` is a macOS unit whose Accessibility grant is missing, `unavailable`
is a build or tooling that cannot synthesize input, and `unknown` is an agent that
has never reported — never rendered as ready. Older agents send neither field and
the server keeps `unknown`, so this addition is wire-additive (MINOR).

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `500 Internal Server Error` — Database error

---

### GET /api/devices/:id

Get a single device by ID.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "device": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "web-server-01",
    "hostname": "web-server-01",
    "os": "linux",
    "arch": "amd64",
    "agent_version": "1.0.0",
    "status": "online",
    "last_seen": "2026-01-15T10:30:45Z",
    "public_ip": "203.0.113.50",
    "private_ip": "192.168.1.100",
    "created_at": "2026-01-10T09:00:00Z",
    "updated_at": "2026-01-15T10:30:45Z"
  }
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `404 Not Found` — Device not found

---

### GET /api/devices/:id/metrics

Get the most recent metrics for a device.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "metrics": {
    "device_id": "550e8400-e29b-41d4-a716-446655440000",
    "timestamp": "2026-09-25T16:30:00Z",
    "cpu": 45.2,
    "ram": 62.1,
    "ram_used": 25587584512,
    "ram_total": 41231686656,
    "disk_usage": 30.0,
    "disk_used": 150032358656,
    "disk_total": 500107862016,
    "net_in": 123456789,
    "net_out": 987654321,
    "uptime": 86400,
    "processes": 145
  }
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `404 Not Found` — No metrics found for device

---

### GET /api/devices/:id/metrics/history

Get historical metrics for a device within a time range.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Query Parameters:**

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `from` | string (RFC3339) | Start of time range (defaults to 24 hours ago) | `?from=2026-09-24T00:00:00Z` |
| `to` | string (RFC3339) | End of time range (defaults to now) | `?to=2026-09-25T00:00:00Z` |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "metrics": [
    {
      "device_id": "550e8400-e29b-41d4-a716-446655440000",
      "timestamp": "2026-09-25T16:00:00Z",
      "cpu": 42.1,
      "ram": 60.5,
      "ram_used": 24944670464,
      "ram_total": 41231686656,
      "disk_usage": 30.0,
      "disk_used": 150032358656,
      "disk_total": 500107862016,
      "net_in": 110000000,
      "net_out": 900000000,
      "uptime": 86400,
      "processes": 142
    },
    {
      "device_id": "550e8400-e29b-41d4-a716-446655440000",
      "timestamp": "2026-09-25T16:01:00Z",
      "cpu": 45.2,
      "ram": 62.1,
      "ram_used": 25587584512,
      "ram_total": 41231686656,
      "disk_usage": 30.0,
      "disk_used": 150032358656,
      "disk_total": 500107862016,
      "net_in": 123456789,
      "net_out": 987654321,
      "uptime": 86460,
      "processes": 145
    }
  ]
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `500 Internal Server Error` — Database error

---

### DELETE /api/devices/:id

Delete a device by ID.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `500 Internal Server Error` — Database error

---

## Client Endpoints (API v2.3)

A **client** is a tenant: the unit a device is assigned to, and the axis a
technician's scope is built from. These endpoints require JWT authentication.
Reads respect the caller's client scope; every write is `admin`-only, because
assigning a unit or granting a client is what moves fleets between people.

The scope rules are the device rules, applied to tenants: a client the caller is
not granted answers **404 with the body a nonexistent client gets**. Slugs
follow the tag normalization rule (lower-case, trimmed, spaces to dashes), so
`Acme  Widgets` and `acme-widgets` are one client, and a collision answers 409.

### GET /api/clients

List the clients the caller may see: every client for `admin`, and only the
granted rows for everyone else.

**Response (200 OK):**

```json
{
  "clients": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "Acme Widgets",
      "slug": "acme-widgets",
      "notes": "3 sites",
      "disabled": false,
      "created_at": "2026-10-27T10:00:00Z",
      "updated_at": "2026-10-27T10:00:00Z"
    }
  ]
}
```

### POST /api/clients · admin

Create a client. `name` is required; `slug` defaults to the normalized name.

- `201 Created` → `{"client": {…}}`
- `400 Bad Request` — `name` missing
- `409 Conflict` → `{"error":"a client with that slug already exists","slug":"acme-widgets"}`
- `403 Forbidden` — caller is not `admin`

### GET /api/clients/:id · PUT /api/clients/:id (admin) · DELETE /api/clients/:id (admin)

`PUT` takes the same body as `POST`, plus `disabled`.

`DELETE` **moves the client's devices to unassigned and never deletes them**;
the grants that named the client go with it. The response states how many units
moved, so the operator knows the fleet is in the unassigned pool and not gone:

```json
{"status": "ok", "devices_unassigned": 12}
```

### PUT /api/clients/:id/devices · admin

Assign a batch of units to a client: `{"device_ids": ["…"]}`. Targets are
validated **before any write**, and a target outside the caller's scope is
refused naming the ids:

```json
{"error": "one or more target devices are outside your client scope", "device_ids": ["…"]}
```

An empty `device_ids` empties the client — its units go to unassigned. One call,
two directions, so "assign" and "unassign" cannot drift into two definitions of
ownership.

### GET /api/clients/:id/rollup

One client's ledger row, computed from the same store reads the fleet ledgers
use — a rollup a technician cannot reproduce by filtering the fleet to that
client is a bug, not a rounding difference.

```json
{
  "rollup": {
    "client_id": "550e8400-e29b-41d4-a716-446655440001",
    "name": "Acme Widgets",
    "slug": "acme-widgets",
    "disabled": false,
    "units": 12,
    "units_online": 9,
    "last_seen": "2026-10-27T10:29:40Z",
    "alerts_open": 3,
    "alerts_critical": 1,
    "alerts_warning": 2,
    "alerts_info": 0,
    "patch_units": 12,
    "patch_compliant": 8,
    "patch_pending": 4,
    "patch_critical": 1,
    "patch_reboot_pending": 2,
    "technicians": 3
  }
}
```

### GET /api/clients/rollups

Every tenant the caller can see, as ledger rows, in **one** read — the dashboard's
per-client panel and the Clients ledger are the same numbers because the server
computes both with the same function over the same scoped reads. `technicians`
is a grouped count, not a query per tenant; `units_online` is computed on the
server so a client clock cannot change it.

Admin also receives the `unassigned` row — the units the 2.2 upgrade left
unplaced, which is the state every installation is in on the day tenancy
arrives. A scoped caller never sees it, because they have no such units.

```json
{
  "rollups": [
    { "client_id": "550e8400-e29b-41d4-a716-446655440001", "name": "Acme Widgets", "slug": "acme-widgets", "units": 12, "units_online": 9, "alerts_open": 3, "patch_compliant": 8, "patch_units": 12, "technicians": 3 }
  ],
  "scope": "all"
}
```

### GET /api/clients/:id/users · admin · PUT /api/clients/:id/users (admin)

The accounts granted this client. `PUT` replaces the set: `{"user_ids": ["…"]}`.

Grants are **rows**, not token claims, so a grant added here is visible on the
technician's next request, and a grant revoked here stops their access on their
next request — same access token, no logout, no re-login.

### Enrollment tokens · admin (API v2.3-4c)

A tenant's **enrollment credential**: an agent that registers presenting it
becomes a unit *of that tenant*. All three routes are `admin`-only, **reads
included** — a token is the ability to place a machine inside a customer's tenant,
and the grant model says nothing about who may hand that out.

| Route | Behaviour |
|---|---|
| `GET /api/clients/:id/tokens` | the tenant's tokens, newest first, revoked rows included. **No secret in any column.** |
| `POST /api/clients/:id/tokens` | mint one. Body `{"label": "front desk image", "expires_in_days": 30}`. `expires_in_days` is optional, and **omitting it means the token never expires**; a value must be 1–3650. `201` returns the token row plus `token_value` — the plaintext, **once**. |
| `DELETE /api/clients/:id/tokens/:tokenId` | revoke. A token id that is not this tenant's answers the 404 a nonexistent id gets. |

The server stores **only the SHA-256 hash** of the secret
(`models.ClientEnrollmentToken` has no plaintext column, unlike `models.APIKey`),
so no read returns a token value. Registration hashes what the agent presented and
walks the unique index on the hash — one indexed read per enrollment, no scan of
credentials.

`client.token_create` and `client.token_revoke` are audit rows on the tenant whose
credential they touched. **They never contain the secret**: the ledger is the
record of who minted what, not a second copy of the credential.

#### Enrollment accepts it in the header the agent already sends

`POST /api/agent/register` takes the credential in `X-Enrollment-Secret`, and both
kinds are accepted there:

- the installation's `ENROLLMENT_SECRET` → the unit joins **unassigned**: the
  pre-2.3 meaning, byte for byte, which is 2.3's wire contract (agents are
  unaffected);
- a tenant's token → the unit joins **that tenant**;
- anything else, including a revoked or expired token → the 401 the agent already
  knows. An expired token, a revoked token and a token that never existed answer
  **identically**, so the enrollment endpoint is not a probe for which tokens
  exist.

Re-registration (the installer run twice) keeps its C4 rule — the device key, the
installation secret, **or the token of the tenant the unit already belongs to**. A
tenant's token is not a skeleton key: it cannot claim a unit placed in another
tenant. It does claim an **unassigned** unit, moving it into the tenant its
credential names.

### GET /api/audit · admin, manager

The audit ledger: every mutating act, its actor, its target, and what the
server answered (2.3-6).

**Query params** (all optional): `action`, `client_id`, `actor`, `target`,
`from` and `to` (RFC3339), `limit` (default 200, capped at 1000), `offset`, and
`format=csv` for the export.

`target` is the record of **one unit** — the deep link from a unit view — and
`actor` is one account's acts, the deep link from the operator's own profile.
They are filters, not scope: the caller's tenant filter runs on top of them, so
a manager asking for a unit in a client they are not granted reads an empty
ledger, not the rows. `target` selects by record target rather than by tenant,
which is what "what happened to this machine" asks for — a unit that has moved
between clients keeps one continuous history. `TestAuditDeepLinkFiltersByTarget
AndActor` pins both halves.

**Response:**

```json
{
  "logs": [
    {
      "id": "5a0f…",
      "actor_user_id": "3f2c…",
      "actor_username": "dana",
      "actor_roles": "technician",
      "actor_provider": "local",
      "action": "device.reboot",
      "target_type": "device",
      "target_id": "9b1c…",
      "client_id": "77aa…",
      "outcome": "ok",
      "details": "{\"delay_seconds\":0}",
      "source_ip": "10.0.0.7",
      "created_at": "2026-10-27T10:30:00Z"
    }
  ],
  "total": 1,
  "scope": "client-1",
  "retention_days": 365,
  "limit": 200,
  "offset": 0,
  "retention": { "days": 365, "rows_pruned": 42, "last_run": "2026-10-27T09:00:00Z", "sweep_errored": false }
}
```

**Who sees what.** Admin reads the installation. A manager reads the rows of the
clients they are granted, resolved from `user_clients` on the request, exactly
like the fleet lists. A **viewer is refused (403)**: the ledger answers "who did
this to my fleet", and a viewer performs no acts worth reading about.

**`client_id` you are not granted answers an empty ledger**, not an error. The
ledger's client ids are a list of the tenants that exist in this installation,
and an error naming the one you may not read would leak exactly what the 404
rule for units closes.

**Refusals are rows.** `outcome` is `ok`, `refused`, `not_found` or `error`,
derived from the status the handler answered with. A technician's out-of-scope
deploy attempt, a viewer's delete, a login with the wrong password
(`auth.login_failed`) and a reboot the agent never received are all in the
table. A log of successes only is a log of what the installation allowed, and
the events an MSP reads the log for are the ones it did not.

**Actor fields are stored, not joined.** Username, roles and provider travel on
the row, because accounts and providers are deleted: a record that resolves to
"user 5f8… does not exist" is not a record.

**`client_id` on the row is the tenant at the time of the act**, denormalized
deliberately. Units move between clients; a query that joined through the
device's *current* client would answer "who did this to the fleet I manage
today", which is a live view wearing a timestamp, not a record.

**Retention is stated in the response.** `retention_days` is
`AUDIT_RETENTION_DAYS`, and `retention` reports the last sweep: rows it
removed, when it ran, and whether it failed. The same value prunes the table,
on the metric retention manager's hourly schedule.

### GET /api/audit/actions · admin, manager

The action names the server writes, so a filter UI is built from the
vocabulary the writers use rather than a list that falls behind it.

```json
{ "actions": ["device.delete", "patch.deploy", "auth.login_failed"], "retention_days": 365 }
```

---

## Agent Endpoints

Agent endpoints do not require JWT authentication. The `register` endpoint is public; `heartbeat` and `metrics` use device key authentication.

### POST /api/agent/register

Register a new device with the server.

**Request:**

```json
{
  "name": "my-workstation",
  "hostname": "my-workstation.local",
  "os": "linux",
  "arch": "amd64",
  "agent_version": "1.0.0",
  "public_ip": "203.0.113.50",
  "private_ip": "192.168.1.100"
}
```

**Response (201 Created):**

```json
{
  "device": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "my-workstation",
    "hostname": "my-workstation.local",
    "os": "linux",
    "arch": "amd64",
    "agent_version": "1.0.0",
    "status": "online",
    "last_seen": "2026-01-15T10:30:00Z",
    "public_ip": "203.0.113.50",
    "private_ip": "192.168.1.100",
    "created_at": "2026-01-15T10:30:00Z",
    "updated_at": "2026-01-15T10:30:00Z"
  },
  "device_key": "a1b2c3d4-e5f6-7890-1234-567890abcdef"
}
```

**Error Responses:**

- `400 Bad Request` — Missing required fields
- `500 Internal Server Error` — Database error

---

### POST /api/agent/heartbeat

Report agent liveness. Updates the device's `last_seen` timestamp and status.

**Headers:**

```
X-Device-Key: <device-key>
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid device key
- `500 Internal Server Error` — Database error

---

### POST /api/agent/metrics

Report system metrics for a device.

**Headers:**

```
X-Device-Key: <device-key>
Content-Type: application/json
```

**Request:**

```json
{
  "cpu": 45.2,
  "ram": 62.1,
  "ram_used": 25587584512,
  "ram_total": 41231686656,
  "disk_usage": 30.0,
  "disk_used": 150032358656,
  "disk_total": 500107862016,
  "net_in": 123456789,
  "net_out": 987654321,
  "uptime": 86400,
  "processes": 145
}
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid device key
- `400 Bad Request` — Invalid metrics data
- `500 Internal Server Error` — Database error

---

## Alert Endpoints

Alert endpoints require JWT authentication.

### GET /api/alerts

List alerts, with optional filtering.

"Open" means **unresolved** (`resolved = false`): acknowledged alerts are open,
because acknowledging is a sign-off and not a close. That is the definition every
console count uses — the dashboard tile, the ledger header and the count chips
are the same population (2.2-4, `server/store/alerts_open_test.go`).

**Headers:**

```
Authorization: Bearer <access-token>
```

**Query Parameters:**

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `severity` | string | Filter by severity (`info`, `warning`, `critical`) | `?severity=critical` |
| `resolved` | string | Filter by resolved status (`true`, `false`) | `?resolved=false` |
| `device_id` | UUID | Filter by device ID | `?device_id=550e8400-...` |

**Response (200 OK):**

```json
{
  "alerts": [
    {
      "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
      "device_id": "550e8400-e29b-41d4-a716-446655440000",
      "device_name": "web-server-01",
      "severity": "critical",
      "message": "CPU usage high: 95.2% (threshold: 90%)",
      "metric": "cpu",
      "value": 95.2,
      "threshold": 90,
      "resolved": false,
      "created_at": "2026-01-15T10:28:00Z"
    }
  ]
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `500 Internal Server Error` — Database error

---

### POST /api/alerts/:id/resolve

Mark an alert as resolved.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Alert ID |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

**Error Responses:**

- `401 Unauthorized` — Missing or invalid token
- `500 Internal Server Error` — Database error

---

## Threshold Rule Endpoints (v2.4-5)

Alert levels are rules, not a build-time constant. A rule names a **scope**
(`device` → `tag` → `client` → `global`), one **metric** (`cpu`, `ram`, `disk`), a
`warning` and a `critical`, and is measured **per metric, most specific wins**:
a unit's own rule answers its CPU even when a tag rule covers the same unit's
disk. A unit no rule covers is measured against the built-in floor — cpu 80/90,
ram 85/95, disk 85/95 — which is exactly what every installation alerted with
before rules existed.

All six routes require role `admin` or `manager`. Managers act inside their own
tenants: a device/client rule for a tenant they cannot see answers the body an
absent tenant gets (`404`), and `tag`/`global` rules — which reach every tenant's
units — are `403` with the role that may define them named. Reads carry
`X-OurWay-Client-Scope` like every other scoped read.

### GET /api/alerts/thresholds

Every rule, most specific first (device, tag, client, global), plus the floor.

```json
{
  "rules": [
    {
      "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
      "scope_type": "device",
      "scope_value": "550e8400-e29b-41d4-a716-446655440000",
      "scope_name": "web-server-01",
      "metric": "cpu",
      "warning": 50,
      "critical": 65,
      "dedup_window_seconds": 0,
      "enabled": true,
      "editable": true,
      "created_at": "2026-10-29T10:00:00Z",
      "updated_at": "2026-10-29T10:00:00Z"
    }
  ],
  "defaults": [
    { "metric": "cpu", "warning": 80, "critical": 90 },
    { "metric": "ram", "warning": 85, "critical": 95 },
    { "metric": "disk", "warning": 85, "critical": 95 }
  ]
}
```

`scope_name` names what the rule points at (the unit, the tenant, the tag; a
global rule names every unit), because a ledger of ids is unreadable. `editable`
is false for a rule the caller may not change — a manager reading a global rule
sees it, and cannot touch it.

### GET /api/alerts/thresholds/resolve?device=<id>

The precedence preview for one unit: per metric, the rule that answers, and the
whole chain in the order it wins — the rule that applies first, every other
matching rule by specificity, and the built-in floor last. This is the read that
answers "why did this unit alert at 62%?".

```json
{
  "device": { "id": "550e8400-…", "name": "web-server-01", "tags": ["prod"], "client_id": "550e8400-…" },
  "metrics": [
    {
      "metric": "cpu",
      "applies": { "scope_type": "device", "scope_value": "550e8400-…", "warning": 50, "critical": 65, "source": "rule", "rank": 4 },
      "candidates": [
        { "scope_type": "device", "warning": 50, "critical": 65, "rank": 4, "source": "rule" },
        { "scope_type": "tag", "scope_value": "prod", "warning": 60, "critical": 75, "rank": 3, "source": "rule" },
        { "scope_type": "client", "warning": 70, "critical": 85, "rank": 2, "source": "rule" },
        { "scope_type": "global", "warning": 80, "critical": 90, "rank": 1, "source": "built-in default" }
      ]
    }
  ]
}
```

### GET /api/alerts/thresholds/simulate?device=<id>&metric=cpu&value=72

What a value would do to a unit, **without recording anything**: the severity, the
threshold line the alert would carry, and the scope that answered. The message is
the engine's own wording, byte for byte — the console cannot show a sentence the
alert will not say.

```json
{
  "device_id": "550e8400-…",
  "metric": "cpu",
  "value": 72,
  "warning": 60,
  "critical": 75,
  "scope_type": "tag",
  "scope_value": "prod",
  "scope_name": "prod",
  "source": "rule",
  "would_fire": true,
  "severity": "warning",
  "threshold": 60,
  "message": "CPU usage elevated: 72.0% (threshold: 60%)"
}
```

### POST /api/alerts/thresholds · PUT /api/alerts/thresholds/:id · DELETE /api/alerts/thresholds/:id

Create, edit and remove rules. A create names `scope_type`, `scope_value` (empty
for `global`), `metric`, `warning`, `critical`, and optionally
`dedup_window_seconds` (0…86400; 0 uses the engine default of 5 minutes) and
`enabled`. An edit sends the fields it touches; **the scope and metric of a saved
rule are immutable** — retargeting the rule an operator is looking at is not an
edit, so a scope change answers `409` ("scope cannot be changed…") and the fix is
to delete the rule and add a new one.

Every write answers a named refusal, and every write that succeeds is audited
(`alert.threshold_create` / `alert.threshold_update` / `alert.threshold_delete`,
with the scope, the metric, and the numbers in the row):

| Situation | Answer |
|-----------|--------|
| `warning` not below `critical` | `400` — a rule that warns above its critical can never alert |
| percentages outside 0…100, or zero | `400` |
| metric other than `cpu`/`ram`/`disk` | `400` |
| `tag` rule naming no tag | `400` |
| device/client rule for an absent unit, or one outside the caller's tenants | `404` |
| `tag`/`global` rule from a manager | `403` naming the role that may define it |
| a second rule on the same scope **and** metric | `409` naming the rule to edit |
| scope change on an existing rule | `409` |

A rule the engine measures with is the rule the operator saved: the alert's
`threshold` field and the wording of its `message` come from the resolved rule, and
an alert auto-resolves when the metric drops back under **that** rule's warning —
not the installation's.

---

## Remote Session Endpoints

All session endpoints require JWT authentication, except
`POST /api/sessions/:id/frame`, which the agent and the native remote-control
binary authenticate with a per-session token or the device key.

There is **no WebRTC handshake**. Frames travel over HTTP, input travels over the
device WebSocket, and a session becomes `active` when the server receives its
first frame.

### POST /api/devices/:id/sessions

Start a remote session for a device. Requires role `admin`, `manager`, or
`technician`.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Headers:**

```
Authorization: Bearer <access-token>
```

**Response (200 OK):**

```json
{
  "session": {
    "id": "session-uuid",
    "device_id": "device-uuid",
    "user_id": "user-uuid",
    "status": "pending",
    "created_at": "2026-10-26T10:00:00Z"
  },
  "viewer_token": "opaque per-session viewer credential",
  "viewer_url": "ourway://session/session-uuid?server=https%3A%2F%2Fhost&token=...&device=..."
}
```

`viewer_token` and the session's remote-control token are returned **once**, at
session start — only their hashes are stored. `viewer_url` is the link the native
Windows viewer opens.

**Error Responses:**

- `403` — caller's role may not start sessions
- `404` — device not found
- `409` — device is not connected

---

### GET /api/sessions

List sessions. `admin` sees every active session; every other role sees only
their own (`ListActiveAll` for admin, `ListActiveByUser` otherwise).

**Response (200 OK):**

```json
{
  "sessions": [ { "id": "…", "device_id": "…", "status": "active" } ],
  "recordings": {
    "session-id": {
      "id": "recording-id",
      "status": "recording",
      "frame_count": 12,
      "bytes": 481203,
      "reason": "policy:control-only",
      "policy": "control-only",
      "started_at": "2026-10-30T14:00:00Z",
      "interruptions": 0,
      "dropped_frames": 0
    }
  },
  "recording_policy": "control-only"
}
```

2.4-8: `recordings` carries the recording of each session that has one, and
`recording_policy` is the installation's policy (`off`, `on`, `control-only`).
This is where the live session panel states plainly that a session **is** being
recorded. A session absent from `recordings` has no recording: under
`control-only` a view-only session is not recorded, and `off` records nothing.

---

### POST /api/sessions/:id/quality

Set capture quality for a session the caller owns.

**Request:**

```json
{
  "quality": 70
}
```

`quality` must be between **1 and 100**; anything outside that range is rejected
with `400`, not clamped. The device is notified so its capture loop honors it.

---

### POST /api/sessions/:id/frame

Uploads one captured frame. This is the **agent / native remote-control binary**
endpoint — it is not JWT-authenticated, and it is the only way a session ever
becomes `active`.

**Headers (one of):**

```
X-Session-Token: <per-session remote token>   # native remote-control binary
X-Device-Key: <device key>                    # agent
```

**Request body:** the captured frame as **raw JPEG** (bounded to 10 MB). The
server base64-encodes it when relaying to the browser.

Frames for a `pending` session mark it `active`; frames for a session that is no
longer active are rejected with `409`.

---

### POST /api/sessions/:id/input

Send keyboard/mouse input to the remote session.

**Request:**

```json
{
  "type": "mouse",
  "payload": {
    "event": "click",
    "x": 42.5,
    "y": 68.2,
    "button": "left"
  }
}
```

`type` is `"key"` or `"mouse"`; `payload` carries the event details. `x` and `y`
are **percentages of the captured frame (0–100)**, not pixels — the agent maps
them onto the display it captured, so a scaled viewer stays exact.

Input is synthesized on Windows and Linux. On macOS the agent logs the event and
the session stays view-only (`agent/session/input_darwin.go`).

**Response (200 OK):**

```json
{
  "status": "sent"
}
```

**Record:** `session.input` naming the unit, its tenant, the session, and the
input type. The `payload` is never in the record — keystrokes are not evidence,
and a log of what a technician typed at a machine is a second system to breach.
2.4-7 shipped this route's row as `session.start`, a second row naming the same
session start; 2.4-8 names the act `session.input`, which is what the playback
overlay and the ledger filter select on.

Under `RECORD_SESSIONS=control-only` this request is also the moment a session's
recording starts (see [Recording a session](#post-apisessionsidrecording)).

---

### DELETE /api/sessions/:id

End a remote session.

**Response (200 OK):**

```json
{
  "status": "ended"
}
```

2.4-8: ending the session ends its recording. The writer's final counters go to
the manifest row and the row gets its `ended_at`. This is the only way a
recording ends — the product has no stop.

---

### POST /api/sessions/:id/recording

Start recording this session (2.4-8). **There is no route that stops one:** a
participant who can end the installation's recording of their own session has a
recording that ends exactly when it stops helping them, which is the opposite of
why the audit log asks for one. The only end a recording knows is its session's
end.

**Who may start one:** the session's own operator (`reason: technician:self`),
and `admin` for a session inside their scope (`reason: operator:ordered`). A
caller who is neither gets the `404` a session that does not exist gets.

**Refusals:**

| Situation | Answer |
| --- | --- |
| `RECORD_SESSIONS=off` | `409` `recording_disabled` — a technician cannot make an installation capture screens it declined to |
| the session is `ended`/`error` | `409` `session_over` — a recording starts while the session is live |
| the recording row cannot be written | `409` `recording_unavailable` |
| the session belongs to another caller, or its unit is outside the caller's tenants | `404` (the same body as a session that never existed) |

Asking twice returns the **existing** recording: a session has one recording, by
the unique index on `session_id`, because two recordings of the same screen leave
the ledger unable to say which is the frame a person saw.

**Response (201 Created):** the recording manifest, as `GET /api/recordings/:id`
describes it.

**Record:** `session.record_start` naming the recording, the session, the reason,
and the policy — starting a recording begins capturing a person's screen.

---

### GET /api/sessions/:id/recording

The recording of one session, or its absence (`404`). The live session panel
reads this to state that a session **is** being recorded.

**Response (200 OK):** the manifest (`recording`, plus `playable`).

---

## Session recordings (2.4-8)

A recording is a manifest row plus a file of the frames the agent already posted
(`RECORDINGS_DIR/<recording-id>.seq`). The recording policy is the installation's
(`RECORD_SESSIONS`, see [Configuration](configuration.md)), and it is the reason
each recording exists: `on` records every session from its start,
`control-only` records a session from the moment someone sends control input,
`off` records nothing.

**Reading a recording is reading a person's screen**, so the doors are scoped the
way every tenant-facing ledger is (2.3.1), and the two acts that a recording is
used for are audited:

| Door | Who | Record |
| --- | --- | --- |
| `GET /api/recordings` | `admin`: the installation; manager/technician: their tenants' recordings **plus their own** | — |
| `GET /api/recordings/:id` | same scoping | **`session.recording_play`** naming the player and the recording |
| `GET /api/recordings/:id/frames` | same | — |
| `GET /api/recordings/:id/frames/:index` | same | — (the playback stream; the manifest row already named the act) |
| `GET /api/recordings/:id/events` | same | — |
| `GET /api/recordings/:id/manifest` | same | **`session.recording_fetch`** (`what: manifest`) |
| `GET /api/recordings/:id/file` | same | **`session.recording_fetch`** (`what: sequence`) |

A recording outside the caller's tenants answers the `404` a recording that never
existed answers, with the identical body. Two doors let a caller read a recording
that is not theirs to nobody: `admin` (the installation) and a manager/technician
whose tenant scope covers the unit. A person **always** reads their own
recordings, including on units they have since lost scope over — the record of
what you did does not become unaccountable when your grants change.

**Manifest fields:** `id`, `session_id`, `device_id`, `device_name`,
`client_id`, `user_id`, `username`, `status`, `reason`, `policy`, `started_at`,
`ended_at`, `frame_count`, `bytes`, `checksum`, `interruptions`,
`dropped_frames`, `retention_until`, `playable`.

- `status` is `recording` (the session's recording is open), `complete` (ended,
  file present) or `missing` (ended, file gone). A row with no `ended_at` is a
  recording being appended to.
- `reason` is `policy:on`, `policy:control-only`, `operator:ordered`, or
  `technician:self`: why this recording exists.
- `interruptions` counts server restarts that resumed the file, and the sequence
  carries a `resume` marker at each, so playback shows the break instead of
  hiding it.
- `dropped_frames` counts frames the writer refused — over the 10 MiB frame cap,
  or once the recording hit `MaxBytes` (2 GiB per recording by default). A
  recording that quietly lost frames is a recording an operator would read as
  complete.
- The live counters are a **sample**: the frame path writes the row every fifth
  frame. The read that shows a recording refreshes it from the writer, so the
  numbers a person is shown are exact; the session's end writes the final ones.

**Playback index** (`GET /api/recordings/:id`) adds `frames` (`{index, at}` per
frame), `markers`, `bytes_on_disk`, `checksum_on_disk`, and
`checksum_mismatch` when the file is not what the row says it is.
`GET /api/recordings/:id/frames` answers the index alone; a recording whose file
is gone answers `404` `recording_unreadable`. A frame index past the end answers
`404` `frame_not_found`.

**Input overlay** (`GET /api/recordings/:id/events`) answers the acts the ledger
recorded for that session inside the recording's own window, oldest first:
`session.input` (with its `input_type`), `session.start`, `session.quality`,
`session.end`, `session.record_start`. The events come from the audit ledger and
not from the recording file: the ledger is the installation's record of acts, it
is scoped, and it is pruned by a policy the operator reads. Input copied into a
media file would be a second copy of what a person did, kept for a different
reason and pruned on a different schedule.

**Retention.** `RECORDING_RETENTION_DAYS` (default 30) is written to the row as
`retention_until`. The metric retention manager prunes recordings past that date
on its hourly tick — rows first, then the files they name — and reports the
counts (`RecordingsDeleted`, `RecordingFiles`, `RecordingsClosed`) the same way it
reports the ledger's prune. It also closes recordings the database says are open
while no session is producing frames, so the console never shows a live recording
forever. A recording this server is appending to is never closed by the sweep.

**Restart.** `RECORDINGS_DIR/<recording-id>.seq` is an append-only sequence of
length-prefixed frames with no trailer to lose: a server that stops mid-session
leaves every earlier frame readable, and a restart appends to the same file
(`interruptions` +1, `resume` marker). A file cut mid-record is reported as
truncated. The sweep deletes files with their rows, and `RECORDINGS_DIR` is not
backed up by the server — export the manifest you need kept, and treat the
directory as media storage.

---

## Metric Streaming Endpoints

Streaming switches an agent from its 60 s metrics interval to a fast interval
while a device detail page is open. Requires JWT authentication.

### POST /api/devices/:id/stream

Ask a connected device to start streaming metrics.

**Request (body optional):**

```json
{
  "interval": 2
}
```

`interval` is in seconds, clamped to **1–60**; an empty body means 2.

**Response (200 OK):**

```json
{
  "status": "streaming",
  "interval": 2
}
```

**Error Responses:** `404` device not found · `503` device not connected

---

### POST /api/devices/:id/stream/stop

Revert the device to its normal metrics interval. An offline device simply
reverts on its next connect.

**Response (200 OK):**

```json
{
  "status": "streaming_stopped"
}
```

---

## Patch Management Endpoints

All patch endpoints require JWT authentication.

### POST /api/devices/:id/updates/scan

Trigger a software update scan on a device.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Response (200 OK):**

```json
{
  "status": "scan_started"
}
```

---

### GET /api/devices/:id/updates

List available software updates for a device.

**Response (200 OK):**

```json
{
  "updates": [
    {
      "id": "update-uuid",
      "device_id": "device-uuid",
      "source": "apt",
      "title": "package-name",
      "version": "1.0.0",
      "size_bytes": 10240,
      "status": "detected",
      "created_at": "2026-09-25T10:00:00Z"
    }
  ]
}
```

---

### POST /api/patch/deploy

Trigger a software update deployment.

**Request (optional):**

```json
{
  "device_ids": ["device-uuid-1", "device-uuid-2"]
}
```

If no `device_ids` are provided, deploys to all devices with approved updates.

**Response (200 OK):**

```json
{
  "deployment_id": "deployment-uuid",
  "status": "deploying"
}
```

---

### POST /api/patch/deployments/:id/rollback

Rollback a completed or failed deployment.

**Request (optional):**

```json
{
  "device_ids": ["device-uuid-1"]
}
```

If no `device_ids` are provided, rolls back on all devices.

**Response (200 OK):**

```json
{
  "status": "rollback_initiated",
  "deployment_id": "deployment-uuid",
  "devices": 2
}
```

---

### GET /api/patch/deployments

List all patch deployments.

**Response (200 OK):**

```json
{
  "deployments": [
    {
      "id": "deployment-uuid",
      "policy_id": "policy-uuid",
      "status": "completed",
      "devices_total": 5,
      "devices_success": 4,
      "devices_failed": 1,
      "started_at": "2026-09-25T10:00:00Z",
      "completed_at": "2026-09-25T10:05:00Z",
      "created_at": "2026-09-25T09:59:00Z"
    }
  ]
}
```

---

### GET /api/patch/policies

List all patch policies.

**Response (200 OK):**

```json
{
  "policies": [
    {
      "id": "policy-uuid",
      "name": "Weekly Patching",
      "scope": "all",
      "schedule": "weekly",
      "auto_reboot": true,
      "approval_required": false,
      "max_devices_per_batch": 10,
      "created_at": "2026-09-25T10:00:00Z"
    }
  ]
}
```

---

### POST /api/patch/policies

Create a new patch policy. Requires admin role.

**Request:**

```json
{
  "name": "Weekly Patching",
  "scope": "all",
  "schedule": "weekly",
  "auto_reboot": true,
  "approval_required": false,
  "max_devices_per_batch": 10
}
```

**Response (201 Created):**

```json
{
  "policy": {
    "id": "policy-uuid",
    "name": "Weekly Patching",
    "scope": "all",
    "schedule": "weekly",
    "auto_reboot": true,
    "approval_required": false,
    "max_devices_per_batch": 10,
    "created_at": "2026-09-25T10:00:00Z"
  }
}
```

---

### POST /api/devices/:id/reboot

Reboot a device.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | UUID | Device ID |

**Response (200 OK):**

```json
{
  "status": "reboot_requested"
}
```

---

## File Transfer Endpoints

File transfer endpoints require JWT authentication, except the agent-facing
endpoints under `/api/agent/files/`, which use device key authentication
(`X-Device-Key` header).

### POST /api/files/upload

Upload a file to the server.

**Request:** `multipart/form-data` with field `file`.

**Response (200 OK):**

```json
{
  "transfer_id": "transfer-uuid",
  "filename": "example.txt",
  "size_bytes": 1024
}
```

---

### POST /api/files/push

Push a file to a device.

**Request:**

```json
{
  "transfer_id": "transfer-uuid",
  "device_ids": ["device-uuid-1", "device-uuid-2"],
  "destination": "/tmp/"
}
```

- `transfer_id` (required): ID returned by `POST /api/files/upload`
- `device_ids` (required): one or more target device IDs
- `destination` (optional): destination directory on the device (defaults to `/home/`)

**Response (200 OK):**

```json
{
  "status": "push_initiated",
  "device_count": 2
}
```

---

### POST /api/files/pull

Pull a file from a device.

**Request:**

```json
{
  "device_id": "device-uuid",
  "source_path": "/var/log/syslog"
}
```

**Response (200 OK):**

```json
{
  "transfer_id": "transfer-uuid",
  "status": "pull_initiated"
}
```

---

### GET /api/files/transfers

List recent file transfers.

**Response (200 OK):**

```json
{
  "transfers": [
    {
      "id": "transfer-uuid",
      "device_id": "device-uuid",
      "uploader_id": "user-uuid",
      "filename": "example.txt",
      "directory": "",
      "source_path": "",
      "destination": "/tmp/example.txt",
      "size_bytes": 1024,
      "status": "completed",
      "direction": "push",
      "progress": 100,
      "error_message": "",
      "created_at": "2026-09-25T10:00:00Z",
      "updated_at": "2026-09-25T10:00:05Z",
      "completed_at": "2026-09-25T10:00:05Z"
    }
  ]
}
```

---

### GET /api/files/transfers/:id

Get details for a specific file transfer.

**Response (200 OK):**

```json
{
  "transfer": {
    "id": "transfer-uuid",
    "device_id": "device-uuid",
    "uploader_id": "user-uuid",
    "filename": "example.txt",
    "directory": "",
    "source_path": "",
    "destination": "/tmp/example.txt",
    "size_bytes": 1024,
    "status": "completed",
    "direction": "push",
    "progress": 100,
    "error_message": "",
    "created_at": "2026-09-25T10:00:00Z",
    "updated_at": "2026-09-25T10:00:05Z",
    "completed_at": "2026-09-25T10:00:05Z"
  }
}
```

---

### GET /api/files/:transfer_id/file

Download a retrieved file (e.g. after a completed pull transfer). Served with `Content-Disposition: attachment`.

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `transfer_id` | UUID | Transfer ID |

---

### GET /api/agent/files/:transfer_id/download

Agent downloads a file pushed to it.

**Headers:**

```
X-Device-Key: <device-key>
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `transfer_id` | UUID | Transfer ID |

Returns the file bytes (404 if the transfer does not exist or does not belong to the device).

---

### POST /api/agent/files/:transfer_id/upload

Agent uploads a file pulled from the device. `multipart/form-data` with field `file`. Marks the transfer as completed.

**Headers:**

```
X-Device-Key: <device-key>
```

**Response (200 OK):**

```json
{
  "status": "uploaded"
}
```

---

### POST /api/agent/files/status

Agent reports transfer status.

**Headers:**

```
X-Device-Key: <device-key>
```

**Request:**

```json
{
  "transfer_id": "transfer-uuid",
  "status": "transferring",
  "progress": 50,
  "error_message": ""
}
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

---

## Health, Readiness and Version

### GET /health

Check server health. No authentication required.

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

This endpoint is used for:

- Docker health checks
- Load balancer health checks
- Uptime monitoring

### GET /ready

Dependencies are reachable. No authentication required.

**Response (200 OK):**

```json
{
  "status": "ok",
  "db": "ok",
  "redis": "ok"
}
```

For Kubernetes readiness probes and load balancers. `redis` reports
`"not_configured"` when Redis is disabled.

### GET /api/agent/version

The stamped build, and the request budget. No authentication required.

**Response (200 OK):**

```json
{
  "version": "2.1.0",
  "git_commit": "5f03d77",
  "rate_limit_per_minute": 100,
  "rate_limit_burst": 10
}
```

Agents poll it for self-update (`docs/agent.md`); the console prints the version
and the budget from this one answer. The rate fields are
`RATE_LIMIT_PER_MINUTE` / `RATE_LIMIT_BURST` (`configuration.md`) — the *effective*
values, so a burst configured as `0` ("derive it") is reported as the derived
number.

---

## WebSocket Endpoint

### GET /ws

WebSocket endpoint for real-time communication. No HTTP request body.

**Authentication — `Sec-WebSocket-Protocol`, never a query parameter:**

Credentials travel in the WebSocket subprotocol list, as
`Sec-WebSocket-Protocol: ourway-auth, <credential>`. They are deliberately **not**
query parameters: a URL credential lands in access logs, proxy logs, and browser
history. A connection with no credential is rejected with `400`.

| Caller | Credential |
|--------|-----------|
| Web dashboard | user JWT access token |
| Agent | device key |

**Examples:**

```js
// User (web dashboard)
new WebSocket('ws://localhost:8080/ws', ['ourway-auth', token]);
```

```go
// Agent
dialer.Dial(ctx, wsURL, http.Header{"Sec-WebSocket-Protocol": []string{"ourway-auth", deviceKey}})
```

Allowed `Origin`s are restricted to the web URL plus `WS_ORIGINS`. `WS_ORIGINS`
is matched against the origin's **host[:port]**, so use `WS_ORIGINS=localhost:3000`:
a full URL (`http://localhost:3000`) rejects every browser connection with 403
while the REST API keeps working.

### WebSocket Message Types

All messages are JSON objects with a `type` field and an optional `payload` field.

#### User → Server

No message types are currently defined for user (browser) connections. User
connections are receive-only; messages sent by the client are ignored by the
server.

#### Server → User

| Type | Payload | Description |
|------|---------|-------------|
| `metrics` | `{ device_id: string, device: string, metrics: object }` | New metrics for a device (broadcast) |
| `heartbeat` | `{ device_id: string, device: string }` | Device heartbeat (broadcast) |
| `status` | `{ ... }` | Device status change, broadcast from the agent's `status` payload |
| `session_frame` | `{ session_id: string, device_id: string, data: string }` | Remote session video frame (base64 JPEG), sent only to the session owner |

#### Agent → Server

| Type | Payload | Description |
|------|---------|-------------|
| `heartbeat` | `{ device_key: string, timestamp: number }` | Agent liveness signal |
| `metrics` | `{ device_key: string, hostname: string, timestamp: number, data: object }` | System metrics |
| `status` | `{ device_key: string, timestamp: number, data: object }` | Status update |

#### Server → Agent

| Type | Payload | Description |
|------|---------|-------------|
| `session_start` | `{ session_id: string, server_url: string, remote_token: string }` | Start a remote session (begin screen capture) |
| `stream` | `{ interval: number }` | Switch to streaming metrics (1–60 s) |
| `stream_end` | `{}` | Revert to the normal metrics interval |
| `session_end` | `{ session_id: string }` | End the remote session |
| `session_quality` | `{ quality: number }` | Change session video quality (1–100) |
| `input` | `{ type: string, payload: object }` | Keyboard/mouse input from the session viewer |
| `file_push` | `{ transfer_id: string, filename: string, destination: string }` | Download a pushed file |
| `file_pull` | `{ transfer_id: string, source_path: string, filename: string }` | Upload a file from the given path |
| `scan_updates` | `{ device_id: string }` | Run a software update scan |
| `deploy_updates` | `{ device_id: string, deployment_id: string, updates: array }` | Deploy approved updates |
| `rollback_updates` | `{ deployment_id: string, device_id: string }` | Roll back a deployment |
| `reboot` | `{ delay_seconds: number }` (or null) | Reboot the device |

### WebSocket Example (JavaScript)

```javascript
// Connect as a user
const token = localStorage.getItem('ourway_access_token');
const ws = new WebSocket(`ws://localhost:8080/ws?token=${token}`);

ws.onopen = () => {
  console.log('Connected');
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log('Message type:', msg.type);

  switch (msg.type) {
    case 'metrics':
      console.log('Metrics from', msg.payload.device);
      break;
    case 'status':
      console.log('Status:', msg.payload);
      break;
  }
};

ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  console.log('Disconnected');
};
```

---

## Error Format

All error responses follow this format:

```json
{
  "error": "Human-readable error message"
}
```

### HTTP Status Codes

| Code | Meaning |
|------|---------|
| `200 OK` | Success |
| `201 Created` | Resource created |
| `400 Bad Request` | Invalid request (missing fields, bad data) |
| `401 Unauthorized` | Missing or invalid authentication |
| `404 Not Found` | Resource not found |
| `500 Internal Server Error` | Server error |

### Error Examples

**Missing authorization:**

```json
{
  "error": "missing authorization header"
}
```

**Invalid token:**

```json
{
  "error": "invalid token"
}
```

**Invalid credentials:**

```json
{
  "error": "invalid credentials"
}
```

**Device not found:**

```json
{
  "error": "device not found"
}
```

---

## Data Models

### Device

```json
{
  "id": "string (UUID)",
  "name": "string",
  "hostname": "string",
  "os": "string",
  "arch": "string",
  "agent_version": "string",
  "status": "string (online|offline)",
  "last_seen": "string (ISO 8601)",
  "public_ip": "string",
  "private_ip": "string",
  "created_at": "string (ISO 8601)",
  "updated_at": "string (ISO 8601)"
}
```

### Metrics

```json
{
  "cpu": "number (0-100)",
  "ram": "number (0-100)",
  "ram_used": "integer (bytes)",
  "ram_total": "integer (bytes)",
  "disk_usage": "number (0-100)",
  "disk_used": "integer (bytes)",
  "disk_total": "integer (bytes)",
  "net_in": "integer (bytes)",
  "net_out": "integer (bytes)",
  "uptime": "integer (seconds)",
  "processes": "integer"
}
```

### Alert

```json
{
  "id": "string (UUID)",
  "device_id": "string (UUID)",
  "device_name": "string",
  "severity": "string (info|warning|critical)",
  "message": "string",
  "metric": "string",
  "value": "number",
  "threshold": "number",
  "resolved": "boolean",
  "created_at": "string (ISO 8601)"
}
```

### User

```json
{
  "id": "string (UUID)",
  "username": "string",
  "email": "string",
  "created_at": "string (ISO 8601)"
}
```

---

## Rate Limiting

All protected (authenticated) endpoints are rate limited:

- **Limit:** `RATE_LIMIT_PER_MINUTE` requests per minute per user, sustained
  (default 100) — a token bucket with a burst allowance of `RATE_LIMIT_BURST`
  (default: a tenth of the limit). `GET /api/agent/version` reports both effective
  values, and the console prints the budget in its shift bar.
- **Scope:** Per user (identified by JWT) with a fallback to the client IP
- **Backend:** In-memory by default; Redis-backed when `REDIS_ENABLED=true`
- **Response:** `429 Too Many Requests` with `{"error": "rate limit exceeded"}`
  when the limit is exceeded

---

## API Versioning

Most routes live directly under `/api/` with no version prefix. The `/api/v2/` prefix is used only for the webhook and API key endpoints.

---

## API Testing with curl

### Register a user

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","email":"admin@example.com","password":"password123"}'
```

### Login

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password123"}'
```

### List devices

```bash
curl -X GET http://localhost:8080/api/devices \
  -H "Authorization: Bearer <access-token>"
```

### Register a device (as agent)

```bash
curl -X POST http://localhost:8080/api/agent/register \
  -H "Content-Type: application/json" \
  -d '{"name":"test-device","hostname":"test.local","os":"linux","arch":"amd64","agent_version":"1.0.0"}'
```

### Send heartbeat

```bash
curl -X POST http://localhost:8080/api/agent/heartbeat \
  -H "X-Device-Key: <device-key>"
```

### Send metrics

```bash
curl -X POST http://localhost:8080/api/agent/metrics \
  -H "X-Device-Key: <device-key>" \
  -H "Content-Type: application/json" \
  -d '{"cpu":45.2,"ram":62.1,"ram_used":25587584512,"ram_total":41231686656,"disk_usage":30.0,"disk_used":150032358656,"disk_total":500107862016,"net_in":123456789,"net_out":987654321,"uptime":86400,"processes":145}'
```

---

## SSO Endpoints

### List SSO Providers (public)
Returns all enabled SSO providers for the login page.

```
GET /api/auth/sso/providers
```

**Response:**
```json
{
  "providers": [
    { "id": "uuid", "type": "oidc", "protocol": "oidc", "name": "keycloak-corp", "label": "https://idp.corp/realms/ourway", "sort_order": 0 },
    { "id": "uuid", "type": "saml", "protocol": "saml", "name": "adfs-corp", "label": "adfs-corp", "sort_order": 1 }
  ]
}
```

Only `id`, `type`, `protocol`, `name`, `label`, `sort_order` and any custom
`redirect_uri` are exposed. Endpoints, scopes, IdP entity IDs, certificates and the
client secret are not: this list is public (2.4-1). Providers appear in `sort_order`,
then name — the order on the login page is a promise to the person logging in.

`label` is a **place**, not a machine identifier: the issuer for an OIDC provider,
and the provider's own name for a SAML one — an entity ID (`urn:ourway:…`) is not a
thing a person can check on a login page, and the login button renders `label`.
`protocol` names the door the button is (2.4-7) for a client that wants to say so.

### Authorize (redirect)
Redirects the user to the SSO provider's authorization page.

```
GET /api/auth/sso/:provider/authorize
```

Sets the one-shot `sso_state` cookie and redirects to the provider's published
`authorization_endpoint`. For an OIDC provider the redirect also carries **PKCE**
(`code_challenge`, `code_challenge_method=S256`, when the provider published a
challenge method) and a **nonce** the `id_token` must echo back. The verifier and the
nonce are held server-side against the state value and are single-use.

For a **SAML provider** (2.4-7) the same route starts the SAML Web SSO login: a
deflated (and, when the provider has an SP signing key, RSA-SHA256-signed)
`AuthnRequest` is sent to the provider's `idp_sso_url`, and the request ID is held
server-side against that same state record — it is what the assertion's
`InResponseTo` must name. The browser comes back to the assertion consumer service
below, not to the OIDC callback. A provider whose SP key cannot be used answers
**400** `assertion_unsigned` and **sends the browser nowhere**: an IdP that requires
signed requests refuses a request it cannot verify.

### SSO Callback
Handles the SSO provider's redirect back to OurWay: exchange the code (presenting the
PKCE verifier), read userinfo, **verify the `id_token`** against the provider's
published keys, require the two identity sources to agree, and provision the account
on first login (JIT). Then redirect to `/login?sso_code=…`.

```
GET /api/auth/sso/:provider/callback?code=xxx&state=yyy
```

A **SAML provider does not answer here**: its assertion arrives on a posted form at
the assertion consumer service, and this route says so — **400**
`{"reason": "saml_uses_acs", "error": "a SAML provider delivers its assertion to
/api/auth/sso/saml/<name>/acs"}` — rather than treating an assertion as a code.

Verification (2.4-2): signature against `jwks_uri` (RS256, rotation-tolerant), `iss`
= the configured issuer, `aud` = this provider's client id, `exp` required (60s
leeway), and the `nonce` = the one this login sent. A provider with **no issuer** has
no key set to verify against; its identity rests on the userinfo call, and the login's
audit row states that (`id_token_verified: false`).

Refusals answer with a named reason and write an `auth.login_failed` ledger row
naming the reason and the asserted identity. The status tells you which half refused:
**401** — the identity was not proven, so the contract never looked at an account
(2.4-2); **403** — the identity was proven and the linking contract (2.4-3) refused it:

| `reason` | What was refused |
|---|---|
| `code_exchange_failed` | The token endpoint refused the code (expired, replayed, verifier mismatch) |
| `missing_id_token` | The provider published a `jwks_uri` and returned no `id_token` |
| `id_token_verification_failed` | Signature, `iss`, `aud`, or `exp` did not check out |
| `nonce_mismatch` | The token was minted for a different login |
| `subject_mismatch` | userinfo and the signed token name different people |
| `userinfo_failed` / `no_identity_claim` | No usable identity was asserted |
| `state_mismatch` / `state_expired` | The CSRF state is not this login's (single-use) |
| `domain_not_allowed` (403) | The asserted email is outside the provider's `allowed_domains` |
| `email_unverified` (403) | The provider requires a verified email and `email_verified` was not true |
| `identity_pending` (403) | The email belongs to an existing account: a pending identity was created, no session issued (2.4-3) |
| `account_pending` (403) | This identity is already pending an admin's decision |

The contract (2.4-3) in one line: **an SSO identity claims the account it already
belongs to, never the account its email names.** An identity never inherits a role from
an email match — JIT provisioning lands at the `viewer` floor, roles come only from
`role_mapping` when the operator configured it for that provider, and a mapping owns
only the roles it names.

The access and refresh tokens **never appear in a URL** (C6). The console redeems
the one-time code below for them; a code is single-use and expires in 60 seconds.

### Exchange SSO Login Code (public)

```POST /api/auth/sso/exchange```

**Request:** `{ "code": "<sso_code>" }`

**Response:** `{ "access_token": "…", "refresh_token": "…", "expires_in": 3600 }`

Both protocols end at this route. A SAML login and an OIDC login produce the same
one-time code and the same redemption — the protocol decides how the identity is
proven, not how a session is issued.

### SAML Assertion Consumer Service (public)

The SAML half of the callback (2.4-7): the IdP's POST of a `Response` carrying a
signed assertion. This is the **only** door that accepts an assertion, and it is the
door an IdP-initiated login arrives at.

```
POST /api/auth/sso/saml/:provider/acs
```

**Request:** a browser form encoding with `SAMLResponse` (base64 XML, capped at 1 MiB
before anything is parsed). Only that field is read: `RelayState` is **ignored** on the
ACS (the SP sends its own entity ID as `RelayState` on the redirect, and the CSRF
binding is the `sso_state` cookie, so nothing a posted value names decides where a
login lands). Nothing in the posted body is logged, and no token ever appears in a URL
(C6).

**Success:** `302` to `/login?sso_code=…` — exactly the OIDC callback's answer.

The order is the security property, and it is the order the SAML Web SSO profile
prescribes: the document is validated **as an identity** (signature against the
certificate the operator pinned, issuer, destination, `InResponseTo`, expiry,
audience, recipient, subject confirmation, replay) and **only then** is the identity
run through the 2.4-3 contract. Nothing here trusts an assertion because it arrived.

Two of the verification rules are vendor shapes, found by verifying a Keycloak
assertion (2.4-0, `docs/user-guide.md`):

- The assertion is canonicalized **with the namespaces in scope from its enclosing
  document** — the `xmlns` declarations the `Response` made are part of what the
  signature covers, because exclusive canonicalization carries the in-scope prefixes of
  the subtree it canonicalizes. A verifier that hands the subtree only its own
  declarations digests bytes the IdP never signed.
- An `AuthnRequest`'s `Destination` is the **IdP's SSO location**, not this
  installation's ACS URL; its `AssertionConsumerServiceURL` is the ACS. `GET` signs the
  transmitted query string (percent-encoded), which is the bytes an IdP verifies.

An SP-initiated login must answer *this* login: the `sso_state` cookie names the
server-side state record, whose request ID the assertion's `InResponseTo` must match.
An **IdP-initiated** POST carries no state and is accepted on the audience, recipient,
destination and expiry rules alone — an IdP-initiated post that arrives as an answer
to a request we sent, without `InResponseTo`, is refused.

**Refusals** are named, audited as `auth.login_failed`, and provision no account:

| `reason` | Status | What was refused |
|---|---|---|
| `provider_not_saml` | 400 | The named provider is not a SAML door |
| `provider_disabled` | 400 | The door is closed |
| `assertion_unparseable` | 400 / 401 | 400 when the form carries no readable `SAMLResponse`; 401 when the value is not base64, not XML, over the size cap, or rooted at something other than `Response` |
| `state_expired` | 400 | The state cookie names a login this installation no longer has (state is single-use) |
| `status_refused` | 401 | The IdP's own `StatusCode` is not `Success` |
| `issuer_mismatch` | 401 | The assertion names an IdP other than this provider's `idp_entity_id` |
| `destination_mismatch` | 401 | The response is addressed to a location other than this installation's ACS URL |
| `in_response_to_mismatch` | 401 | The response answers a request this login did not send |
| `no_assertion` | 401 | The response carries no assertion |
| `assertion_unsigned` | 401 | The assertion is not signed — an unsigned assertion is an identity anyone can forge |
| `assertion_signature_invalid` | 401 | The signature does not verify against the pinned certificate (canonicalized `exc-c14n`, RSA-SHA256) |
| `response_signature_invalid` | 401 | The provider requires a signed **response** and it does not verify |
| `response_expired` / `response_not_yet_valid` | 401 | `NotOnOrAfter` / `IssueInstant` fall outside the window (90 s clock-skew tolerance) |
| `audience_mismatch` | 401 | `AudienceRestriction` does not name this installation's SP entity ID |
| `recipient_mismatch` | 401 | `SubjectConfirmationData.Recipient` is not this ACS URL |
| `subject_confirmation_method` | 401 | The subject is not confirmed `bearer` |
| `session_start_in_future` | 401 | `SessionNotOnOrAfter` precedes `AuthnStatement.SessionStart` |
| `assertion_replayed` | 401 | This assertion `ID` was already consumed (remembered for the assertion's whole accepted window) |
| `no_identity_claim` | 401 | The assertion asserts no usable identity |
| `domain_not_allowed`, `email_unverified`, `identity_pending`, `account_pending` | 403 | The assertion **was** proven and the 2.4-3 contract refused it — the same reasons, and the same split of 401/403, as the OIDC callback |

The status keeps 2.4-2's meaning: **401** the assertion was not proven, **403** it was
proven and the linking contract refused it. `idp_certificate` expiry is the failure an
operator would otherwise meet as a wave of `assertion_signature_invalid`, which is why
`POST /api/sso/providers/:id/test` reports it by name.

### SAML SP Metadata (public)

The document an operator uploads to the IdP so it knows this installation:

```
GET /api/auth/sso/saml/:provider/metadata
```

**Response:** `200`, `application/samlmetadata+xml` — an `EntityDescriptor` naming the
SP entity ID, `AuthnRequestsSigned`, `WantAssertionsSigned="true"`, the `NameIDFormat`,
the ACS `Location` and binding, and the SP's **signing certificate** when the provider
has one. Public, like the login page it belongs to: it carries no key material, ever.

The SP entity ID is derived from the provider's immutable name and id
(`urn:ourway:<name>:<id>`), not typed — the assertion's audience names this
installation, and an operator who could type it could point two installations'
assertions at each other.

The ACS URL is `WEB_URL` + `/api/auth/sso/saml/<name>/acs`. There is **no
per-provider override** for it (unlike OIDC's `redirect_uri`): the `Destination`
and `Recipient` checks compare against it exactly, so an installation behind a
reverse proxy must have `WEB_URL` set to the URL the IdP and the browser see, and
the metadata endpoint serves that URL. (`docs/configuration.md`.)

### List SSO Providers (admin)
Returns all SSO providers with configuration details.

```
GET /api/sso/providers
```

**Response:**
```json
{
  "providers": [
    {
      "id": "uuid",
      "type": "oidc",
      "name": "keycloak-corp",
      "client_id": "ourway",
      "issuer": "https://idp.corp/realms/ourway",
      "discovery_url": "https://idp.corp/realms/ourway/.well-known/openid-configuration",
      "jwks_uri": "https://idp.corp/realms/ourway/protocol/openid-connect/certs",
      "redirect_uri": "https://rmm.example/api/auth/sso/keycloak-corp/callback",
      "discovered_at": "2026-10-28T14:00:00Z",
      "pkce_supported": true,
      "auth_url": "https://idp.corp/realms/ourway/protocol/openid-connect/auth",
      "token_url": "https://idp.corp/realms/ourway/protocol/openid-connect/token",
      "user_info_url": "https://idp.corp/realms/ourway/protocol/openid-connect/userinfo",
      "scope": "openid profile email",
      "allowed_domains": "corp.example",
      "require_verified_email": true,
      "link_policy": "pending",
      "role_mapping": "",
      "sort_order": 0,
      "enabled": true,
      "created_at": "2026-10-28T00:00:00Z"
    },
    {
      "id": "uuid",
      "type": "saml",
      "name": "adfs-corp",
      "client_id": "",
      "issuer": "",
      "auth_url": "",
      "token_url": "",
      "user_info_url": "",
      "allowed_domains": "corp.example",
      "require_verified_email": true,
      "link_policy": "pending",
      "role_mapping": "",
      "sort_order": 2,
      "enabled": true,
      "created_at": "2026-10-29T00:00:00Z",
      "saml": {
        "idp_entity_id": "https://idp.corp/saml",
        "idp_sso_url": "https://idp.corp/saml/sso",
        "idp_logout_url": "https://idp.corp/saml/logout",
        "sp_entity_id": "urn:ourway:adfs-corp:6f2c1e90-…",
        "acs_url": "https://rmm.example/api/auth/sso/saml/adfs-corp/acs",
        "idp_certificate_sha256": "9C:1A:…:04",
        "idp_certificate_expires": "2027-06-01T00:00:00Z",
        "signs_requests": true,
        "require_response_signature": false,
        "nameid_format": "emailaddress",
        "attr_email": "",
        "attr_username": "username",
        "attr_name": "displayName",
        "attr_groups": "memberOf",
        "verified_email_attribute": "emailVerified"
      }
    }
  ]
}
```

`client_secret` is never serialized (`json:"-"`), on any route. `discovered_at` is
null for a provider configured by hand-typed endpoints with no issuer, and a SAML
provider never has one (there is no discovery document to fetch).

A SAML provider row carries a `saml` object; an OIDC provider row does not. The
**PEM the operator pasted is never sent back** — `idp_certificate`, `sp_cert_pem` and
`sp_key_pem` are `json:"-"`. What the console gets instead is the pinned certificate's
SHA-256 **fingerprint** (the value to compare with what the IdP published) and its
expiry (the fact that explains a wave of refused logins before it happens).

### Discover an Issuer (admin)

Fetches `<issuer>/.well-known/openid-configuration` and reports what the provider
publishes, **before any provider row exists** — the console shows this preview and
only writes the row when the operator saves (2.4-1).

```
POST /api/sso/providers/discover
```

**Request:**
```json
{
  "issuer": "https://idp.corp/realms/ourway",
  "discovery_url": "",
  "provider_name": "keycloak-corp",
  "redirect_uri": ""
}
```

**Response (200, `ok` false when the provider is unusable):**
```json
{
  "ok": true,
  "discovery": "ok",
  "issuer": "https://idp.corp/realms/ourway",
  "discovery_url": "https://idp.corp/realms/ourway/.well-known/openid-configuration",
  "redirect_uri": "https://rmm.example/api/auth/sso/keycloak-corp/callback",
  "endpoints": { "authorization": "…", "token": "…", "userinfo": "…", "jwks": "…" },
  "signing_algs": ["RS256"],
  "response_types": ["code"],
  "scopes": ["openid", "profile", "email"],
  "pkce_methods": ["S256"],
  "checks": [
    { "name": "discovery", "state": "pass", "detail": "https://idp.corp/realms/ourway" },
    { "name": "pkce", "state": "pass", "detail": "S256" },
    { "name": "email_scope", "state": "pass", "detail": "email" },
    { "name": "id_token_alg", "state": "pass", "detail": "RS256" },
    { "name": "userinfo", "state": "pass", "detail": "…" },
    { "name": "jwks", "state": "pass", "detail": "…" },
    { "name": "redirect_uri", "state": "pass", "detail": "…" }
  ],
  "document": { "…": "the document exactly as the provider published it" }
}
```

`discovery` is a named kind: `ok`, `discovery_unreachable`, `discovery_document`,
`issuer_mismatch`, `no_code_flow`. A malformed `issuer` (not an http(s) URL, or one
carrying credentials) answers **400** and is never fetched. The call is audited as
`sso.discover`.

### Create/Update SSO Provider (admin)
Creates a new SSO provider or updates an existing one (by name).

```
POST /api/sso/providers
```

**Request** (`issuer` is the field an operator fills in; the endpoints are the
provider's published ones. `auth_url`/`token_url`/`user_info_url` are an override,
for a proxy or a private alias — precedence is typed > discovered > preset). An
`oidc` provider:
```json
{
  "type": "oidc",
  "name": "keycloak-corp",
  "client_id": "ourway",
  "client_secret": "secret",
  "issuer": "https://idp.corp/realms/ourway",
  "scope": "openid profile email",
  "redirect_uri": "",
  "claims_email": "",
  "claims_username": "",
  "claims_name": "",
  "claims_groups": "",
  "allowed_domains": "corp.example",
  "require_verified_email": true,
  "link_policy": "pending",
  "role_mapping": "",
  "sort_order": 0,
  "enabled": true
}
```

A **SAML provider** (2.4-7) is a different door, not an OIDC provider with extra
fields — no issuer to discover, no client id, no token endpoint. It is the same route,
and the fields that decide whether an assertion can be validated:

```json
{
  "type": "saml",
  "name": "adfs-corp",
  "idp_entity_id": "https://idp.corp/saml",
  "idp_sso_url": "https://idp.corp/saml/sso",
  "idp_logout_url": "https://idp.corp/saml/logout",
  "idp_certificate": "-----BEGIN CERTIFICATE-----\n…",
  "generate_sp_key": true,
  "require_response_signature": false,
  "nameid_format": "emailaddress",
  "attr_email": "",
  "attr_username": "username",
  "attr_name": "displayName",
  "attr_groups": "memberOf",
  "verified_email_attribute": "emailVerified",
  "allowed_domains": "corp.example",
  "require_verified_email": true,
  "link_policy": "pending",
  "role_mapping": "{\"RMM Admins\":\"admin\"}",
  "sort_order": 2,
  "enabled": true
}
```

What the SAML door requires, refused at save with the sentence the console puts
beside the field — **no row is written**, exactly as a provider whose issuer does not
answer is not saved:

| Field | Rule |
|---|---|
| `idp_entity_id` | Required: it is who must sign the assertion |
| `idp_sso_url` | Required, **https only**, no port, no `localhost`, no `.local`, no IP literal, no loopback/link-local/metadata address. The browser is redirected there and the assertion is posted back from it — 2.4-1's issuer SSRF rule, applied to every endpoint a provider row names |
| `idp_logout_url` | Optional, same rule when set |
| `idp_certificate` | Required, must parse as a certificate with a validity window that has not closed |
| `nameid_format` | `emailaddress` (default), `persistent`, `transient` or `unspecified` |
| `attr_email`, `attr_username`, `attr_name`, `attr_groups` | The **attribute names the IdP writes** (defaults `email`, `username`, `displayName`, `memberOf`). An IdP asserting a `persistent` or `transient` NameID carries the address in an attribute, not in the NameID: `attr_email` is what makes such an assertion usable, and `persistent` with `attr_email` empty is refused as `no_identity_claim` |
| `require_response_signature` | When true, the **Response** must carry a signature that verifies against the pinned certificate, in addition to the assertion's — for IdPs that sign the assertion inside a signed response envelope. The assertion signature is required by the protocol and is not a setting: `WantAssertionsSigned="true"` is what the metadata publishes |
| `verified_email_attribute` | Required when `require_verified_email` is on: SAML has no standard "this email is verified" attribute and OurWay does not guess one |
| `generate_sp_key` | When true, OurWay generates the SP signing key pair (cert + key) for signed `AuthnRequest`s. The key is write-only, and a key with no certificate is refused |

`sp_entity_id` and the ACS URL are not accepted as input: they are derived from the
provider's immutable name and id and this installation's public URL, and reported by
`GET /api/sso/providers` and the metadata endpoint.

Discovery runs at save time **for OIDC providers**; a SAML provider skips it — its
endpoints are the IdP's entity ID and SSO location, which the rules above checked, and
fetching an issuer a SAML row does not have would refuse a correctly configured
provider. A SAML provider needs **no `client_id`**; every other type does.

Discovery of an OIDC issuer: a provider whose issuer does not answer, answers with
no usable document, describes a **different** issuer, or offers no authorization-code
flow answers **400** with `{"error": "provider not saved: …", "discovery": "<kind>"}`
and **writes no row**.

`google`, `microsoft` and `apple` are **presets of the same path**: they supply an
issuer, and their endpoints come from discovery like any other IdP. `name` must be
a slug (letters, digits, `-`, `_`, `.`), because it is a path segment of the
callback URL.

### Test an SSO Provider (admin)

Reports discovery, the redirect URI this installation will send, and the JWKS
endpoint as named states (`pass` / `warn` / `fail` / `skipped`) — not a 500.

```
POST /api/sso/providers/:id/test
```

Audited as `sso.test`. Admin only: probing the installation's identity provider is
not a tenant operation.

A **SAML provider** is probed with the questions SAML can answer — there is no
discovery document and no token endpoint to call — and the answers keep the same
named states:

| Check | What it states |
|---|---|
| `protocol` | `saml` |
| `idp_certificate` | The fingerprint, the expiry, and the days left — `fail` when it has expired, `warn` under 14 days |
| `idp_sso_url` | The https/port/host rule above, with the sentence it failed |
| `acs_url`, `sp_entity_id` | The values the IdP must be told about — the ACS URL is the one the assertion's `Destination` must name |
| `metadata` | That the metadata this installation serves parses, and its route |
| `signed_requests` | `warn` when the SP sends unsigned `AuthnRequest`s: an IdP that requires signed requests will refuse logins |
| `response_signature` | `warn` when the provider does not require the response signature |
| `verified_email` | `fail` when required and no attribute is configured to state it |

The OIDC checks are still answered, as `skipped`: a SAML row has no issuer to
discover and no `jwks_uri`, and `redirect_uri` reports the OIDC callback URL this
installation would use — which a SAML provider does not; `acs_url` is the value that
matters for it.

### Edit SSO Provider (admin)
```
PUT /api/sso/providers/:id
```

Edits a saved provider **in place** (2.4-4), so enable, order and policy changes are
not delete-and-recreate. Field by field: **a field the request omits is left alone**,
and an explicit empty string is a real state (no scope override, no domain allow-list,
no role mapping). The `client_secret` is sent only to replace it — an empty value keeps
the stored one, and no response ever contains it.

Discovery re-runs on an edit. A provider that no longer describes a reachable IdP is
refused with the same named kinds as at save (`discovery_unreachable`,
`discovery_document`, `issuer_mismatch`, `no_code_flow`), answering
`{"error":"provider not changed: …","discovery":"issuer_mismatch"}` and leaving the
stored row working. A SAML provider has no discovery to re-run; its SAML fields
(`idp_entity_id`, `idp_sso_url`, `idp_certificate`, the attribute names, the NameID
format) are all optional in the request body, so the rule that protects them is field
presence: **an edit that omits them changes nothing** — the console's enable toggle
cannot erase a pinned certificate, and the certificate is never read back to be
re-sent.

**The name is immutable** — `409 {"reason":"name_immutable"}`. It is the identity key
stored on every account that signed in through this provider (`users.provider`), so
renaming a provider is not editing a label; it would orphan those identities. Create a
new provider to change it.

**The protocol is immutable too** — `409 {"reason":"type_immutable"}` (2.4-7). A
SAML provider files its accounts by asserted **NameID** and an OIDC provider by
**subject**; a row switched from one to the other would answer logins with rules it
never had. (`oauth2`, `oauth` and `oidc` are spellings of the one flow, so they may
be exchanged.)

Audited as `sso.update` with name, type, issuer, enabled, link policy, allowed domains,
verified-email requirement, role mapping and order — never the client secret. For a
SAML provider, also `idp_entity_id`, `idp_sso_url`, `sp_entity_id`, the pinned
certificate's **fingerprint**, the response-signature requirement, the NameID format,
the verified-email attribute, and a boolean for "has an SP signing key": never the
certificate, never the key. Admin only, like every provider route.

### Delete SSO Provider (admin)
```
DELETE /api/sso/providers/:id
```

---

## Notification Channels (API v2)

A **channel** is one way this installation tells people what happened. Since 2.4-6
one row carries four kinds — `webhook` (the v2.3 HTTP endpoint), `slack`, `teams`
and `email` — with its routing (tenants, tags, severity floor) stated on the row,
and one delivery ledger behind every kind.

`/api/v2/webhooks` and `/api/v2/channels` address the **same ledger through the
same handlers**: the old name is what a v2.3 integration already calls, the new
name is what the row is now. Both are documented here; neither is a shim.

### Create Channel
```
POST /api/v2/channels          (also: POST /api/v2/webhooks)
```
**Access:** admin, manager. A manager must name the tenants the channel reaches.

**Request** (only `name`, `events` and the kind's destination are required; absent
fields keep their stored value on update):
```json
{
  "name": "On-call mailbox",
  "kind": "email",
  "events": ["alert_created", "patch_deployed"],
  "config": { "to": ["oncall@corp.example"] },
  "min_severity": "critical",
  "scope_clients": ["<client-id>"],
  "tags": ["prod"],
  "headers": { "X-Custom": "value" },
  "enabled": true
}
```

| Field | Meaning |
| --- | --- |
| `kind` | `webhook`, `slack`, `teams`, `email`. Immutable after creation. |
| `url` | The endpoint for `webhook`, `slack`, `teams`. An email channel takes none. |
| `config` | Per-kind settings. For `email`: `to` (addresses), optional `from`. |
| `min_severity` | `""` (any event), `info`, `warning`, `critical`. Events below the floor are never routed here. |
| `scope_clients` | Tenant ids the channel receives. Empty reaches every tenant — an admin-only choice. |
| `tags` | Tags the unit must carry. Empty reaches every unit. |
| `events` | The event types the channel subscribes to, `["alert_created", …]`. Include `test` to allow a test send. |

**Supported event types:** `device_registered`, `device_online`, `device_offline`,
`alert_created`, `alert_resolved`, `patch_deployed`, `patch_detected`,
`reboot_pending`, `session_started`, `test`.

**Response** — `201`, with the generated HMAC signing secret shown once:
```json
{ "webhook": { "id": "…", "kind": "email", "min_severity": "critical", … },
  "secret": "hex" }
```
No read path returns the secret again; the channel's JSON never carries it.

### List Channels
```
GET /api/v2/channels           (also: GET /api/v2/webhooks)
```
Returns every channel for an admin, and only the channels scoped to the caller's
tenants for a manager. The response also states the installation's mail transport,
which is a fact of the installation rather than of any channel, and which is what
explains an email channel that cannot deliver:
```json
{ "webhooks": [ … ],
  "mail": { "configured": true, "host": "mail.corp.example", "port": 587, "sender": "ourway@corp.example" } }
```
Credentials are never part of `mail`.

### Get · Update · Delete Channel
```
GET    /api/v2/channels/:id
PUT    /api/v2/channels/:id
DELETE /api/v2/channels/:id    (admin only)
```
A field that is absent from the request keeps its stored value, so the console's
enable toggle sends `{"enabled": false}` and cannot wipe the routing beside it.

### Test a Channel
```
POST /api/v2/channels/:id/test
```
Queues a `test` event **through the channel's own routing** and writes the same
delivery row every send writes, so the test is pointable in the ledger.

### Delivery History
```
GET /api/v2/channels/:id/deliveries
```
```json
{ "deliveries": [{
    "id": "…", "webhook_id": "…", "kind": "slack", "channel": "ops slack",
    "event": "alert_created", "status": "pending", "attempts": 2,
    "status_code": 500, "response_body": "endpoint refused",
    "next_retry_at": "2026-10-29T10:30:00Z", "created_at": "…" }] }
```
`status` is `pending`, `in_flight`, `delivered` or `failed`. `next_retry_at` is the
scheduled backoff (1m, 2m, 4m, 8m after the first failure). A permanent refusal —
a 4xx, an unreachable mail configuration — ends on the first attempt and says so
in `response_body`.

### Retry a Delivery
```
POST /api/v2/channels/:id/deliveries/:delivery_id/retry
```

### Refusals
Every refusal names the requirement in words, with a machine-readable `reason`, so
the console states what is missing instead of showing a code:

| Status | `reason` | Means |
| --- | --- | --- |
| `400` | `url_required` | `url is required for a webhook, slack and teams channel` |
| `400` | `url_not_used` | `an email channel has no url: name the recipients in config.to` |
| `400` | `kind_invalid` | `kind must be webhook, email, slack or teams` |
| `400` | `min_severity_invalid` | `min_severity must be empty, info, warning or critical` |
| `400` | `recipient_invalid` | `config.to must hold mail addresses` |
| `400` | `mail_not_configured` | no mail server on this installation (`SMTP_HOST`, `SMTP_PORT`) |
| `403` | `admin_only_scope` | only an admin may define a channel that reaches every tenant |
| `404` | `client_unavailable` | a tenant in `scope_clients` that does not exist, or that the caller does not administer |
| `409` | `kind_immutable` | `kind cannot be changed: a channel that delivers a different payload is a different channel` |
| `422` | `test_refused` | the channel does not subscribe to the `test` event |

A channel's kind is immutable because it decides what the payload means, and the
delivery history is read against that meaning: create the other kind as its own
channel.

### Routing semantics
Routing is applied **before** a delivery row is written. An event below the floor,
from a tenant the channel does not list, or from a unit carrying none of its tags
produces **no row at all** — not a failed row an operator has to explain. A unit
with no facts attached (an event carrying no unit) is matched by a channel with no
tag and no tenant restriction.

### Audit
`webhook.create`, `webhook.update`, `webhook.delete` and `webhook.test` record the
kind, destination, events, severity floor, tenant scope, tags, enabled state and —
for a mail channel — the addresses it was told to reach. The signing secret and the
SMTP credentials are never recorded.

---

## API Key Endpoints (API v2)

### Create API Key
```
POST /api/v2/api-keys
```

**Request:**
```json
{
  "name": "Production Key",
  "scopes": ["read", "write"],
  "expires": "never"
}
```

**Expires options:** `never`, `1h`, `24h`, `7d`, `30d`, `90d`

**Response (201 Created):**
```json
{
  "key": {
    "id": "uuid",
    "name": "Production Key",
    "key": "owk_2ffe6d5f9663f6c1c9e535ef8f3ab07e",
    "scopes": "[\"read\",\"write\"]",
    "last_used_at": null,
    "expires_at": null,
    "revoked_at": null,
    "rotated_at": null,
    "created_at": "2026-09-25T12:23:25.509659-04:00"
  }
}
```

> **Note:** The full API key is only shown at creation and rotation time. Store it securely.

### List API Keys
```
GET /api/v2/api-keys
```

### Get API Key
```
GET /api/v2/api-keys/:id
```

### Update API Key
```
PUT /api/v2/api-keys/:id
```

**Request:**
```json
{
  "name": "Updated Key Name",
  "scopes": ["read"]
}
```

### Delete API Key
```
DELETE /api/v2/api-keys/:id
```

### Revoke API Key
```
POST /api/v2/api-keys/:id/revoke
```

### Rotate API Key
Generates a new key value for the same key record. The old key value is immediately invalidated.
```
POST /api/v2/api-keys/:id/rotate
```

**Optional Request:**
```json
{
  "expires": "7d"
}
```

---

## Channel Payload Format

The `webhook` kind ships this event JSON exactly as v2.3 shipped it — a receiver
written against the older release keeps parsing the same body, signature included.
The other kinds render the same event for the place it arrives at.

```json
{
  "type": "alert_created",
  "timestamp": "2026-09-25T10:30:00Z",
  "data": {
    "alert_id": "uuid",
    "device_id": "uuid",
    "device": "my-server",
    "severity": "warning",
    "message": "CPU usage elevated: 85.0% (threshold: 80%)",
    "metric": "cpu",
    "value": 85.0
  }
}
```

Request headers include:
- `Content-Type: application/json`
- `X-OurWay-Event: <event_type>`
- `X-OurWay-Signature: sha256=<hex>` — HMAC-SHA256 of the payload under the
  webhook's secret (sent when the webhook has a signing secret)

### Slack and Teams
A `slack` channel posts the event as an attachment: the alert's own message as the
text, the event type and severity as fields, and the unit, metric, value and
threshold as a field line. A `teams` channel posts a MessageCard with the same
facts, coloured by severity.

### Mail
An `email` channel renders a localized subject and body from the templates in
`server/i18n/locales` (`en`, `de`, `fr`), checked by the same key-parity gate as
the console's own strings: a notification that reaches a German operator is
German, and the fields it names are the same fields in every locale. The rendered
mail carries the unit, severity, metric, value, threshold, tenant and the
engine's own message, plus the addresses it was sent to.

The mail server is the installation's (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`,
`SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_STARTTLS`); **who** it writes to is the
channel's (`config.to`), which is what makes "client A's criticals to the on-call
mailbox" one row.

The bytes a delivery attempt sent are what the ledger records, and a retry sends
**those bytes** — the record an operator reads is the record that went out.
