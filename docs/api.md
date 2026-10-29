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
- [Webhook Endpoints (API v2)](#webhook-endpoints-api-v2)
- [API Key Endpoints (API v2)](#api-key-endpoints-api-v2)
- [Health, Readiness and Version](#health-readiness-and-version)
- [WebSocket Endpoint](#websocket-endpoint)
- [Rate Limiting](#rate-limiting)
- [Webhook Payload Format](#webhook-payload-format)
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

---

### DELETE /api/sessions/:id

End a remote session.

**Response (200 OK):**

```json
{
  "status": "ended"
}
```

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
    { "id": "uuid", "type": "oidc", "name": "keycloak-corp", "label": "https://idp.corp/realms/ourway", "sort_order": 0 },
    { "id": "uuid", "type": "oidc", "name": "google", "label": "https://accounts.google.com", "sort_order": 1 }
  ]
}
```

Only `id`, `type`, `name`, `label` (the issuer), `sort_order` and any custom
`redirect_uri` are exposed. Endpoints, scopes and the client secret are not: this
list is public (2.4-1). Providers appear in `sort_order`, then name — the order on
the login page is a promise to the person logging in.

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

### SSO Callback
Handles the SSO provider's redirect back to OurWay: exchange the code (presenting the
PKCE verifier), read userinfo, **verify the `id_token`** against the provider's
published keys, require the two identity sources to agree, and provision the account
on first login (JIT). Then redirect to `/login?sso_code=…`.

```
GET /api/auth/sso/:provider/callback?code=xxx&state=yyy
```

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
    }
  ]
}
```

`client_secret` is never serialized (`json:"-"`), on any route. `discovered_at` is
null for a provider configured by hand-typed endpoints with no issuer.

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
for a proxy or a private alias — precedence is typed > discovered > preset):
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

Discovery runs at save time. A provider whose issuer does not answer, answers with
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

### Delete SSO Provider (admin)
```
DELETE /api/sso/providers/:id
```

---

## Webhook Endpoints (API v2)

### Create Webhook
```
POST /api/v2/webhooks
```

**Request:**
```json
{
  "name": "Slack notifications",
  "url": "https://hooks.slack.com/services/xxx",
  "events": ["alert_created", "alert_resolved"],
  "headers": { "X-Custom": "value" },
  "enabled": true
}
```

**Supported event types:**
- `device_registered` — new device registered
- `device_online` — device comes online
- `device_offline` — device goes offline
- `alert_created` — new alert created
- `alert_resolved` — alert resolved
- `patch_deployed` — patch deployed to device
- `session_started` — remote session started
- `session_frame` — remote session video frame received

### List Webhooks
```
GET /api/v2/webhooks
```

### Get Webhook
```
GET /api/v2/webhooks/:id
```

### Update Webhook
```
PUT /api/v2/webhooks/:id
```

### Delete Webhook
```
DELETE /api/v2/webhooks/:id
```

### Test Webhook
Sends a test event to the webhook.
```
POST /api/v2/webhooks/:id/test
```

### List Deliveries
Returns recent delivery history for a webhook.
```
GET /api/v2/webhooks/:id/deliveries
```

### Retry Delivery
Retries a failed webhook delivery.
```
POST /api/v2/webhooks/:id/deliveries/:delivery_id/retry
```

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

## Webhook Payload Format

All webhook events use a consistent JSON payload format:

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
