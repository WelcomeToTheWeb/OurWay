# Phase 2.2: Make the claim true — Task Breakdown

**Status:** Planned
**Target release:** `v2.2.0`
**Parent plan:** [`roadmap-2.x.md`](roadmap-2.x.md)
**Baseline:** `v2.1.0`

The theme is narrow on purpose: **every capability the product advertises works on
all three platforms, and the console stops throttling itself.** No new product
surface is added here. Findings referenced below are numbered as in
[`buglist.md`](../buglist.md) (the "Re-audit at v2.1.0" section).

Work in the order given: 2.2-1 and 2.2-2 are independent and can run in parallel;
2.2-3 only makes sense after 2.2-2 has been measured; 2.2-8 lands first because the
rest need somewhere to be proven.

---

## 2.2-0 · Prove it: test harness that can fail a release

**Why first:** every acceptance criterion below is "a test proves it". Today there
is no end-to-end layer to write them into, and Phase 4.3 of the 2.0 roadmap is the
unfinished phase.

**Do**
- [ ] Playwright project under `web/e2e/`, run in CI (`tests` job) against the
      built bundle + a real server + PostgreSQL.
- [ ] Seed fixtures (devices, alerts, updates) for the suite — review-scaffolding
      style, kept out of the shipped bundle, and never presented as product data.
- [ ] Smoke flows, in this order: login → device list renders rows → open device
      detail → alert acknowledge → patch approve.
- [ ] Re-measure coverage for `server/` and `agent/` and commit the number; add a
      CI step that fails if coverage drops below the recorded baseline (ratchet).
- [ ] Agent smoke test per OS in CI (Linux + Windows + macOS runners): install as
      service, heartbeat lands, metrics land, session capture starts.
- [ ] `go mod tidy -C agent` — prune the transitive requires left in `go.sum` by
      the WebRTC dead-code removal (2026-10-26, `buglist.md` M15).

**Acceptance criteria**
- A red e2e smoke flow blocks the merge; the suite runs in under 6 minutes.
- Coverage baseline recorded in the repo, with the ratchet failing on a decrease.
- No test asserts on fixture data outside the suite's own seed.

---

## 2.2-1 · macOS remote control: real input

**Findings:** `buglist.md` H8. `agent/session/input_darwin.go` — `synthesizeKey`
and `synthesizeMouse` log and return. Capture (`screen_capture_darwin.go`),
geometry (`geometry_darwin.go`), framing and the session control path are real, so
a macOS session opens and streams while discarding every keystroke and click.

**Do**
- [ ] `synthesizeKey(key, event)` via `CGEventCreateKeyboardEvent` +
      `CGEventPost(kCGHIDEventTap, …)`, with the key-code table matching the
      browser `KeyboardEvent.key`/`code` values the web client sends
      (`web/src/components/SessionView.tsx` sends `{event, key, code}`).
- [ ] `synthesizeMouse(event, x, y, button, delta)` via
      `CGEventCreateMouseEvent` for move / down / up / click and the scroll-wheel
      event for `delta`. Reuse `toScreenCoords(x, y)` — `x`/`y` already arrive as
      **0–100 percentages of the captured frame** (`agent/session/geometry.go`).
- [ ] Detect and report the Accessibility (TCC) permission: `AXIsProcessTrusted()`
      (+ the prompt option), and surface the state on the device detail page so an
      operator learns *why* control is inert before a technician finds out live.
      This needs a new agent → server status field, carried in the existing
      `{type, payload}` envelope (`CONTRIBUTING.md`).
- [ ] Note the deployment reality in `docs/agent.md`: TCC grants are per binary
      and per stable path — a launchd agent that self-updates to a new path loses
      the grant, and ad-hoc-signed binaries change identity. Document the
      codesign + fixed-path requirement for the update pipeline.

**Acceptance criteria**
- From a browser session on a macOS unit: type, click, drag, right-click, and
  scroll all act on the remote machine; measured round-trip latency reported in
  the agent smoke test.
- Without the Accessibility grant, the console states "macOS unit needs
  Accessibility permission for control" on the device detail page and in the
  session panel — not a silent no-op.
- With the grant, control works with no console warning.
- New test in `agent/session/` for the key-code table (pure mapping, no display
  needed) and for coordinate mapping at 0 / 50 / 100 on a fractional scale.
- `GOOS=darwin GOARCH=arm64` and `GOOS=windows GOARCH=amd64` agent builds green.
- Strings in `en`/`de`/`fr` in lockstep.

