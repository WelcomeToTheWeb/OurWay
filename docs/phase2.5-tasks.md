# Phase 2.5: The Ops Plane — Task Breakdown

**Status:** Planned
**Target release:** `v2.5.0`
**Parent plan:** [`roadmap-2.x.md`](roadmap-2.x.md)
**Baseline:** `v2.4.x`

**Why this release exists.** `PRODUCT.md`, Product Principle 5: *"Surface what is
self-hosted and what is broken about it plainly — versions, connectivity,
compliance, queue state — because the operator is the one who fixes it."* Today the
operator cannot see their own infrastructure: the 2026-10 review's standing finding
was *"no dropped-frame counters anywhere"*, and `buglist.md` M8/M10 record silent
drops with log-only visibility. `server/ws/hub.go` has **14 `select { … default: }`
send sites** — each is a place a message can be dropped, and each currently leaves
no number behind.

**Migration numbering.** 2.3.0 ships 7–8, 2.4.0 ships 9–12, so this release uses
**13 (device update state)** and **14 (agent update rollouts)**.

---

## 2.5-0 · Instrument the drop sites (the release's foundation)

**Context:** `server/ws/hub.go` — 14 `select { … default: }` sites; `log.Printf` at
some of them, nothing at others; M10 ("log-and-drop with no metric — degraded
delivery is invisible") is only half-fixed by definition, because a log line is not
observable.

**Do**
- [ ] A counters module in `server/metrics/` (process-local, mutex-guarded; no new
      dependency) with named counters and histograms for:
      WS sends dropped by queue-full (per site name), frames dropped for
      unconnected session owners, alert-evaluation queue full, broadcast
      duplicates/fallbacks (the M8 Redis path), heartbeat/metrics ingest rate,
      alert-engine evaluation latency, session start/end counts, queued-deploy
      depth, deploy outcomes by kind, WS connections by role, active user
      connections, Redis degraded transitions, DB pool open/idle/waiting.
- [ ] Every one of the 14 sites increments a **named** counter, with the site name
      fixed in code so dashboards and docs can reference it (`ws.broadcast.full`,
      `ws.session_frame.unconnected`, `ws.alert_queue.full`, …).
- [ ] Counters also drive the console surface (2.5-2), so they are useful without a
      Prometheus install.
- [ ] Log lines stay, rate-limited (a drop storm must not become a log storm).

**Acceptance criteria**
- A test fills a send queue and asserts the named counter increments and the log
  line is emitted at most once per window.
- Every drop site in `hub.go` maps to a counter named in `docs/architecture.md`,
  listed by name — a new send site with no counter is a review defect.
- Counters expose through both `/metrics` (2.5-1) and the health detail endpoint
  (2.5-2).

---

## 2.5-1 · `/metrics`, with the dependency decision made openly

**Context:** `server/go.mod` has **no** Prometheus client today (50 require lines,
none of them a metrics library).

**Do**
- [ ] **Decide and record:** `github.com/prometheus/client_golang` (standard
      exposition, registry, histograms for free) **vs** a hand-rolled exposition
      writer over the 2.5-0 counters (~200 lines, no new dependency). Record the
      decision in this file and in `docs/architecture.md` before implementing — the
      product's position is that the operator owns and audits the source, so a new
      direct dependency is a conscious call, not a default.
- [ ] `GET /metrics` — **off by default** (`METRICS_ENABLED=false`), and when on it
      requires a bearer token (`METRICS_TOKEN`), because the payload exposes fleet
      shape. Documented in `docs/configuration.md` **and** `.env.example` in the
      same change (the standard set in 2.2: an env var in code and nowhere in docs
      is a bug).
- [ ] Extend `GET /ready`'s shape for machine consumption (dependency reachability
      already reported there) and add `GET /api/system/health` for the console
      (admin; managers scoped per 2.3.1) — health detail is not public.
