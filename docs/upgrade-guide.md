# OurWay RMM — Upgrade Guide

Step-by-step instructions for upgrading OurWay components.

---

## Table of Contents

- [Upgrade Overview](#upgrade-overview)
- [In-Place vs Fresh Install](#in-place-vs-fresh-install)
- [Server Upgrade](#server-upgrade)
- [Agent Upgrade](#agent-upgrade)
- [Web Dashboard Upgrade](#web-dashboard-upgrade)
- [Backup Procedures](#backup-procedures)
- [Rollback Procedures](#rollback-procedures)

---

## Upgrade Overview

OurWay consists of three independently upgradable components:

| Component | Upgrade Method | Downtime |
|-----------|---------------|----------|
| Server | Replace binary | Brief (seconds) |
| Agent | Per-device reinstall | None (per device) |
| Web Dashboard | Replace static files | None |

Components are version-independent — you can upgrade them in any order.

### 2.3: a technician's visible set shrinks — read this first

2.3 adds client tenancy: a unit can belong to one client, and a non-admin account
is granted clients. **After the upgrade, every existing unit is unassigned**
(`devices.client_id IS NULL`), and an unassigned unit is visible to administrators
and **invisible to every other account**. So on the first login after the upgrade a
technician's fleet is empty — not broken, not deleted, and not a reason to roll
back. It is the release working: a technician sees the tenants they are granted and
nothing else. Administrators are unaffected (their scope is `all`), the agent↔server
pairing is untouched, and no agent is reinstalled — assignment is a server-side row.

The one-time step, in this order, on the console:

1. **Create the clients** — `/clients` → *New client*.
2. **Assign the units** — open a client's detail, tick its units, *Save units*.
   A unit moves between clients; it never belongs to two.
3. **Grant the clients to the accounts** — client detail → *Save access* (the
   account list is non-admin accounts; the grant is what makes their fleet).

Until step 2 runs, a technician's fleet is empty; until step 3 runs, it is empty
for that account. The server says so out loud: every scoped response carries
`X-Ourway-Client-Scope` (client ids, or `all` for an administrator), so "the unit
is gone" has a checkable answer in the network panel and in a HAR.

What the change looks like once it is set up:

- A unit outside the caller's clients answers `404` with the **same body a unit
  that does not exist gets**, on every route that reaches a unit — list, detail,
  alerts, sessions, files, patch actions. A technician cannot find, act on, or
  count another tenant's unit, and the fleet and compliance numbers they see are
  computed inside their own clients.
- Deleting a client leaves every unit intact and unassigned; it never deletes a
  unit.
- Bulk commands refuse out-of-scope targets by naming them (`device_ids`,
  `update_ids`), and the refusal is recorded in the audit ledger.

### Version Compatibility

| Server | Agent | Dashboard |
|--------|-------|-----------|
| 2.0.x | 1.x or 2.x | 2.x |
| 1.x | 1.x or 2.x (limited features) | 1.x or 2.x |

---

## In-Place vs Fresh Install

### In-Place Upgrade (Recommended for Most)

**When to use:** Upgrading between minor/patch versions, or 1.x → 2.0

**Pros:**
- Preserves database and configuration
- Faster
- Simpler

**Cons:**
- Old configuration options remain (may need cleanup)

### Fresh Install

**When to use:** Starting over, major version jumps, or corrupted installation

**Pros:**
- Clean configuration
- No legacy artifacts

**Cons:**
- Manual data migration (if needed)
- Reconfigure everything

---

## Server Upgrade

### Step 1: Backup

```bash
# Database backup
pg_dump -U ourway ourway > ourway-db-backup-$(date +%Y%m%d-%H%M%S).sql

# Configuration backup
cp /etc/ourway/server.env /etc/ourway/server.env.backup
```

### Step 2: Stop the Server

```bash
sudo systemctl stop ourway-server
```

### Step 3: Replace the Binary

Option A: Build from source
```bash
cd /path/to/ourway/server
git pull
go build -o ourway-server .
sudo cp ourway-server /opt/ourway/server/ourway-server
```

Option B: Download release binary
```bash
# Download new version (example: 2.0.0)
curl -L https://releases.ourway.io/server/v2.0.0/ourway-server-linux-amd64 \
  -o /opt/ourway/server/ourway-server
chmod +x /opt/ourway/server/ourway-server
```

### Step 4: Review Configuration Changes

Check the release notes for new environment variables. Update `/etc/ourway/server.env` if needed.

### Step 5: Start the Server

```bash
sudo systemctl start ourway-server
```

### Step 6: Verify

```bash
# Check health
curl http://localhost:8080/health

# Check logs for migration messages
journalctl -u ourway-server -f
```

### Step 7: Test

1. Log in to the web dashboard
2. Verify devices are showing
3. Check metrics are updating
4. Test API endpoints

### The audit ledger arrives with 2.3

Upgrading to 2.3 adds the `audit_log` table (migration 9) and starts recording
every mutating act. Nothing to configure, and nothing to migrate by hand: the
table starts empty, so **the log is not a record of the past**. History from
before the upgrade is not recoverable, and an operator who expects to see last
quarter's reboots in the page after upgrading is reading the wrong surface.

`AUDIT_RETENTION_DAYS` (default 365) sets the window. Rows past it are removed
by the same hourly sweep that prunes metric history, and the ledger page prints
the window, the last sweep and the rows it removed. If a client contract
requires a longer log, set it **before** the first write, not after a row has
already aged out.

Two visible things to expect after the upgrade:

- Administrators see every tenant's rows. A manager sees only the clients they
  are granted, and a viewer is refused the page. Asking for a client the caller
  is not granted answers an empty ledger, by design — the ledger's client ids
  are a list of which tenants exist in the installation.
- Refusals appear in the log as rows with an outcome (`refused`, `not_found`,
  `error`). A page full of `refused` entries after a permissions change is not
  an outage; it is the log doing the job it was built for, recording the attempts
  that did not work.

---

## Agent Upgrade

### Single Device Upgrade

```bash
# On the target device:
sudo ourway-agent --uninstall
sudo ourway-agent --server wss://ourway.example.com/ws --key <device-key> --install
```

Or download a new binary:
```bash
curl -L https://releases.ourway.io/agent/v2.0.0/ourway-agent-linux-amd64 \
  -o /usr/local/bin/ourway-agent
chmod +x /usr/local/bin/ourway-agent
sudo systemctl restart ourway-agent
```

### Batch Upgrade (All Devices)

Create a script to upgrade all agents:

```bash
#!/bin/bash
# upgrade-all-agents.sh

DEVICES=("host1" "host2" "host3")
for host in "${DEVICES[@]}"; do
  echo "Upgrading agent on $host..."
  ssh root@$host "sudo ourway-agent --uninstall && sudo ourway-agent --server wss://ourway.example.com/ws --key $(cat /etc/ourway/device-key) --install"
done
```

### Docker Upgrade

```bash
docker compose pull ourway-server
docker compose up -d ourway-server
```

---

## Web Dashboard Upgrade

### In-Place Upgrade

```bash
cd /path/to/ourway/web
git pull
npm install
npm run build
```

If using Docker:
```bash
docker compose pull ourway-web
docker compose up -d ourway-web
```

### Browser Cache

After upgrading, users should refresh their browsers (Ctrl+Shift+R) to get the latest assets.

---

## Backup Procedures

### Database Backup

```bash
# Full backup
pg_dump -U ourway ourway > ourway-full-backup-$(date +%Y%m%d-%H%M%S).sql

# Compressed backup
pg_dump -U ourway ourway | gzip > ourway-backup-$(date +%Y%m%d-%H%M%S).sql.gz

# Schema-only backup
pg_dump -U ourway --schema-only ourway > ourway-schema-backup.sql
```

### Restore from Backup

```bash
# From plain SQL
psql -U ourway ourway < ourway-backup-YYYYMMDD-HHMMSS.sql

# From compressed SQL
gunzip -c ourway-backup-YYYYMMDD-HHMMSS.sql.gz | psql -U ourway ourway
```

### Automated Backups

Add a cron job for daily backups:

```cron
0 2 * * * pg_dump -U ourway ourway | gzip > /backups/ourway-$(date +\%Y\%m\%d).sql.gz
```

Keep 7 days of backups:

```cron
0 3 * * * find /backups -name "ourway-*.sql.gz" -mtime +7 -delete
```

---

## Rollback Procedures

### Server Rollback

```bash
# Stop server
sudo systemctl stop ourway-server

# Restore old binary
sudo cp /opt/ourway/server/ourway-server-backup /opt/ourway/server/ourway-server

# Start server
sudo systemctl start ourway-server
```

### Database Rollback

```bash
# Drop and restore (careful with data!)
psql -U ourway -c "DROP DATABASE ourway;"
createdb -U ourway ourway
psql -U ourway ourway < ourway-backup-YYYYMMDD-HHMMSS.sql
```

### Agent Rollback

```bash
# On each device
sudo ourway-agent --uninstall
sudo ourway-agent-old --server wss://ourway.example.com/ws --key <device-key> --install
```

---

## Troubleshooting Upgrades

### Server won't start

```bash
# Check logs
journalctl -u ourway-server -f

# Common issues:
# - Database migration failed → check PostgreSQL is running
# - Port already in use → check netstat -tlnp | grep 8080
# - Permission denied → check file permissions
```

### Devices showing offline

```bash
# Check agent status
systemctl status ourway-agent

# Check agent logs
journalctl -u ourway-agent -f

# Verify server is reachable
curl http://ourway.example.com/health
```

### Dashboard not loading

```bash
# Check nginx
systemctl status nginx

# Check web build
ls /var/www/ourway/

# Clear browser cache
# Ctrl+Shift+R
```

### RBAC issues after upgrade

Existing users keep their current roles after the upgrade (2.0 does not auto-assign roles). If a user has no role:

```bash
# Manually assign admin role via API
curl -X PUT http://localhost:8080/api/users/<user-id>/roles \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{"roles": ["admin"]}'
```
