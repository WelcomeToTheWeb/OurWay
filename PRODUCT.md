# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: **MSP technicians and MSP admins** — the people who run the console all day at a
desktop, at speed, across **many client fleets and hundreds to thousands of endpoints**. Their
job is triage-then-act: open the console, see what is broken across every client right now,
pick the one that matters, drill in, and do the thing (remote-control it, deploy the patch,
clear the alert, push the file) — then hand off to a colleague mid-shift.

Secondary audiences confirmed by the code's RBAC: `admin` (settings, SSO, users), `manager`
(policies, users, webhooks), `technician` (sessions, files, patches — no admin surfaces), and
read-only viewing. Multiple staff share one installation and one live fleet view.

Deployment reality is **MSP-scale, self-hosted**: scale, filtering, grouping, and dense
information per screen are the primary requirement. Single-homelab use exists but is not the
design target.

## Product Purpose

OurWay is a cross-platform Remote Monitoring & Management tool — monitoring, alerting, patch
management, remote control, file transfer, and user/tenant administration for **Windows, Linux,
and macOS endpoints from one installation**, with one agent, one backend, one web console.

It exists so a small IT shop or MSP can run a real RMM on their own hardware. Success means an
operator can tell, within seconds of opening the console, which of many thousands of devices
needs them — and act on it without leaving the screen.

## Positioning

Open source (MIT), self-hosted via Docker Compose, **no per-agent licensing and no SaaS
dependency** — README's stated position is "combining the best of all top RMMs with a focus on
technology over monetization". The mechanism a neighbouring RMM cannot truthfully copy: the
operator owns the data, the update pipeline, and the agent binaries (source-built, checksum-
verified installer, uninstaller shipped alongside the agent), and one Go agent covers all three
desktop OSes rather than a Windows-first product with bolted-on macOS/Linux support.

## Operating Context

- Console is opened repeatedly through the working day, on desktop, in a browser; live status
  arrives over WebSocket, so the screen is expected to change under the operator.
- Triage loops: fleet-wide view → filtered device list → one device → action. Bulk actions
  (approve/skip/deploy, acknowledge) are real workflows, not decoration.
- Patching runs inside **maintenance windows** with policy schedules (daily/weekly/monthly),
  scoped targets, tag scoping, queued offline deploys, reboot-pending states, and compliance
  reporting per fleet.
- Remote control is a real, in-shift task: web session viewer plus a **native Windows viewer**
  (`cmd/ourway-viewer`) driven by the agent's frame relay.
- Self-hosted installs are run by the operator: `docker compose up -d`, env config via
  `.env.example`, agent installed by a one-liner (`install.sh` / `install.ps1`).
- Console is used in **English, German, and French** — every user-facing string goes through
  `t(...)`, and locale keys are introduced in lockstep across `en`, `de`, `fr`.

## Capabilities and Constraints

Capabilities on hand (shipped, in this repo): device registry + realtime status, metric
collectors (CPU/RAM/disk/network/uptime/processes), heartbeat 15s / metrics 60s / streaming 2s
when a detail page is open, threshold alert engine with acknowledge/assign/resolve, patch scan +
deploy + policies for Windows/macOS/Linux, remote sessions with frame relay, file transfer with
download links, users + RBAC, SSO (OIDC/OAuth2; SAML is an open gap), webhooks with HMAC
signatures + retries, scoped API keys, Redis-backed scaling, CLI + self-contained installer +
uninstaller, native viewer.

Durable engineering constraints (from `CONTRIBUTING.md`, binding on redesign work):

- Verification gate must stay green: `go -C server build|vet|test`, `go -C agent build|vet|test`
  plus `GOOS=windows GOARCH=amd64` and `GOOS=darwin GOARCH=arm64` agent builds,
  `npm ci --prefix web` + `npm run build --prefix web`. CI runs the same gate.
- WebSocket messages cross web/server/agent as a `{type, payload}` envelope, kept identical on
  all three sides.
- Server layering `api → store interface → implementations`; routes are registered in
  `server/api/handlers.go`; interface changes cascade to the mocks in the same change.
- Tests track the shipped contract — when a response shape changes, tests and web decoders are
  updated in the same change.
- i18n lockstep across `en`/`de`/`fr`; never shadow the translator `t`.

Explicitly released by the owner for this redesign: **no incumbent visual language, page layout,
copy, theme, font, palette, or component is required to survive.** The whole console is
re-designed, MSP-first.

Open decision: whether existing English copy is treated as fixed content (the default for
refinement) or is rewritable as part of the redesign — assumed rewritable, since scope is a full
replacement and locale keys must be updated in lockstep if it is.

## Brand Commitments

- Name **OurWay** (repo `WelcomeToTheWeb/OurWay`, MIT-licensed).
- README voice: direct, technical, anti-marketing — "technology over monetization".
- No logo, brand artwork, colour, typographic, or tone constraint is binding.

## Evidence on Hand

- Real product surface: 13 console pages and shared components in `web/src/` (~7,100 lines),
  with tests for Devices, Alerts, Patches, Sidebar, RoleRoute, client, attention/patch-window utils.
- Real product docs: `README.md`, `docs/` (architecture, api, agent, configuration,
  installation, user-guide, roadmap-2.0, release-notes-2.0, changelog, remote-viewer).
- Real, cited review evidence: `buglist.md` — severity-graded findings with fix status and the
  strongest/weakest areas of the system.
- Real locale files: `web/src/i18n/locales/{en,de,fr}.json`.
- Real deployment artifacts: `docker/`, `docker-compose.prod.yml`, `install.sh`, `install.ps1`,
  `scripts/verify.sh`, `.github/workflows/build.yml`.

Absences that future work **must not fabricate**: customers, client/fleet names, device counts,
testimonials, case studies, benchmarks, SLAs, pricing, packaging tiers, roadmap dates beyond
`docs/roadmap-2.0.md`, logos/screenshots of real deployments.

## Product Principles

1. **Triage is the product.** Every screen answers "what needs me, where, and how bad is it"
   before it shows anything else.
2. **Scale is a feature.** Anything that works for 10 devices must stay legible at 1,000 —
   filtering, grouping, sorting, bulk action, and density are first-class, not later polish.
3. **Act in place.** Reading a state and fixing it live on the same screen; no dead-end lists.
4. **Trust the operator's time.** Live data must be legible at a glance, keyboard-reachable,
   and honest about staleness, partial failure, and "we don't know".
5. **Own your infrastructure.** Surface what is self-hosted and what is broken about it plainly —
   versions, connectivity, compliance, queue state — because the operator is the one who fixes it.

## Accessibility & Inclusion

No formal standard has been mandated. Existing practice worth carrying forward: light and dark
theme parity, keyboard shortcuts, a skip-to-content link, visible focus indicators, and labelled
navigation regions.
