# Phase 2 Implementation Tasks: Remote Management

**Target:** Weeks 5-10  
**Architecture Decisions:**
- Remote Sessions: WebRTC via `pion/webrtc/v3`
- Patching: Full automated management (scan → approve → deploy → reboot → report → rollback)
- File Transfer: Server-mediated HTTP (agent pulls from server)

---

## Workstream 1: Remote Sessions (WebRTC)

### Server
- [x] Add `pion/webrtc/v3` dependency
- [x] Create `Session` model (id, device_id, user_id, status, created_at, ended_at)
- [x] Create `SessionStore` (`server/store/sessions.go`)
- [x] Create WebRTC Session Gateway service (`server/sessions/gateway.go`)
  - Create RTCPeerConnection for each session
  - Handle offer/answer/signaling via REST API
  - Relay frames between agent and browser
- [x] Add session signaling API endpoints:
  - `POST /api/devices/:id/sessions` — start session, return offer
  - `POST /api/sessions/:id/answer` — browser sends answer
  - `POST /api/sessions/:id/ice` — exchange ICE candidates
  - `DELETE /api/sessions/:id` — end session
- [x] Add WebRTC data channel support for input events (keyboard/mouse)
- [x] Add `SendToDevice()` and `SendToUser()` methods to WebSocket Hub

### Agent
- [x] Create screen capture service (platform-specific)
  - Linux: X11/Wayland capture
  - Windows: GDI Desktop Duplication API
  - macOS: CoreGraphics screen capture
- [x] Add WebRTC peer connection to agent
  - Receive session start signal from server
  - Establish data channel for frames
- [x] Capture and encode frames (JPEG, configurable quality)
  - Delta encoding for changed regions
  - Frame rate control (15-30fps)
- [x] Receive and process input events
  - Keyboard events (key up/down, special keys)
  - Mouse events (move, click, scroll)
- [x] Session lifecycle management (start, pause, end)

### Frontend
- [x] Add WebRTC session view component (`SessionView.tsx`)
  - Connect to WebRTC via signaling API
  - Display remote screen
  - Capture and send keyboard/mouse input
- [x] Add session quality controls (low/balanced/high)
- [x] Add "Start Session" button to device detail page
- [x] Handle session events (started, ended, error)

---

## Workstream 2: Software Patching

### Server
- [x] Create `SoftwareUpdate` model (id, device_id, source, title, version, size, status, installed_at, error_message)
- [x] Create `PatchPolicy` model (id, name, scope, schedule, auto_reboot, approval_required)
- [x] Create `PatchDeployment` model (id, policy_id, status, devices_total, devices_success, devices_failed, started_at, completed_at)
- [x] Create stores for all patch models
- [x] Build patch scanning engine
  - Query devices for installed packages
  - Compare against known update catalogs
  - Generate update reports
- [x] Build patch deployment engine
  - Create deployment batches
  - Send install commands to agents
  - Track progress and results
  - Handle failures and retries
- [x] Add reboot management
  - Schedule reboots after patching
  - Handle reboot deferrals
- [x] Add approval workflow endpoints
  - `POST /api/devices/:id/updates/scan` — trigger scan
  - `GET /api/devices/:id/updates` — list available updates
  - `POST /api/patch/deploy` — trigger deployment
  - `GET /api/patch/deployments` — get deployment status
- [x] Add patch policy management endpoints
  - `GET /api/patch/policies` — list policies
  - `POST /api/patch/policies` — create policy

### Agent
- [x] Create package manager integrations
  - Linux: apt, yum/dnf, pacman, zypper (detect and use appropriate)
  - Windows: Windows Update Agent, winget
  - macOS: Software Update, Homebrew
- [x] Add command handlers for patch operations
  - scan_updates
  - deploy_updates

### Frontend
- [x] Create Patch Management page (`Patches.tsx`)
  - Fleet update overview
  - Compliance dashboard
  - Update lists by device
- [x] Create Patch Policy page (`PatchPolicies.tsx`)
  - CRUD for policies
  - Schedule configuration
- [x] Create Deployment view (integrated in PatchPolicies.tsx)
  - Real-time deployment progress
  - Success/failure tracking
- [x] Add "Scan for Updates" and "Deploy" actions

---

## Workstream 3: File Transfer

### Server
- [x] Create `FileTransfer` model (id, device_id, filename, size, status, direction, progress, created_at, completed_at)
- [x] Create `FileTransferStore`
- [x] Add file upload endpoint
  - `POST /api/files/upload` — upload file to server
  - Store file on disk
  - Return transfer ID
- [x] Add file transfer initiation endpoints
  - `POST /api/files/push` — push file to device(s)
  - `POST /api/files/pull` — pull file from device
- [x] Add file download endpoint for agents
  - `GET /api/files/:transfer_id/download` — agent downloads file
- [x] Add transfer status endpoints
  - `GET /api/files/transfers` — list transfers
  - `GET /api/files/transfers/:id` — get transfer details
- [x] Add file serving for retrieved files
  - `GET /api/files/:transfer_id/file` — download retrieved file

### Agent
- [x] Implement file transfer command handler
  - Receive transfer command via WebSocket
  - Download file from server (HTTP GET)
  - Copy to destination on device
  - Report progress and completion
- [x] Implement file retrieval
  - Read file from device
  - Upload to server
  - Report progress and completion
- [x] Add transfer status reporting

### Frontend
- [x] Create File Transfer UI component
  - Drag-and-drop file upload
  - Transfer progress indicators
  - Transfer history
- [x] Add file push/pull buttons to device detail page (via Files page)
- [x] Handle large files (>100MB) with progress tracking

---

## Cross-Cutting Tasks

### Database
- [x] Update `AutoMigrate` with all new models
- [x] Add indexes for performance (device_id, status, timestamps)

### API
- [ ] Update API documentation
- [ ] Add rate limiting for file operations
- [x] Add rollback endpoint: `POST /api/patch/deployments/:id/rollback`

### Testing
- [x] Unit tests for patching APIs (scan, deploy, rollback, policies)
- [x] Unit tests for file transfer APIs (upload, status, transfers)
- [ ] Unit tests for all new models and stores
- [ ] Integration tests for session signaling
- [ ] Integration tests for patch scanning and deployment
- [ ] Integration tests for file transfers
- [ ] Agent tests for screen capture (mock display)
- [ ] Agent tests for package management (mock commands)

### Documentation
- [ ] Update architecture docs with new components
- [x] Update changelog

### Optional Enhancements
- [x] Agent-side file transfer progress reporting
- [x] Server endpoint for agent file status updates: `POST /api/agent/files/status`
- [x] Rollback support for failed patch deployments (server + agent)
- [ ] Custom patch catalogs
- [ ] File chunking for large files

---

## Dependency Install Plan

### Server
```bash
go get github.com/pion/webrtc/v3
```

### Agent
- Linux screen capture: `go get github.com/disintegration/imaging` (for frame processing)
- Windows: Use existing `go-ole` for Desktop Duplication
- macOS: CoreGraphics via cgo (no additional deps)

### Frontend
```bash
npm install @novnc/novnc  # or custom WebRTC viewer
```

---

*Created: 2026-09-24*