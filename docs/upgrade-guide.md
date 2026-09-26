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
# Check current version
ourway-server --version

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

# Check version
ourway-server --version

# Check logs for migration messages
journalctl -u ourway-server -f
```

### Step 7: Test

1. Log in to the web dashboard
2. Verify devices are showing
3. Check metrics are updating
4. Test API endpoints

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

Existing users should be auto-assigned the Admin role. If not:

```bash
# Manually assign admin role via API
curl -X POST http://localhost:8080/api/v2/users/<user-id>/roles \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{"role_id": "<admin-role-id>"}'
```
