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

# Coverage floor (2.2-0)
scripts/coverage.sh

# Locale lockstep (2.2 gate): en/de/fr must define the same key paths
python3 scripts/i18n-parity.py
```

The darwin cross-compile above is a compile check of the **non-cgo** path. cgo
cannot cross-compile from Linux, so the macOS agent that controls input
(`agent/session/input_darwin_cgo.go`, CoreGraphics) is only ever built on a macOS
runner with `CGO_ENABLED=1` — `.github/workflows/build.yml`, `build-macos-agents`.
A darwin binary built any other way reports `input_capability: unavailable` and
drops remote input by design, so never ship one.

`scripts/coverage.sh` measures total statement coverage for `server/` and `agent/`
(inside a `GO_IMAGE` container when the machine has no Go toolchain)
against `coverage.baseline.json` and fails on a decrease — see `scripts/coverage.sh`
for the flags. Raise the baseline with `--record` in the same commit that earns it.

The same gate runs in CI (`.github/workflows/build.yml`, `tests` job) and
Docker images are only pushed when it passes.

## Agent smoke (2.2-0)

`scripts/agent-smoke.sh` runs the **real agent binary** against a running server and
asserts the install path in order: enroll → device WebSocket (unit goes `online`) →
heartbeat → metric samples → a remote session reaching `active` (which only happens
when the agent ships frames) → the installed service reporting heartbeats of its own.
CI runs it on Linux, Windows and macOS
(`.github/workflows/agent-smoke.yml`, `--expect-frames --expect-service`).

```sh
# Linux needs an X display for capture (scrot/import), and scrot is not installed:
Xvfb :99 -screen 0 1280x800x24 -ac & export DISPLAY=:99
scripts/agent-smoke.sh --server http://localhost:9090 --agent ./ourway-agent \
                       --secret "$ENROLLMENT_SECRET" --expect-frames
```

Notes that cost an hour to learn: `Xvfb` takes `-screen 0 <WxHxD>` (the screen
*index* first) and `-ac` for access control, and `--expect-service` needs systemd /
launchd / the Windows SCM, so it is a runner feature, not a container one. On macOS
the script refuses to assert frames (Screen Recording is a TCC grant that cannot be
granted non-interactively) and says so out loud.

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

`scripts/e2e-stack.sh restart` is the shortcut for the stack above when the
machine has no Go toolchain (this repo's dev container): it recreates the
`ourway_e2e` database, builds the server in a Go container, runs it on the host
at `:9099`, and serves the built bundle at `:3100` with the proxy pointed at it.
Then:

```sh
OURWAY_E2E_ENROLLMENT_SECRET=dev-enrollment-secret \
OURWAY_E2E_BASE_URL=http://localhost:3100 OURWAY_E2E_API_URL=http://localhost:9099 \
  npx playwright test -c e2e/playwright.config.ts --reporter=list   # from web/
```

`OURWAY_E2E_BASE_URL` / `OURWAY_E2E_API_URL` name the console and API origins
(`http://localhost:3000` and `http://localhost:9090` by default), and the preview
proxy target follows `OURWAY_PROXY_TARGET`. `vite.config.ts` shares the proxy
config between `server` and `preview`, and Vitest stays scoped to
`src/**/*.test.{ts,tsx}` so it never collects Playwright specs.

The seeded fleet deliberately carries all four remote-control states (2.2-1), and
the fake agent reports them on the wire (`heartbeat` + `metrics`, in the agent's
own `{type, payload}` envelope): a Windows and a Linux unit report `granted`, the
Mac unit reports `denied` + `capture_state: error`, and a unit whose agent has
never reported renders as *Not reported*. The seeded load travels with the
report, so the alert engine keeps evaluating the fleet the seeder described.

Four constraints that are easy to get wrong in this suite:

- **Seeding is registration, not SQL** (`e2e/fixtures.ts`), so re-running against
  a database that already holds a fleet hits the C4 re-registration guard. Point
  `OURWAY_E2E_API_URL` at a fresh database (what CI does), or set
  `OURWAY_E2E_RUN_ID=2` to seed a run-scoped fleet — host names and private
  addresses move with the run, so the units enroll cleanly. Set
  `OURWAY_E2E_ENROLLMENT_SECRET` to the server's `ENROLLMENT_SECRET` and the
  harness sends `x-enrollment-secret` on registration, so the gate is exercised
  on every run; run without it against a server that has one and seeding stops
  there. The harness quotes the server's own reason in the 401, because "already
  enrolled" and "enrollment secret missing" are opposite problems. A shared
  database plus that secret means registration replays: state from the previous
  run (approved updates, acknowledged alerts) survives, so repeated local runs
  want a run id, not a shared fleet.
