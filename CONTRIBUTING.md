# Contributing to OurWay

Thanks for contributing! This document describes the project layout, the
verification gate every change must pass, and the conventions the codebase
follows.

## Repository layout

OurWay is a device-management, monitoring, patching and remote-control system
composed of:

| Path | What it is |
|------|-----------|
| `server/` | Go backend: REST API, WebSocket hub, stores, alert engine, webhooks |
| `agent/` | Go endpoint agent (collects metrics, applies patches, remote sessions) |
| `cmd/ourway-cli/` | Go command-line client |
| `cmd/ourway-installer/` | Self-contained per-platform installer (embeds the agent binary) |
| `web/` | React + TypeScript + Vite dashboard |
| `docker/` | Dockerfiles (`Dockerfile.server`, `Dockerfile.web`) |
| `docs/` | API, configuration, user and release documentation |

Each Go directory is its own module with its own `go.mod`/`go.sum`.

## The verification gate

A change is not done until the full gate is green:

```sh
# Server
go -C server build ./...
go -C server vet ./...
go -C server test ./...

# Agent (plus cross-compiles when touching platform files)
go -C agent build ./...
go -C agent vet ./...
go -C agent test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...

# Web (tsc + vite)
npm ci --prefix web
npm run build --prefix web
```

The same gate runs in CI (`.github/workflows/build.yml`, `tests` job) and
Docker images are only pushed when it passes.

## Conventions

- **Commit messages** use conventional-commit prefixes: `fix:`, `feat:`,
  `ci:`, `docs:`, `style:`, with an optional scope — `fix(ci):`,
  `feat(auth):`. Imperative subject, no trailing period. Larger changes land
  as one batched commit whose subject enumerates the concerns.
- **Go formatting** is enforced with `gofmt`; `go vet` cleanliness is itself
  a gate.
- **Server layering** is `api → store interface → store implementations`.
  New endpoints conventionally need a store entry, a migration (if
  persistence is required), an API handler, and a route registration in
  `server/api/handlers.go` — routes are registered there, not in handler
  files. Interface changes cascade to the mocks in `_test.go` files in the
  same change.
- **WebSocket messages** cross the web/server/agent boundary as a
  `{type, payload}` envelope, kept identical on all three sides.
- **Web i18n**: user-facing strings go through the translation function
  `t(...)`. Locale keys are introduced **in lockstep across `en`, `de` and
  `fr`** — a key missing from any locale is a bug. Do not name locals or
  callback parameters `t` (it shadows the translator).
- **Error handling** favors failing loudly: a failed install step must not
  leave a partially-initialized state, and one failing metric collector must
  not drop the whole metrics batch.
- **Tests** track the shipped contract: when a response shape changes, the
  tests (and web client decoders) are updated in the same change.

## Installing locally

- Server: `go -C server run .` (needs PostgreSQL; see `docs/configuration.md`
  and `.env.example`).
- Web dev server: `npm run dev --prefix web` — it proxies `/api` and `/ws`
  to `localhost:9090`, so run the server with `SERVER_PORT=:9090`.
- Agent: `go -C agent run . --server http://localhost:8080 --key <device-key>`.

## Releasing

Releases are cut from `v*` git tags. The pipeline builds in order:
agents → installers → Docker images (GHCR) → GitHub Release with all
binaries attached. Verify the release run is green before announcing a
version.
