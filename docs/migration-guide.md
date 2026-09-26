# OurWay RMM — Migration Guide (1.x to 2.0)

This guide walks you through migrating from OurWay RMM 1.x to 2.0.

---

## Table of Contents

- [What's New in 2.0](#whats-new-in-20)
- [Breaking Changes](#breaking-changes)
- [System Requirements](#system-requirements)
- [Pre-Migration Checklist](#pre-migration-checklist)
- [Migration Steps](#migration-steps)
- [Database Migrations](#database-migrations)
- [Post-Migration Verification](#post-migration-verification)
- [Rollback](#rollback)

---

## What's New in 2.0

OurWay 2.0 transforms from a monitoring foundation into a complete RMM platform with:

### Phase 1: Foundation & Scale
- **macOS Agent**: Complete macOS support (arm64 + x64) with native metrics
- **RBAC**: Role-based access control (Admin, Manager, Technician, Viewer)
- **Alert Enhancements**: Deduplication, acknowledgment, assignment, custom thresholds

### Phase 2: Remote Management
- **Remote Sessions**: WebRTC-based screen sharing and control
- **Software Patching**: Automated update scanning, approval, and deployment
- **File Transfer**: Server-mediated file push and pull

### Phase 3: Authentication & Integration
- **SSO**: OAuth 2.0 and OpenID Connect (Google, Microsoft, Apple)
- **API v2**: Versioned API with rate limiting
- **Webhooks**: Event-driven integrations with retry logic

### Phase 4.1: Scalability
- **Redis Integration**: Rate limiting, caching, WebSocket state sharing
- **Time-Series Storage**: Optimized historical metrics storage
- **Load Testing**: Validated for 10,000+ device simulation

### Phase 4.2: Frontend Enhancements
- **Dark Mode**: Full dark theme support
- **Responsive Design**: Mobile-friendly layout
- **i18n**: English, German, French support
- **Keyboard Shortcuts**: Chord-based navigation
- **Virtual Scrolling**: Optimized for large device lists

---

## Breaking Changes

### 1. RBAC for All API Endpoints

In 1.x, any authenticated user could access all API endpoints. In 2.0, each endpoint has role-based access control:

- **GET /api/devices**: All roles
- **POST /api/devices/:id/sessions**: Technician+ roles
- **POST /api/sso/providers**: Admin only
- **POST /api/patch/policies**: Admin only

If you have custom integrations, ensure they use users with appropriate roles.

### 2. API Key Scopes

API keys created in 2.0 have scopes (read, write). Legacy keys from 1.x retain full access until rotated.

### 3. Database Schema Changes

The database schema has been extended. GORM will automatically run migrations on server startup, but:

- **New tables**: roles, permissions, role_assignments, sso_providers, webhooks, webhook_deliveries, api_keys, software_updates, patch_policies, patch_deployments, file_transfers
- **Modified tables**: users (+ provider, provider_id, sso_attributes), devices (+ group_id)

### 4. Alert Engine Behavior

- Alerts now include deduplication (5-minute window)
- Alerts can be acknowledged and assigned
- Custom per-device thresholds are now supported

### 5. Default JWT Secret

The default JWT secret is still `ourway-secret-key`, but 2.0 will warn at startup if you haven't changed it. Existing tokens remain valid.

---

## System Requirements

### Server

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4 cores |
| RAM | 2 GB | 4 GB |
| Disk | 10 GB | 50 GB (for metrics history) |
| PostgreSQL | 14 | 16 |
| Redis | 6.0 (optional) | 7.x |

### Agent

| Component | Minimum |
|-----------|---------|
| OS | Windows 10+, macOS 10.15+, or Linux (systemd) |
| CPU | Any modern x86_64 or arm64 |
| RAM | 50 MB |
| Disk | 10 MB |

### Web Dashboard

| Component | Version |
|-----------|---------|
| Node.js | 22+ |
| npm | 10+ |

---

## Pre-Migration Checklist

Before migrating:

- [ ] **Back up the database**:
  ```bash
  pg_dump -U ourway ourway > ourway-backup-$(date +%Y%m%d).sql
  ```

- [ ] **Back up configuration files**:
  ```bash
  cp /etc/ourway/server.env /etc/ourway/server.env.backup
  ```

- [ ] **Note your current version**:
  ```bash
  ourway-server --version
  ```

- [ ] **Test in staging**: If possible, test the migration on a staging environment first.

- [ ] **Communicate with users**: Inform users about potential downtime.

---

## Migration Steps

### Step 1: Stop the Server

```bash
sudo systemctl stop ourway-server
```

### Step 2: Build or Download the 2.0 Server

Option A: Build from source
```bash
cd server
git pull
go build -o ourway-server .
sudo mv ourway-server /opt/ourway/server/
```

Option B: Download binary
```bash
curl -L https://releases.ourway.io/server/v2.0.0/ourway-server-linux-amd64 \
  -o /opt/ourway/server/ourway-server
chmod +x /opt/ourway/server/ourway-server
```

### Step 3: Update Configuration (if needed)

Add any new environment variables to `/etc/ourway/server.env`:

```bash
# Redis (optional but recommended)
REDIS_URL="redis://localhost:6379/0"

# Rate limiting
RATE_LIMIT_REQUESTS="100"
RATE_LIMIT_WINDOW="1m"
```

### Step 4: Start the Server

```bash
sudo systemctl start ourway-server
```

GORM will automatically detect the schema version and run any pending migrations. Check the logs:

```bash
journalctl -u ourway-server -f
```

You should see migration messages like:
```
INFO Migrating database...
INFO Migrated: users table updated
INFO Migrated: roles table created
...
INFO Database migrations complete
```

### Step 5: Verify the Migration

1. Navigate to your dashboard URL
2. Log in with your existing credentials
3. Verify devices are showing up
4. Check that alerts are displaying correctly
5. Test RBAC by logging in as different users

### Step 6: Upgrade Agents

Agent upgrades are done per-device:

```bash
# On each managed device, run:
sudo ourway-agent --version  # Check current version
sudo ourway-agent --uninstall
sudo ourway-agent --server wss://ourway.example.com/ws --key <device-key> --install
```

Or use the installer script:
```bash
curl -sL https://releases.ourway.io/agent/install.sh | bash -s \
  --server wss://ourway.example.com/ws \
  --key <device-key> \
  --upgrade
```

Agents are backward-compatible, so you can upgrade the server first and agents later.

### Step 7: Test New Features

- Create a new user with a different role
- Start a remote session to a device
- Scan for updates on a device
- Push a file to a device
- Configure an SSO provider (if applicable)
- Create a webhook

---

## Database Migrations

All database migrations are handled automatically by GORM on server startup. The following changes are made:

### New Tables

| Table | Purpose |
|-------|---------|
| `roles` | RBAC roles |
| `permissions` | Permission definitions |
| `role_permissions` | Role-permission mappings |
| `user_roles` | User-role assignments |
| `sso_providers` | SSO provider configurations |
| `webhooks` | Webhook configurations |
| `webhook_deliveries` | Webhook delivery history |
| `api_keys` | API key management |
| `software_updates` | Software update tracking |
| `patch_policies` | Patch deployment policies |
| `patch_deployments` | Patch deployment records |
| `file_transfers` | File transfer tracking |
| `metric_history` | Historical metrics (time-series) |

### Modified Tables

| Table | Changes |
|-------|---------|
| `users` | Added: `provider`, `provider_id`, `sso_attributes`, `role_id` |
| `devices` | Added: `group_id`, `macos_energy`, `macos_disk_encryption` |
| `alerts` | Added: `acknowledged`, `acknowledged_by`, `assigned_to`, `dedup_key` |

### Migration Safety

- Migrations are idempotent — safe to run multiple times
- Migrations use `ADD COLUMN IF NOT EXISTS` where supported
- Data is preserved — no columns are dropped
- Migrations run in a transaction — either all succeed or none are applied

---

## Post-Migration Verification

Run through this checklist after migration:

- [ ] Server is running: `systemctl status ourway-server`
- [ ] Health check passes: `curl http://localhost:8080/health`
- [ ] Can log in to dashboard
- [ ] All devices are showing correct status
- [ ] Alerts are displaying correctly
- [ ] RBAC is working (test with different user roles)
- [ ] Agents are connecting successfully
- [ ] WebSocket connections are working
- [ ] Metrics are being collected
- [ ] No errors in server logs

---

## Rollback

If something goes wrong and you need to roll back:

### Rollback Server

```bash
# Stop 2.0 server
sudo systemctl stop ourway-server

# Restore 1.x binary
sudo cp /opt/ourway/server/ourway-server-1.x-backup /opt/ourway/server/ourway-server

# Start 1.x server
sudo systemctl start ourway-server
```

### Rollback Database

```bash
# Restore from backup
pg_restore -U ourway -d ourway --clean --if-exists ourway-backup-YYYYMMDD.sql
```

### Rollback Agents

```bash
# On each device, reinstall the 1.x agent
sudo ourway-agent --uninstall
sudo ourway-agent-1.x --server wss://ourway.example.com/ws --key <device-key> --install
```

---

## FAQ

### Q: Can I run 1.x and 2.0 servers simultaneously?

No. Each installation has one server version. However, 2.0 agents can connect to a 1.x server (limited feature set), and 1.x agents can connect to a 2.0 server.

### Q: Do I need to re-register devices?

No. Existing device registrations and keys remain valid.

### Q: Do I need to change my JWT secret?

No, but it's recommended. If you change it, all existing tokens become invalid and users will need to log in again.

### Q: What happens to my existing users?

Existing users are preserved. In 2.0, they will be assigned the Admin role by default. You can change their roles after migration.

### Q: Do agents auto-upgrade?

No. Agents must be upgraded manually per device. However, 2.0 supports an auto-update feature (planned for v2.1).

### Q: Is the web dashboard backward-compatible with older browsers?

OurWay 2.0 supports Chrome 100+, Firefox 100+, Safari 15+, and Edge 100+.