**Wire contract:** a new status field in the agent's metrics/status payload →
MINOR. Older agents simply do not report it, and the console must treat "not
reported" as unknown, never as granted.

---

## 2.2-2 · Shared fleet store: end the per-page polling

**Finding:** `.impeccable/review/verdict.md`, debt #1 — polling is per-page; a
shared fleet store cuts request pressure roughly in half.

**Do**
- [ ] One fleet read model owning the device list, alert rollups, and metric
      history — `web/src/stores/devices.ts`, `metrics.ts`, and `fleetHistory.ts`
      become views on it instead of three independent pollers.
- [ ] A single fleet poller with a page-driven subscription, so opening device
      detail adds **zero** new list reads.
- [ ] Streaming (`POST /api/devices/:id/stream`) stays per-detail and is unaffected.
- [ ] Measure the dashboard mount cost before and after, on the same fixture set.

**Acceptance criteria**
- Dashboard mount issues **≤ 10** GET requests measured in the browser (the
  finish review found 14 and a 429 on mount at 100 req/min with burst 10).
- Two console tabs open for 10 minutes trip **no** 429 on a 100 req/min budget.
- Devices, Alerts, Dashboard and DeviceDetail render from the store, and
  `web/src/api/readThrottle.test.ts` still passes with the throttle as the backstop,
  not the mechanism.
- A vitest test proves a second subscriber does not add a list request.

---

## 2.2-3 · Make the rate budget an operator's dial, not a constant

**Finding:** `server/api/handlers.go:38` builds `NewRateLimiter(time.Minute, 100)`
and `server/api/ratelimit.go` derives burst as `maxCount/10` = 10. It is documented
as the console's design budget, with nothing to tune.

**Do**
- [ ] `RATE_LIMIT_PER_MINUTE` (default 100) and `RATE_LIMIT_BURST` (default
      `maxCount/10`) in `server/config/config.go`, used by the limiter
      constructor. Keep Redis-backed limiting behaviour identical.
- [ ] Document both in `docs/configuration.md` and `.env.example`, including the
      "raising it is a server-side option for big shifts, not a requirement" note
      from the finish review.
- [ ] The console's shift bar prints the configured budget, so an operator knows
      the ceiling they are operating under.

**Acceptance criteria**
- Setting `RATE_LIMIT_PER_MINUTE=500` changes the observed ceiling; a test in
  `server/api/` proves the limiter reads config, not a literal.
- `docs/configuration.md` and `.env.example` both list it; the console prints it.
- The rate-limit state machine (429 → named console state → 60 s back-off → polls
  rest) is unchanged and still tested.

---

## 2.2-4 · One answer to "how many units need me"

**Finding:** `.impeccable/review/verdict.md`, debt #2 — `GET /devices` carries
alert state, so the dashboard's flagged count disagrees with the alert ledger's
open count whenever severities differ.

