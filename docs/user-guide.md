# OurWay RMM — User Guide

A task-oriented guide to using the OurWay RMM dashboard.

---

## Table of Contents

- [Getting Started](#getting-started)
- [Dashboard](#dashboard)
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
   (The device key can be found in the Devices page after pre-registering, or the agent auto-registers.)

2. **View your device**: Once the agent connects, it appears on the Dashboard.

3. **Configure alerts**: Navigate to Devices → [Device] to set custom thresholds.

---

## Dashboard

The Dashboard provides an at-a-glance overview of your entire fleet.

### What You'll See

- **Device count**: Total, online, and offline devices
- **Alert count**: Unresolved alerts by severity
- **Device grid**: Quick status of all devices with real-time updates

### Real-Time Updates

The dashboard updates in real-time via WebSocket. You don't need to refresh the page — device status, metrics, and alerts update automatically.

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

#### Streaming Mode

When viewing a device detail page, the agent automatically switches to streaming mode, sending metrics every 2 seconds instead of 60. Close the page to revert to normal mode.

#### Remote Session

Click **Remote Session** on the device detail page to start a WebRTC-based remote desktop session.

- **View mode**: Watch the screen without controlling
- **Control mode**: Click, type, and interact with the remote machine
- **Quality settings**: Adjust resolution and FPS

#### Device Actions

- **Reboot**: Restart the device
- **Run command**: Execute a shell or PowerShell command
- **Scan for updates**: Trigger a software update scan
- **Remove device**: Delete the device from OurWay

### Custom Alert Thresholds

On the device detail page, click **Alert Settings** to configure per-device thresholds:

- CPU warning/critical thresholds (%)
- Memory warning/critical thresholds (%)
- Disk warning/critical thresholds (%)

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
4. **Resolved**: Alert cleared manually or automatically when threshold is met

### Actions

- **Acknowledge**: Mark as seen
- **Assign**: Assign to a user
- **Resolve**: Mark as resolved
- **View device**: Jump to the device detail page

### Alert Deduplication

Alerts are automatically deduplicated within a 5-minute window. If the same alert condition persists, you won't get spam notifications.

---

## Remote Sessions

Remote sessions provide real-time screen sharing and control over any managed device.

### Starting a Session

1. Open a device detail page
2. Click **Remote Session**
3. Choose **View** or **Control** mode
4. Adjust quality settings (optional)
5. Click **Start**

### Session Controls

- **Keyboard input**: Type directly into the remote session
- **Mouse input**: Click and drag to interact
- **Copy/Paste**: Clipboard sync between local and remote
- **Screenshot**: Capture the current screen
- **End session**: Close the remote session

### Session Recording

Sessions can be recorded for playback later. Access recordings from the device detail page under **Sessions**.

---

## Patch Management

Patch management automates software update scanning, approval, and deployment across your fleet.

### Scanning for Updates

1. Navigate to **Patches**
2. Click **Scan All Devices** or select specific devices
3. Wait for scan results

### Viewing Available Updates

The Patches page shows:

- Devices with available updates
- Update details (package name, version, source)
- Total update count per device

### Creating Patch Policies

Click **New Policy** to create a patch policy:

- **Name**: Policy name
- **Scope**: All devices or specific group
- **Schedule**: Weekly, monthly, or custom
- **Auto-reboot**: Automatically reboot after deployment
- **Approval required**: Require manual approval before deploying
- **Batch size**: Maximum devices to patch simultaneously

### Deploying Patches

1. Select updates to deploy
2. Click **Deploy**
3. Monitor deployment progress
4. View results (success/failure per device)

### Deployment Rollback

If a deployment fails, you can roll it back:

1. Navigate to **Patches** → **Deployments**
2. Select the failed deployment
3. Click **Rollback**

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
| Admin | Everything | Everything | Yes | Yes |
| Manager | Everything | Devices, Alerts | No | Yes |
| Technician | Devices, Alerts | Alerts, Commands | No | No |
| Viewer | Everything | Nothing | No | No |

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
3. Choose a type: Google, Microsoft, or Apple
4. Enter client ID, client secret, and other details
5. Enable the provider
6. Click **Save**

### Provider Details

#### Google

- **Auth URL**: `https://accounts.google.com/o/oauth2/v2/auth`
- **Token URL**: `https://oauth2.googleapis.com/token`
- **User Info URL**: `https://openidconnect.googleapis.com/v1/userinfo`
- **Scopes**: `openid profile email`

#### Microsoft

- **Auth URL**: `https://login.microsoftonline.com/common/oauth2/v2.0/authorize`
- **Token URL**: `https://login.microsoftonline.com/common/oauth2/v2.0/token`
- **User Info URL**: `https://graph.microsoft.com/oidc/userinfo`
- **Scopes**: `openid profile email`

#### Apple

- **Auth URL**: `https://appleid.apple.com/auth/authorize`
- **Token URL**: `https://appleid.apple.com/auth/token`
- **User Info URL**: `https://appleid.apple.com/auth/userinfo`
- **Scopes**: `name email`

### Just-in-Time Provisioning

When a user logs in via SSO for the first time, an account is automatically created with:

- Email from SSO as username
- Random password (not used, SSO is primary)
- Viewer role (admin can change)

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
- `alert_created`: New alert created
- `alert_resolved`: Alert resolved
- `patch_deployed`: Patch deployed to device
- `session_started`: Remote session started

### Testing a Webhook

Click **Test** on a webhook to send a sample event and verify it works.

### Delivery History

Click a webhook to view recent delivery history, including status codes and response times.

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
7. **Copy the key** — it's only shown once

### Using an API Key

Include the key in API requests:

```bash
curl -H "X-API-Key: owk_<your-key>" https://ourway.example.com/api/v2/devices
```

Or with the Bearer prefix:

```bash
curl -H "Authorization: Bearer owk_<your-key>" https://ourway.example.com/api/v2/devices
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
- `g + a`: Go to Alerts
- `g + f`: Go to Files
- `g + p`: Go to Patches
- `g + u`: Go to Users
- `g + s`: Go to Settings
- `g + h`: Go to Home

---

## Tips & Best Practices

### Monitoring

- Set custom alert thresholds per critical server
- Acknowledge alerts promptly to prevent alert fatigue
- Use device groups to organize related devices

### Patching

- Test patches on a pilot group before fleet-wide deployment
- Use maintenance windows for reboot-required updates
- Monitor deployment success rates

### Security

- Use SSO for centralized identity management
- Enable MFA where possible
- Use API keys with minimal scopes
- Rotate API keys periodically