- [ ] Label discipline fixed in writing: `channel`, `client_id` (from 2.3), `role`,
      `site`. No device-key labels — a device key must never appear in a metrics
      endpoint (C3's rule, applied to the new surface).

**Acceptance criteria**
- With `METRICS_ENABLED=false` (default), `/metrics` returns 404 and nothing in the
  console implies otherwise.
- With it on and no/wrong token: 401. With the token: exposition includes every
  counter from 2.5-0.
- A test asserts no `device_key` value appears anywhere in the exposition body.
- `docs/configuration.md` and `.env.example` both list the variables.

---

## 2.5-2 · Server-health surface in the console

**Do**
- [ ] A Server health panel (admin): server version + commit (already exposed by
      `GET /api/agent/version`), Redis connected/degraded/fallback, DB pool
      open/idle, WS connections by role, per-instance connection count,
      alert-engine latency, queue depths (alert evaluations, queued offline
      deploys), **drop counters with a nonzero state that reads as a problem**, the
      configured rate budget (2.2-3), metric-retention and audit-retention prune
      outcomes.
- [ ] Honest staleness: every figure carries its sample time. "Redis degraded 11
      minutes ago" is a different statement from "Redis is degraded".
- [ ] Multi-instance truth: each server instance reports itself (an instance id in
      the payload), so a fleet of three shows three rows, not a merged average.
- [ ] Nothing here is a coloured dot: the 2.1.0 console language (stamps, tabular
      numerals, ruled field boxes) applies, per `DESIGN.md`.

**Acceptance criteria**
- On a three-instance deployment behind nginx, the panel shows three instances with
  their own connection counts and drop counters.
- Force a degraded Redis (stop the container) and the panel states the transition
  time; restart it and the recovery is visible without a page reload.
- A nonzero drop counter is visually a problem, not a number in a column.

---

## 2.5-3 · Linux patch parity: pacman and zypper

**Context:** `docs/roadmap-2.0.md` §2.2 promised apt, yum/dnf, **pacman** and
**zypper**. `agent/patch/patch.go` implements apt (`scanLinuxUpdates`,
`parseAptSimulate`) and yum/dnf (`rpmTool`, `rpmInstallSpec`); `deployLinuxUpdates`
and `rollbackLinuxUpdates` use the pre-deploy state files
(`savePreDeployState`, `stateFile`) so rollback has something to restore to.

**Do**
- [ ] Scan: `pacman -Qu` (+ `checkupdates` where available) and `zypper -q lu`
      parsed into `Update` rows with `Source: "pacman" | "zypper"`, `ExternalID`,
      `KB`, `Severity`, `Category` per the columns migration 3 added.
- [ ] Deploy: `pacman -U --noconfirm <pkg>` and
      `zypper --non-interactive update <name>` — **only the approved packages**,
      following the existing guard in `deployLinuxUpdates` that refuses an
      unrequested full-system upgrade.
- [ ] Rollback through the same pre-deploy state mechanism as apt/rpm, reporting
      per-package outcomes via `reportResultWithMessage`.
- [ ] Non-interactive discipline: no TTY prompts, no GPG-prompt hangs, explicit
      timeouts; a stuck package manager must fail the deployment, not hang the
      agent.
- [ ] Server side: nothing to change — `Source` is already a string, and
      `docs/api.md` documents updates generically. Verify the console's OS
      breakdown and compliance views handle the new sources (2.1.0 grouped them by
      OS).

**Acceptance criteria**
- Parser tests against **captured real output** fixtures (Arch rolling, Arch with
  AUR present, openSUSE Tumbleweed, openSUSE Leap) — not synthesized strings.
- Deploy tests prove only approved packages are passed to the package manager, and
  an empty approved set fails exactly as apt/dnf does today
  (`ErrNoApprovedUpdates`).
- Rollback restores the recorded versions and reports per-package outcome; a failed
  rollback is visible (`buglist.md` M14's contract).
- A package manager that hangs past its timeout produces a failed deployment, and
  the agent stays alive and continues heartbeating.

---

## 2.5-4 · macOS rollback, honestly

**Context:** `rollbackMacOSUpdates` (`agent/patch/patch.go:625`) returns
"macos: automatic rollback is not supported; restore packages manually
(brew pin/reinstall)". Deploy is `softwareupdate` / `brew` based
(`deployMacOSUpdates`).

**Do**
- [ ] Capture pre-deploy versions for the Homebrew path
      (`brew list --versions`, `brew pin` on the deployed formulae) using the same
      state-file mechanism as Linux.
- [ ] Rollback for Homebrew packages: unpin, `brew install <name>@<recorded>`,
      report per-package outcome. Where the update came from
      `softwareupdate` (system update), rollback stays **explicitly refused** with
      the named reason and the manual steps surfaced in the console — no fake
      success.
- [ ] The deployment result row distinguishes `rolled_back`,
      `rollback_partial`, `rollback_unsupported`, with the reason in the message.

**Acceptance criteria**
- A Homebrew deploy → rollback round trip restores the recorded versions on real
  macOS hardware, and each package's outcome is recorded.
- A `softwareupdate` deploy's rollback returns `rollback_unsupported` with the
  manual steps, and the console shows it as such (M3's lesson: never report success
  for an action that did not happen).
- A rollback that restores 2 of 3 packages reports `rollback_partial` naming the
  one that failed.
- `agent/patch/patch_test.go` gains parser fixtures for `brew list --versions`
  and outcome tests for `rolled_back` / `rollback_partial` / `rollback_unsupported`.
  Note: the macOS rollback refusal is **untested behaviour today** — the suite
  covers Windows/apt/rpm parsing only, so this task adds coverage where none exists.

---

## 2.5-5 · Fleet update posture: what is the fleet actually running

**Context:** `devices.agent_version` is written at **registration**
(`server/api/devices.go`: the register request carries `agent_version`; the
heartbeat handler updates `last_seen`/status only). `OURWAY_UPDATE_CHANNEL` is read
by the agent (`agent/selfupdate/version.go:153`) and **never reported to the
server**. So today the console cannot tell what the fleet runs, what channel each
unit is on, or which units are stuck.

**Do**
- [ ] Migration 13 `devices_update_state`: `agent_version`, `update_channel`,
      `agent_version_reported_at`, `last_update_from`, `last_update_at`,
      `last_update_error`.
- [ ] Heartbeat carries `agent_version` + `update_channel` (additive fields; older
      agents send neither and are shown as **unknown**, never assumed stable).
      Wire-contract note: a new heartbeat field is a **MINOR** capability agents
      must understand, per `docs/versioning.md`.
- [ ] Stalled-update detection: server version > agent version on that agent's
      channel for more than N heartbeat intervals → a named state
      (`update_stalled`), with the reason when the agent reports one.
- [ ] Console: fleet posture ledger — version spread, channel spread, stalled
      units, units reporting `dev`, units below the server's minimum supported
      agent version; per-client (2.3 scope) and per-tag filters.
- [ ] Staged roll-out control: server-side "update rollout" gate (migration 14
      `update_rollouts`) so an operator can release a version to a pilot tag/group
      before the whole fleet, reusing the deployment batching discipline
      (`MaxDevicesPerBatch`). Default behaviour is unchanged: an agent takes an
      update when the server orders above it.
- [ ] The self-update ordering rules from `docs/versioning.md` stay the contract; the
      rollout gate only ever *delays*, never forces a downgrade.

**Acceptance criteria**
- A fleet of mixed versions/channels renders the exact spread, with
  client/tag filters, within one heartbeat interval of a self-update.
- A unit that cannot update (permission-denied binary, blocked download) surfaces as
  `update_stalled` with its reported reason, not as a version the operator has to
  diff by eye.
- A `dev` build and a prerelease server produce **no** update for a stable agent,
  and the console says why (`selfupdate/version.go`'s ordering rules already
  guarantee it; the console must show the state, not invent it).
- Rollout gate test: pilot tag updates, non-pilot units on the same server do **not**
  update until the rollout is opened; opening it updates them without an agent
  restart.
- Older agents (no heartbeat fields) are shown as unknown, proven by a fixture that
  sends the 2.4 heartbeat shape.

---

## 2.5-6 · Queue and window state, legible

**Context:** queued offline deploys exist (`models/queued_deploy.go`), maintenance
windows ship with window fields, time zone, last run, delete (2.1.0), and
`PolicyEngine` drives scan → approve → deploy hourly with `MaxDevicesPerBatch`.
None of it is visible as *state* to an operator today.

**Do**
- [ ] Queue ledger: queued deploys with the device, the reason it is queued
      (offline / outside window / batch limit), its next attempt, its `expires_at`
      and what happens when it expires, and an explicit cancel. Today
      `models/queued_deploy.go` is `(device_id, policy_id)` unique with an
      `expires_at` — the expiry is real and invisible to an operator.
- [ ] Window state on the policy row: next window, last run outcome, devices in
      scope, and a plain statement when a policy will **not** run (disabled, no
      in-scope devices, window in the future).
- [ ] Drop/attempt counters for the queue (2.5-0), so "why didn't this deploy
      happen" has an answer.
- [ ] Everything client-scoped per 2.3.1.

**Acceptance criteria**
- A device offline at deploy time appears in the queue ledger with its next attempt
  and cancels cleanly (row removed, no later deploy).
- A policy whose window has not been reached states the next window in the operator's
  time zone, and the row is explicit about time zone, per the 2.1.0 maintenance-window
  language.
- Cancelling a queued deploy is audited (2.3-6 coverage test keeps being green).

---

## 2.5-7 · Retention and sweep observability, plus a benchmark artifact

**Context:** M11's lesson — a `DeleteOlderThan` method with no caller is not a
retention policy. Metric-history retention ships; 2.3 adds audit pruning. Both are
invisible today.

**Do**
- [ ] Every sweep reports: rows examined, rows deleted, bytes reclaimed, duration,
      last run, next run, and the failure that ended a run.
- [ ] Sweeps counted in 2.5-0 and shown in 2.5-2; a sweep that has not run in
      2× its interval is a named problem state.
- [ ] `scripts/benchmark.sh` producing a committed artifact (the 10,000-device load
      run + list/alert/patch query timings) refreshed per release, with the delta
      against the previous release printed.
- [ ] OWASP Top 10 pass written into `SECURITY.md` (the 2.0 Phase 4.3 item), with
      findings added to `buglist.md` in the audit format this repo now uses.

**Acceptance criteria**
- Metric-history and audit sweeps show last/next run and rows deleted; a
  deliberately-stopped sweep surfaces as a problem within two intervals.
- The benchmark artifact is committed for `v2.5.0` and names the `v2.4.x` baseline
  it is compared against.
- The OWASP pass produces either "no findings" or entries in `buglist.md` with
  severity and status. No silent list.

---

## 2.5-8 · Prove it under load, with the metrics path on

**Do**
- [ ] Run the load tool (10,000 simulated devices, ~100 req/s sustained) **with
      `METRICS_ENABLED=true`** and the counters enabled, with three server
      instances sharing Redis.
- [ ] Report: request failures (must be zero), p50/p95 for device list, alert list,
      patch overview, WS fan-out latency, drop counters (must be zero under nominal
      load, and the suite must show them **nonzero** under an induced overload — a
      counter that never moves is not a counter).
- [ ] Add the load run with metrics to CI as a nightly job (not per-commit), with
      the numbers recorded in the benchmark artifact.

**Acceptance criteria**
- Zero request failures at nominal load; **zero** drop counters at nominal load.
- An induced overload (queue deliberately shrunk) produces counted drops and the
  console shows them, proving the instruments are wired to real paths.
- Three instances: device connected to instance 2 is reachable from a console on
  instance 1 (the H2 cross-instance guarantee, re-proven with the new surface).
- Alert-engine latency p95 stays under the read deadline that produced M5.

---

## Release gate for `v2.5.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/i18n-parity.sh
scripts/benchmark.sh                       # new in 2.5-7
scripts/release.sh --check 2.5.0
```

Plus, non-negotiable for this release:

- [ ] All 14 drop sites in `server/ws/hub.go` carry named counters, and a test moves
      each one (2.5-0).
- [ ] Load run with metrics on: zero failures, zero drops at nominal load, drops
      counted and visible under induced overload (2.5-8).
- [ ] Fleet posture correct for mixed versions/channels, with stalled units named and
      older agents shown as unknown (2.5-5).
- [ ] pacman + zypper scan/deploy/rollback green against captured fixtures, with
      only approved packages passed to the package manager (2.5-3).
- [ ] macOS Homebrew rollback round trip on real hardware; a `softwareupdate`
      rollback reports `rollback_unsupported`, never success (2.5-4).
- [ ] `/metrics` off by default, token-gated, with no device key in the exposition
      (2.5-1).
- [ ] New env vars in `docs/configuration.md` **and** `.env.example`; new routes in
      `docs/api.md` with the 2.2-7 route↔docs check green; strings in all three
      locales.

**Wire contract:** a new heartbeat field (`agent_version`, `update_channel`) is a
new capability agents must understand → **MINOR**. Older agents keep working and are
shown as unknown. `/metrics` and the health endpoints are additive. No agent↔server
pairing break, no schema-level breaking change for agents.

---

## Deferred inside 2.5 (so the release stays shippable)

- Tracing/OTel and distributed spans across instances (the counters answer the
  operator's questions; traces answer an engineer's, at a real complexity cost).
- Alerting on OurWay's own health to a human channel (the 2.4-6 abstraction is what
  makes this cheap; 2.6+).
- Prometheus Grafana dashboards as shipped artifacts (document the metric names; do
  not ship a dashboard we cannot maintain).
- Log shipping/centralization, log rotation policy for agent logs.
- pacman/zypper *transaction* rollback (version restore is the scope; full
  transaction replay is not).
- Multi-region HA and read replicas (roadmap 2.0 architecture sketch; deferred out of
  2.x in `docs/roadmap-2.x.md`).