**Do**
- [ ] Pick the alert ledger as the single source of truth for counts, and derive
      the dashboard's "needs attention" figures from it
      (`web/src/utils/attention.ts` and the server's alert rollup, not the device
      row's derived flags).
- [ ] Make the server's device-list alert summary and the alert list agree by
      construction — same severity filter, same "open" definition, one query.
- [ ] Show the definition on screen: the dashboard figure is labelled with what it
      counts (open, unacknowledged, severity ≥ …), not a bare number.

**Acceptance criteria**
- On the same fixture set, dashboard flagged count == alert ledger open count for
  every severity filter, proven by a test on both sides (vitest + a server store
  test).
- Deliberately inconsistent fixtures (an acknowledged critical, a resolved alert
  on an offline device) produce equal counts.
- The label stating the definition appears in all three locales.

---

## 2.2-5 · i18n: close the gap, then make it un-closeable

**Findings:** `.impeccable/review/verdict.md`, debt #3. Measured on `v2.1.0`:
`en` has 383 keys and **`de` and `fr` are each missing exactly 2**
(`patchPolicies.scopeDevicesHelp`, `patchPolicies.scopeTagsHelp`) — a bug per
`CONTRIBUTING.md` ("a key missing from any locale is a bug"). Hardcoded English
JSX text nodes: `Settings.tsx` 5, `DeviceDetail.tsx` 2, `Alerts.tsx` 1,
`Devices.tsx` 1, `PatchPolicies.tsx` 1, `Sessions.tsx` 1.

**Do**
- [ ] Add the 2 missing keys to `de` and `fr`.
- [ ] Extract the hardcoded nodes listed above through `t(...)`.
- [ ] Add `scripts/i18n-parity.sh` (or a `web` npm script) that flattens the three
      locale files and **fails CI** on any missing or extra key, and reports the
      count of untranslated JSX text nodes so the number only goes down.
- [ ] Keep the `t` shadowing rule enforced (`CONTRIBUTING.md`): no local named `t`.

**Acceptance criteria**
- Parity script passes: 383 keys in all three locales, 0 missing, 0 extra.
- 0 hardcoded English text nodes in `web/src/pages` (measured by the script, not
  by eye).
- A deliberately dropped key in one locale fails the CI gate.

---

## 2.2-6 · Say "no samples yet"

**Finding:** `.impeccable/review/verdict.md`, debt #4 — history micro-fields with
fewer than 3 samples render an empty ruled slot with no explanation.

**Do**
- [ ] Render a stated empty state below the sample threshold, and the same for a
      unit with no history at all (a freshly enrolled unit is the common case).
- [ ] Keep the slot's geometry identical to a filled one — the row must not reflow.

**Acceptance criteria**
- A unit with 0, 1, 2 and 120 samples renders legibly, with the empty cases
  carrying the "no samples yet" string in `en`/`de`/`fr`.
- A vitest test covers the 0/1/2/3 boundary.

---

## 2.2-7 · Documentation completeness

**Context:** the 2026-10-26 audit corrected the *false* documentation
(`buglist.md` M16 — endpoints that were never registered, query-param WebSocket
auth after C5 removed it, the known `JWT_SECRET` template value, the README's
WebRTC claims). What is left is documentation for shipped surface that has **never
been documented**.

**Do**
- [ ] `docs/api.md`: users and roles, device tags, the streaming endpoints (done
      2026-10-26), installers, `GET /api/agent/version`, `/health` + `/ready`,
      API keys, sessions list/quality (added 2026-10-26), and reboot — every route
      in `server/api/handlers.go` must appear.
- [ ] Add a CI check that diffs registered routes against the endpoint headings in
      `docs/api.md`, so this cannot drift again.
- [ ] `docs/user-guide.md`: macOS Accessibility grant for control (2.2-1), and the
      view-only caveat removed once control works.
- [ ] `docs/agent.md`: the TCC/codesign requirement for a self-updating launchd
      agent.
- [ ] `docs/changelog.md`: the 2.2.0 section at release time, generated per
      `docs/versioning.md` (release notes come from that section).

**Acceptance criteria**
- Every route registered in `server/api/handlers.go` appears in `docs/api.md`,
  proven by the CI diff check.
- No documented endpoint is missing from the router — the check fails in both
  directions.

---

## 2.2-8 · macOS parity claims, verified end to end

**Do**
- [ ] Real macOS hardware (Apple Silicon and Intel) run of: install → enroll →
      heartbeat → metrics → session view → session **control** → self-update →
      uninstall.
- [ ] Record the measured session latency and the permission-grant behaviour after
      a self-update, in `docs/release-notes` / changelog prose for 2.2.0.

**Acceptance criteria**
- The full loop completes on both architectures with an update in the middle.
- Control survives the self-update (the TCC/codesign case from 2.2-1) or the
  console states the loss plainly.
- README's "Known gaps" section loses the macOS input line and gains nothing new.

---

## Release gate for `v2.2.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/i18n-parity.sh                     # new in 2.2-5
scripts/release.sh --check 2.2.0           # every check + the full gate, publishes nothing
```

Plus, non-negotiable for this release:

- [ ] macOS remote control verified on real hardware (2.2-8), not on a build.
- [ ] Dashboard mount ≤ 10 reads, measured in a browser (2.2-2).
- [ ] `scripts/i18n-parity.sh` green: 383/383/383 keys, 0 hardcoded nodes.
- [ ] Route↔docs diff check green (2.2-7).
- [ ] The Playwright smoke flows are green and are the reason we believe the above.

Tag with `scripts/release.sh 2.2.0` — it refuses a dirty tree, a non-`main`
branch, a HEAD that is not the pushed tip, a version not greater than the latest
tag, and a red gate. Ship 2.2.x patches as they arrive; security fixes ship alone.

---

## Out of scope for 2.2 (deliberate)

Client tenancy and the audit log (2.3.0) · alert thresholds and notification
channels (2.4.0) · generic OIDC + discovery and SAML (2.4.0) · self-metrics and
`/metrics` (2.5.0) · pacman/zypper and macOS rollback (2.5.0).
