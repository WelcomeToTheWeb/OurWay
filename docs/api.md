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
- [Agent Endpoints](#agent-endpoints)
- [Alert Endpoints](#alert-endpoints)
- [Remote Session Endpoints](#remote-session-endpoints)
- [Patch Management Endpoints](#patch-management-endpoints)
- [File Transfer Endpoints](#file-transfer-endpoints)
- [Webhook Endpoints (API v2)](#webhook-endpoints-api-v2)
- [API Key Endpoints (API v2)](#api-key-endpoints-api-v2)
- [Health Endpoint](#health-endpoint)
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
- `500 Internal Server Error` — Server error

---

## Device Endpoints

All device endpoints require JWT authentication.

### GET /api/devices

List all registered devices.

**Headers:**

```
Authorization: Bearer <access-token>
```

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
      "created_at": "2026-01-10T09:00:00Z",
      "updated_at": "2026-01-15T10:30:45Z"
    }
  ]
}
```

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

All session endpoints require JWT authentication.

### POST /api/devices/:id/sessions

Start a remote session for a device. Returns a WebRTC offer.

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
    "status": "pending",
    "created_at": "2026-09-25T10:00:00Z"
  },
  "offer": "<SDP offer as a JSON string>"
}
```

---

### POST /api/sessions/:id/answer

Submit the browser's WebRTC answer.

**Request:**

```json
{
  "answer": "<SDP answer as a JSON string>"
}
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

---

### POST /api/sessions/:id/ice

Add an ICE candidate.

**Request:**

```json
{
  "candidate": "candidate:..."
}
```

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

---

### POST /api/sessions/:id/input

Send keyboard/mouse input to the remote session.

**Request:**

```json
{
  "type": "mouse",
  "payload": {
    "x": 100,
    "y": 200,
    "button": "left"
  }
}
```

`type` is `"key"` or `"mouse"`; `payload` carries the event details.

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

## Health Endpoint

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

---

## WebSocket Endpoint

### GET /ws

WebSocket endpoint for real-time communication. No HTTP request body.

**Query Parameters (required — one of the following):**

| Parameter | Type | Description |
|-----------|------|-------------|
| `token` | string | User JWT access token (for web dashboard) |
| `device_key` | string | Device API key (for agents) |

**Examples:**

```
# User (web dashboard)
ws://localhost:8080/ws?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

# Device (agent)
ws://localhost:8080/ws?device_key=a1b2c3d4-e5f6-7890-1234-567890abcdef
```

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
| `session_start` | `{ session_id: string, server_url: string }` | Start a remote session (begin screen capture) |
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

- **Limit:** 100 requests per minute per user, sustained (token bucket with a
  burst allowance of 10 requests)
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
    { "type": "oauth2", "name": "google" },
    { "type": "oauth2", "name": "microsoft" }
  ]
}
```

### Authorize (redirect)
Redirects the user to the SSO provider's authorization page.

```
GET /api/auth/sso/:provider/authorize
```

### SSO Callback
Handles the SSO provider's redirect back to OurWay. Exchanges the code for tokens, retrieves user info, provisions the user if needed (JIT), and redirects to the frontend with a JWT.

```
GET /api/auth/sso/:provider/callback?code=xxx&state=yyy
```

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
      "type": "oauth2",
      "name": "google",
      "client_id": "your-client-id",
      "auth_url": "https://accounts.google.com/o/oauth2/v2/auth",
      "token_url": "https://oauth2.googleapis.com/token",
      "user_info_url": "https://openidconnect.googleapis.com/v1/userinfo",
      "scope": "openid profile email",
      "enabled": true,
      "created_at": "2026-09-25T00:00:00Z"
    }
  ]
}
```

### Create/Update SSO Provider (admin)
Creates a new SSO provider or updates an existing one (by name).

```
POST /api/sso/providers
```

**Request:**
```json
{
  "type": "oauth2",
  "name": "google",
  "client_id": "your-client-id",
  "client_secret": "your-client-secret",
  "auth_url": "https://accounts.google.com/o/oauth2/v2/auth",
  "token_url": "https://oauth2.googleapis.com/token",
  "user_info_url": "https://openidconnect.googleapis.com/v1/userinfo",
  "scope": "openid profile email",
  "enabled": true
}
```

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
