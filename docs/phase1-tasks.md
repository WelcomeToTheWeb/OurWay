# Phase 1 Implementation Tasks

## Workstream 1: User Management (RBAC)

### Server
- [x] Create `Role` model (`server/models/role.go`)
- [x] Create `UserRole` model (`server/models/user_role.go`)
- [x] Create `RoleStore` (`server/store/roles.go`)
- [x] Create `UserRoleStore` (`server/store/user_roles.go`)
- [x] Add roles to JWT claims (`server/auth/jwt.go`)
- [x] Create RBAC middleware (`server/api/middleware.go`)
- [x] Create role management API endpoints (`server/api/roles.go`)
- [x] Create user management API endpoints (`server/api/users.go`)
- [x] Seed built-in roles (Admin, Manager, Technician, Viewer)
- [x] Update `AutoMigrate` to include new models
- [x] Update auth handler to include roles in tokens

### Frontend
- [x] Update `User` type with roles
- [x] Create role-based protected routes (`RoleRoute` component)
- [x] Add user management page (`Users.tsx`)
- [x] Update auth context with role info and `hasRole`/`hasAnyRole` helpers
- [x] Update sidebar with role-based navigation
- [x] Add acknowledge/assign buttons to alert cards
- [x] Show acknowledged/assigned status on alerts

## Workstream 2: macOS Agent Support

### Agent
- [x] Verify existing `install/darwin.go` works
- [x] Add macOS energy collector (`collector/darwin_energy.go`)
- [x] Add macOS software inventory collector (`collector/darwin_software.go`)
- [x] Add macOS disk encryption collector (`collector/darwin_encryption.go`)
- [x] Add platform collector injection system
- [x] Test macOS build (arm64 + x64)
- [x] Test Linux and Windows builds still work

### Docker
- [x] Create `docker/Dockerfile.agent-darwin` for macOS agent builds
- [x] Add build script for macOS agents (`docker/build-agents.sh`)

## Workstream 3: Alert System Enhancements

### Server
- [x] Add deduplication fields to Alert model
- [x] Add acknowledgment/assignment fields to Alert model
- [x] Update alert engine with configurable thresholds
- [x] Implement alert deduplication (5-min window)
- [x] Add acknowledge and assign API endpoints
- [ ] Add per-device custom threshold configuration
- [ ] Add tests for enhanced alert system

### Frontend
- [ ] Update alert UI with new fields
- [ ] Add alert rule configuration UI
- [ ] Add acknowledge/assign actions

---

*Started: 2026-09-24*
