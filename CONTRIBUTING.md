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
| `cmd/ourway-viewer/` | Native Windows remote-control viewer (Gio); talks to the server, not the agent |
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

## End-to-end suite (2.2-0)

The e2e suite drives the **production bundle against a real server and
PostgreSQL** — no mocks, no dev-server behaviour — so it is the layer that can
fail a release. It lives in `web/e2e/` and runs in CI
(`.github/workflows/e2e.yml`).

```sh
# 1. infrastructure the suite asserts against:
#    PostgreSQL (and Redis if you use it), then the server against it
DATABASE_URL=postgresql://ourway:ourway@localhost:5432/ourway?sslmode=disable go -C server run
# 2. the built console, proxied to that server (vite.config.ts preview.proxy)
npm run build --prefix web
npm run preview --prefix web            # :3000, proxies /api and /ws
# 3. the suite
npm run test:e2e:install --prefix web   # once, installs Chromium
npm run test:e2e --prefix web
```

`OURWAY_E2E_BASE_URL` / `OURWAY_E2E_API_URL` name the console and API origins
(`http://localhost:3000` and `http://localhost:9090` by default), and the preview
proxy target follows `OURWAY_PROXY_TARGET`. `vite.config.ts` shares the proxy
config between `server` and `preview`, and Vitest stays scoped to
`src/**/*.test.{ts,tsx}` so it never collects Playwright specs.

Three constraints that are easy to get wrong in this suite:

- **Seeding is registration, not SQL** (`e2e/fixtures.ts`), so re-running against
  a database that already holds a fleet hits the C4 re-registration guard. Point
  `OURWAY_E2E_API_URL` at a fresh database (what CI does), or set
  `OURWAY_E2E_RUN_ID=2` to seed a run-scoped fleet — host names and private
  addresses move with the run, so the units enroll cleanly.
- **Do not `waitForLoadState('networkidle')`.** The console holds a WebSocket open
  for the whole session, so that event never settles.
- **Destructive actions are gated by `window.confirm` today** (`buglist.md` M19,
  task 2.2-9): Playwright dismisses dialogs by default, so a test that ends a
  session must accept one. That is the smell, not the design.

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

**The git tag is the version** — read `docs/versioning.md` first: what
MAJOR/MINOR/PATCH mean for an operator, the stable/edge/dev channels, and how
the stamp reaches every binary, image and the console.

Cut a release with the script, not by hand:

```sh
scripts/release.sh --check 2.1.0   # every check + the full gate, publishes nothing
scripts/release.sh 2.1.0           # commit, tag v2.1.0, push main and the tag
```

It refuses a dirty tree, a non-`main` branch, a HEAD that is not the pushed
tip, a number that is not greater than the latest tag, and a red gate.

The pipeline then runs in order: agents → installers → Docker images (GHCR) →
GitHub Release with all binaries attached. A tag publishes `vX.Y.Z`, the
floating `X.Y`, and `latest`; a `main` push publishes `edge` only (the old
scheme published a per-commit sha tag; `edge` replaces it). Verify the
release run is green before announcing a version.