- **A viewer socket *is* a session.** The hub ends the session the moment the
  viewer that started it disconnects (`server/ws/hub.go:685`). So a test that
  starts a session from the console overlay and then navigates is watching a
  session it just ended: start sessions through the API when the assertion is
  about the ledger, and keep the overlay open when the assertion is about the
  session itself.
- **One connection per device key.** The hub accepts a single client per key, so
  a test that opens a device socket displaces the harness's fake agent for that
  unit. The displacement arrives as close code **1008**
  ("superseded by newer connection", `server/ws/hub.go:617`), and the support
  agent treats it as such — 50 ms, not the exponential back-off a network drop
  deserves, and one pending reconnect per key, because two sockets for one key
  take turns superseding each other. Tests that need a connected unit wait for it
  to report `online` (`expectUnitOnline` in `e2e/sessions.spec.ts`) before they
  act on it.
- **Assert where the subject can exist.** A browser assertion needs its setup to
  be possible in CI, not just locally. `POST /api/devices/:id/sessions` answers
  409 "device is not connected" (the hub has no socket) while the unit's stored
  status says `online`, and CI's darwin unit sits in that state for a whole run —
  four retries, four runs. A spec that cannot start the thing it asserts is a
  spec that quietly never runs, so 2.2-1's session-panel copy is asserted in
  `web/src/components/SessionView.test.tsx` (the component and the prop that
  decide it) while `e2e/devices.spec.ts` keeps the unit view. When a browser test
  fails only in CI, say which of the two is true — the product is wrong, or the
  test's precondition never holds there — and put the assertion where it runs.
- **Do not `waitForLoadState('networkidle')`.** The console holds a WebSocket open
  for the whole session, so that event never settles.
- **Destructive actions are gated by `window.confirm` today** (`buglist.md` M19,
  task 2.2-9): Playwright dismisses dialogs by default, so a test that ends a
  session must accept one. That is the smell, not the design.

## Alert counts (2.2-4)

**Open is unresolved, acknowledged included.** One definition, in
`web/src/utils/alerts.ts` (`isAlertOpen`, `openAlertCount`, `filterAlerts`), and
the server's unresolved query is pinned by `server/store/alerts_open_test.go`.
Acknowledging a signal is a sign-off; only resolving closes it.

Rules that follow from that:

- Counts are derived from the collection in `stores/fleet.ts`, never from a
  second query. The dashboard tile is a beneficiary of the ledger's collection;
  do not add a device-row-based alert count.
- The ledger filters client-side over the whole collection. Server-side filters
  are what made the count chips and the rows disagree — a chip counts the
  population the rows are drawn from, so they must share a predicate.
- A mutation re-reads (`invalidateFleet('alerts')`) rather than patching the row
  in place, and an alert frame over the WebSocket invalidates the collection.
  Optimistic row-patching leaves every *other* surface showing the old number.
- Count chips say what they count and count what is on screen. A chip is a
  control: clicking it sets the filter it names.

## The fleet read model (2.2-2)

`web/src/stores/fleet.ts` owns the shared fleet collections — the unit list,
the patch-compliance view, the policy list, the session ledger — with **one
ref-counted poller per collection**. Pages subscribe through
`useFleet(key, { enabled?, owner? })`:

- A page that *is* the collection (the sessions ledger on the sessions page,
  the unit ledger on the devices page) passes `owner: true`: it reads on mount
  unless the data landed in the last 3 s, which is the same-commit case.
- Everyone else rides along. Opening a unit view after the dashboard issues
  **no** fleet list read — the unit view's own reads are that unit's record and
  its metric history.
- A role that may not read a collection passes `enabled: false` and subscribes
  nothing (a viewer's 403 is not a feature).
- A mutation invalidates: starting a session calls `invalidateFleet('sessions')`
  so a ledger open in another tab is not a minute behind.
- Streaming (`POST /api/devices/:id/stream`) and per-device metric history stay
  per-detail, exactly as they were.

Two rules this layer must keep:

- Do not persist a `fetchedAt` stamp without its data. The store persists the
  unit list only, so a reload reads the rest at mount; persisting stamps without
  data shows an empty ledger for the length of the freshness window.
- The read queue and 429 back-off in `api/client.ts` are the back-stop, not the
  mechanism. If a page needs a poller, it subscribes; it does not add its own
  `setInterval`.

Measured on the seeded e2e fleet (production bundle, `e2e/fleet.spec.ts`): a cold
dashboard mount costs 5 fleet reads (7 with the shell's auth/version reads)
against 14 before 2.1.0; a unit view opened from it costs 2 reads, both about
that unit; two surfaces open sit at roughly 15 reads/min against the documented
100 req/min default (`RATE_LIMIT_PER_MINUTE`, 2.2-3).

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
