# Phase 3 Implementation Tasks: Authentication & Integration

**Target:** Weeks 11-14  
**Goal:** Enterprise-grade authentication and third-party integrations.

---

## Workstream 1: SSO Integration

### Server
- [x] Create `SSOProvider` model (id, type, name, config JSON, enabled, created_at)
- [x] Create `SSOProviderStore` (`server/store/sso_providers.go`)
- [x] Add `provider`, `provider_id`, `sso_attributes` fields to User model
- [x] Build OAuth 2.0 authentication flow
  - [x] Google, Microsoft, Apple support
  - [x] State parameter for CSRF protection
  - [x] Token exchange and user info retrieval
- [x] Add SSO API endpoints:
  - [x] `GET /api/auth/sso/providers` — list configured providers
  - [x] `GET /api/auth/sso/:provider/authorize` — redirect to provider
  - [x] `GET /api/auth/sso/:provider/callback` — handle callback
  - [x] `POST /api/sso/providers` — create/update provider (admin)
  - [x] `GET /api/sso/providers` — list providers (admin)
  - [x] `DELETE /api/sso/providers/:id` — delete provider (admin)
- [x] Just-in-time user provisioning
  - [x] Auto-create users on first SSO login
  - [x] Map provider attributes to user fields
- [x] Fallback to local authentication

### Frontend
- [x] Add "Sign in with [Provider]" buttons to login page
- [x] SSO provider configuration UI (admin)
  - [x] Add/edit/delete providers
  - [x] Configure client ID, client secret, endpoints
  - [x] Enable/disable providers

---

## Workstream 2: API v2 & Webhooks

### Server
- [x] Create versioned API structure (`/api/v2/` routes)
- [x] Create `Webhook` model (id, name, url, events JSON, headers JSON, enabled, created_at)
- [x] Create `WebhookDelivery` model (id, webhook_id, event, status, response_code, response_body, attempts, next_retry_at, delivered_at, created_at)
- [x] Create stores for webhook models
- [x] Build webhook event system
  - [x] Event types: device_registered, device_online, alert_created, alert_resolved, patch_deployed, session_started
  - [x] Event publisher interface
  - [x] HTTP delivery with retry logic (exponential backoff, max 5 attempts)
  - [x] Delivery tracking and logging
- [x] Add webhook API endpoints:
  - [x] `POST /api/v2/webhooks` — create webhook
  - [x] `GET /api/v2/webhooks` — list webhooks
  - [x] `GET /api/v2/webhooks/:id` — get webhook
  - [x] `PUT /api/v2/webhooks/:id` — update webhook
  - [x] `DELETE /api/v2/webhooks/:id` — delete webhook
  - [x] `POST /api/v2/webhooks/:id/test` — send test event
- [x] Add webhook delivery endpoints:
  - [x] `GET /api/v2/webhooks/:id/deliveries` — list deliveries
  - [x] `POST /api/v2/webhooks/:id/deliveries/:delivery_id/retry` — retry failed delivery
- [x] Rate limiting middleware (per user/API key)
- [x] Trigger webhooks from existing flows (alerts, devices, patches, sessions)

### Frontend
- [x] Webhook configuration UI
  - [x] CRUD for webhooks
  - [x] Event type selection (checkboxes)
  - [x] Custom headers support
  - [x] Test delivery button
  - [x] Delivery history with status

---

## Cross-Cutting Tasks

### Database
- [x] Update `AutoMigrate` with all new models

### API
- [x] Update API documentation for all new endpoints

### Testing
- [ ] Unit tests for SSO OAuth flow (mock provider)
- [ ] Unit tests for webhook delivery (mock HTTP)
- [ ] Unit tests for rate limiting
- [ ] Integration tests for SSO callback flow

### Documentation
- [x] SSO setup guide

---

*Created: 2026-09-25*
*Updated: 2026-09-25*
