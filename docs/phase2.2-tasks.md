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
- [x] Playwright project under `web/e2e/`, run in CI (`.github/workflows/e2e.yml`)
      against the built bundle (`vite preview`) + a real server + PostgreSQL +
      Redis. 13 tests, ~13 s per run.
- [x] Seed fixtures (devices, alerts, updates) for the suite — review-scaffolding
      style, kept out of the shipped bundle, and never presented as product data
      (`e2e/fixtures.ts`, `e2e/global-setup.ts`): data arrives through the
      agent-facing API (`/api/agent/register|heartbeat|metrics|updates`), the admin
      is bootstrapped through first-run registration, and a real WS client
      (`e2e/support/fake-agent.mjs`) holds device connections for the run.
- [x] Smoke flows, in this order: login → device list renders rows → open device
      detail → alert acknowledge → patch approve → start and end a remote session.
- [ ] Re-measure coverage for `server/` and `agent/` and commit the number; add a
      CI step that fails if coverage drops below the recorded baseline (ratchet).
      **Done 2026-10-26:** `coverage.baseline.json` (server 39.0, agent 30.0) and
      `scripts/coverage.sh`, run in the `tests` job and in the local gate.
- [ ] Agent smoke test per OS in CI (Linux + Windows + macOS runners): install as
      service, heartbeat lands, metrics land, session capture starts.
      **Done 2026-10-26:** `scripts/agent-smoke.sh` +
      `.github/workflows/agent-smoke.yml` (matrix: ubuntu / windows / macos).
      The Linux path is validated end to end locally (Xvfb + scrot): enroll →
      online → heartbeat → metrics → **session reaches active on real frames** →
      enrollment-secret gate both ways. Windows/macOS run the same script on their
      runners; the service tier cannot run in a container, so its proof is the CI
      run. macOS frames are deliberately not asserted (TCC cannot be granted
      non-interactively) and the script prints that instead of passing them off.
- [x] `go mod tidy -C agent` — prune the transitive requires left in `go.sum` by
      the WebRTC dead-code removal (2026-10-26, `buglist.md` M15). 127 lines gone.

**Delivered by the harness, not by the plan** — each recorded in `buglist.md`: the
`localhost` WebSocket 403 (H9, fixed with the harness), `POST /api/users` answering
500 for a duplicate e-mail (M18), and the surviving `window.confirm` call sites
(M19 → 2.2-9 below). The harness also proved it needs 2.2-3: it is one user making
many reads, so it starves on the old 100/min constant.

**Acceptance criteria**
- [x] A red e2e smoke flow blocks the merge; the suite runs in under 6 minutes.
- [ ] Coverage baseline recorded in the repo, with the ratchet failing on a decrease.
- [x] No test asserts on fixture data outside the suite's own seed.

---

## 2.2-1 · macOS remote control: real input · ✅ delivered

**Delivered:** CGEvent posting in `agent/session/input_darwin_cgo.go` (key,
mouse move/down/up/click with click-count, scroll), the browser→macOS key table
in `agent/session/macosmapping.go`, modifier state (`ctrl/shift/alt/meta`)
carried on every key event on all three platforms, `AXIsProcessTrusted` read on
the agent and reported as `input_capability` (+ `capture_state`) on the metrics
envelope, migration 7 (`devices.input_capability`, `devices.capture_state`), the
state rendered on the unit view and in the session panel, `--grant-input-permission`,
`scripts/agent-smoke.sh --expect-capability / --expect-latency-ms`, and a macOS CI
job that compiles the cgo agent for arm64 **and** amd64 and asserts the unit
reports a **stated decision** (`granted` or `denied`) — the macOS images ship with
Accessibility granted to the runner user, so `granted` is the honest answer there and
`unknown`/`unavailable` from a cgo build is the failure the assertion catches.
Measured control round-trip:
**0.034 s** (server → agent, same host, Linux smoke run).
Remaining for the human gate: the physical Mac (type/click/drag/right-click/scroll
with the grant, and control surviving a self-update at a fixed path).

Shipped with it, because the acceptance criteria are harness criteria:
`.github/workflows/build.yml` no longer cross-compiles darwin — the macOS agent,
remote exe and installer are built on a macOS runner with `CGO_ENABLED=1`
(arm64 + amd64) and merged into the same artifact the server image and the release
download, so the Mac binary the console offers for download is one that can
control the unit. `scripts/agent-smoke.sh` gained `--expect-capability` and
`--expect-latency-ms`; the macOS smoke job runs the cgo binary and asserts it reports a
decision (the runner is granted, so `granted` — a cgo build that reports nothing is the
bug). The e2e fake agent now reports
capabilities on the metrics wire, the seed fleet carries all four states, and two
specs assert the console states them (unit view, `e2e/devices.spec.ts`; session
panel, `web/src/components/SessionView.test.tsx`). The panel's assertion is a
component test because opening a console session needs the unit's wire live at the
instant of the click: CI's darwin unit answers `POST /api/devices/:id/sessions`
with 409 on every attempt of four, across four runs, while the same fleet's REST
surface is healthy. A browser test that cannot start the thing it asserts is a test
that quietly never runs — so the copy and its condition are asserted on the
component that renders them, next to the prop that decides them.
`scripts/i18n-parity.py` is in the gate — building it found two locale keys
missing from `de`/`fr`, which is the debt 2.2-5 is about.

**Findings:** `buglist.md` H8. `agent/session/input_darwin.go` — `synthesizeKey`
and `synthesizeMouse` log and return. Capture (`screen_capture_darwin.go`),
geometry (`geometry_darwin.go`), framing and the session control path are real, so
a macOS session opens and streams while discarding every keystroke and click.

**Do**
- [x] `synthesizeKey(key, event)` via `CGEventCreateKeyboardEvent` +
      `CGEventPost(kCGHIDEventTap, …)`, with the key-code table matching the
      browser `KeyboardEvent.key`/`code` values the web client sends
      (`web/src/components/SessionView.tsx` sends `{event, key, code}`).
- [x] `synthesizeMouse(event, x, y, button, delta)` via
      `CGEventCreateMouseEvent` for move / down / up / click and the scroll-wheel
      event for `delta`. Reuse `toScreenCoords(x, y)` — `x`/`y` already arrive as
      **0–100 percentages of the captured frame** (`agent/session/geometry.go`).
- [x] Detect and report the Accessibility (TCC) permission: `AXIsProcessTrusted()`
      (+ the prompt option), and surface the state on the device detail page so an
      operator learns *why* control is inert before a technician finds out live.
      This needs a new agent → server status field, carried in the existing
      `{type, payload}` envelope (`CONTRIBUTING.md`).
- [x] Note the deployment reality in `docs/agent.md`: TCC grants are per binary
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

## 2.2-2 · Shared fleet store: end the per-page polling · ✅ delivered

**Delivered:** `web/src/stores/fleet.ts` is the read model (unit list, patch
compliance, policy list, session ledger) with one ref-counted poller per
collection; `useFleet(key, { enabled, owner })` is the subscription, `web/src/stores/devices.ts`
and the per-page `setInterval` pollers are gone, and `useRateLimitAwarePoll` is
gone with them (rate-limit rest moved into the shared poller). Measured on the
seeded e2e fleet with the production bundle (`web/e2e/fleet.spec.ts`): cold
dashboard mount **5 fleet reads / 7 total** (14 before 2.1.0), a unit view opened
from it adds **zero** fleet reads (its own record + metric history only), two
surfaces open ≈ **15 reads/min** against the documented 100. 8 vitest cases in
`web/src/stores/fleet.test.ts` cover the subscriber claims, including the two
that the browser cannot: an invalidated collection re-reads, and a role that may
not read a collection subscribes nothing.

Two things the harness caught on the way, both kept as rules in
`CONTRIBUTING.md`: persisting a freshness stamp without its data shows an empty
ledger for the length of the window (the reload test in `e2e/sessions.spec.ts`
found it), and measuring a mount by navigating *after* signing in counts the
landing mount too — mount costs are measured on a cold context.

**Finding:** `.impeccable/review/verdict.md`, debt #1 — polling is per-page; a
shared fleet store cuts request pressure roughly in half.

**Do**
- [x] One fleet read model owning the device list, alert rollups, and metric
      history — `web/src/stores/devices.ts`, `metrics.ts`, and `fleetHistory.ts`
      become views on it instead of three independent pollers.
- [x] A single fleet poller with a page-driven subscription, so opening device
      detail adds **zero** new list reads.
- [x] Streaming (`POST /api/devices/:id/stream`) stays per-detail and is unaffected.
- [x] Measure the dashboard mount cost before and after, on the same fixture set.

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
- [x] `RATE_LIMIT_PER_MINUTE` (default 100) and `RATE_LIMIT_BURST` (default
      `maxCount/10`) in `server/config/config.go`, used by the limiter
      constructors (`NewRateLimiterWithBurst`, `NewRedisRateLimiterWithBurst`; the
      Redis-backed path is behaviourally identical). `SetupRouter` takes the budget
      from `main.go`, so no literal remains in the route table.
- [x] Document both in `docs/configuration.md` and `.env.example`, including the
      "raising it is a server-side option for big shifts, not a requirement" note
      from the finish review.
- [x] The console's shift bar prints the configured budget: `GET
      /api/agent/version` carries it, `Layout.tsx` renders `budget N/min`, and the
      suite asserts it.

**Acceptance criteria**
- [x] Setting `RATE_LIMIT_PER_MINUTE=500` changes the observed ceiling; a test in
      `server/api/` proves the limiter reads config, not a literal —
      `ratelimit_test.go` (explicit burst honoured, default burst is a tenth,
      per-user isolation, refill after the window) and
      `server/config/config_test.go` (env defaults).
- [x] `docs/configuration.md` and `.env.example` both list it; the console prints it.
- [x] The rate-limit state machine (429 → named console state → 60 s back-off →
      polls rest) is unchanged and still tested.

---

## 2.2-4 · One answer to "how many units need me" · ✅ delivered

**Delivered:** the definition is **open = unresolved, acknowledged included**,
stated in one client helper (`web/src/utils/alerts.ts`: `isAlertOpen`,
`openAlertCount`, `filterAlerts`) and pinned on the server by
`server/store/alerts_open_test.go` (acknowledging keeps an alert in the
unresolved query; only resolving removes it). The ledger is now the *owner* of
the `alerts` collection in the shared fleet store, filters run client-side over
that collection, and the dashboard's tile rides on the same data with the same
predicate — it no longer counts units whose `status === 'alert'`, which was the
third population. The count chips measure the searched population, so a chip and
the rows it summarises are one predicate; an alert frame over the WebSocket
invalidates the collection, so the counts move on the event that changed them.
Copy states the definition in all three locales (`dashboard.activeAlertsHint`:
"unresolved · acknowledged included"). Proven by `e2e/alerts.spec.ts` (tile ==
ledger == server, acknowledging does not move the number, resolving moves both
by one) and by `web/src/pages/Alerts.test.tsx` (chip counts equal row counts;
mutations re-read the collection rather than patching a row).

**Finding:**

**Finding:** `.impeccable/review/verdict.md`, debt #2 — `GET /devices` carries
alert state, so the dashboard's flagged count disagrees with the alert ledger's
open count whenever severities differ.

**Do**
- [x] Pick the alert ledger as the single source of truth for counts, and derive
      the dashboard's "needs attention" figures from it
      (`web/src/utils/attention.ts` and the server's alert rollup, not the device
      row's derived flags).
- [x] Make the server's device-list alert summary and the alert list agree by
      construction — same severity filter, same "open" definition, one query.
- [x] Show the definition on screen: the dashboard figure is labelled with what it
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
- [ ] Add `scripts/i18n-parity.py` (or a `web` npm script) that flattens the three
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
- [x] `docs/user-guide.md`: macOS Accessibility grant for control (2.2-1), and the
      view-only caveat removed once control works.
- [x] `docs/agent.md`: the TCC/codesign requirement for a self-updating launchd
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

## 2.2-9 · Finish the confirm-sheet migration (browser dialogs are banned)

**Finding:** `buglist.md` M19. `DESIGN.md` — "Confirmation is a state of the
control, not a browser dialog… `window.confirm()` is banned". Ten call sites used
the native dialog: End session (`Sessions.tsx:33`), Delete device
(`DeviceDetail.tsx:251`), delete user (`Users.tsx:85`), delete webhook
(`Webhooks.tsx:85`), delete SSO binding (`SSO.tsx:74`), delete policy
(`PatchPolicies.tsx:124`), purge metrics history and delete account
(`Settings.tsx:64,80`), delete and revoke API key (`Settings.tsx:136,146`).

**Correction to the finding as written in 2.2 planning:** 2.1.0 did **not** ship
`ConfirmSheet`. The component existed only in `DESIGN.md` prose and in the buglist —
no such file was in `web/src/components/` at `v2.1.0` or in any earlier commit. The
sheet is built by this task; the migration is from a native dialog to a component
that had to be written first.

Native dialogs are unthemeable, unlocalisable (the copy is string concatenation in
English, so `de`/`fr` users get English on the one control that asks them to destroy
data), and they are auto-dismissed by automation — the e2e suite had to accept a
dialog to end a session at all.

**Do**
- [x] Route every remaining destructive action through `ConfirmSheet` with the
      named target in the title ("End session `a1b2c3d4`?"), the consequence in the
      body, and the verb on the confirm button.
      **Done 2026-10-27:** `web/src/components/ConfirmSheet.tsx` (sheet anatomy: stamp-red
      head, consequence body, Cancel + verb; Escape cancels, focus opens on Cancel and
      returns to the control that asked, both controls rest while the act is in flight)
      and `web/src/utils/confirmSheet.ts` (`confirmRequest`/`pendingConfirm`, so a sheet
      can only confirm the act that opened it). `Settings` also lost its `window.alert`:
      the clear-history result is a line on the sheet.
- [x] Move the copy into `en`/`de`/`fr` (interpolated target, not concatenation) so
      the 2.2-5 parity check covers it — `confirm.<action>.{title,body,action}` for all
      ten acts, 436 key paths in lockstep.
- [x] Add one e2e assertion that an inline confirmation is required before the write
      happens — `sessions.spec.ts` clicks End, asserts the sheet names the session and
      the row is still there, cancels (writes nothing), then confirms and the row goes.
- [x] Make the ban a gate step: `scripts/browser-dialogs-check.sh` fails on any
      `confirm(` / `alert(` / `prompt(` call in `web/src` — in the `tests` job and in
      `scripts/release.sh`. It found one more thing: a test fixture helper named `alert`
      in `fleet.test.ts` shadowing the banned global (now `alertRow`).

**Acceptance criteria**
- [x] `grep -rn "confirm(" web/src --include=*.tsx` finds no `window.confirm` and no
      bare `confirm(` in a page component — and the check that says so runs in CI.
- [x] The destructive copy exists in all three locale files and the parity check fails
      a removal (`scripts/i18n-parity.py`: 436/436/436).
- [x] `web/e2e/sessions.spec.ts` no longer registers a dialog handler: ending a session
  clicks the control and confirms in the page.

---

## Release gate for `v2.2.0`

```sh
go -C server build ./... && go -C server vet ./... && go -C server test ./...
go -C agent  build ./... && go -C agent  vet ./... && go -C agent  test ./...
GOOS=windows GOARCH=amd64 go -C agent build ./...
GOOS=darwin  GOARCH=arm64 go -C agent build ./...
npm ci --prefix web && npm run build --prefix web && npm run test:run --prefix web
npm run typecheck --prefix web
scripts/coverage.sh                        # new in 2.2-0 (the floor, not a wish)
python3 scripts/i18n-parity.py             # new in 2.2-5 (named .sh in the plan; the script is Python)
bash scripts/browser-dialogs-check.sh      # new in 2.2-9 (DESIGN.md §6, enforced)
scripts/release.sh --check 2.2.0           # every check + the full gate, publishes nothing
```

Plus, non-negotiable for this release:

- [ ] macOS remote control verified on real hardware (2.2-8), not on a build.
- [x] Dashboard mount ≤ 10 reads, measured in a browser (2.2-2): 7 on the seeded
      e2e fleet, asserted by `web/e2e/fleet.spec.ts`.
- [x] `python3 scripts/i18n-parity.py` green: 436 key paths in all three locales
      (383 at the finding, plus what 2.2-1/2.2-4/2.2-9 added), 0 missing, 0 extra.
- [ ] Route↔docs diff check green (2.2-7).
- [x] Destructive confirmation happens in the page: 0 native dialogs in `web/src`,
      asserted by `scripts/browser-dialogs-check.sh` (2.2-9).
- [x] The Playwright smoke flows are green and are the reason we believe the above.

Tag with `scripts/release.sh 2.2.0` — it refuses a dirty tree, a non-`main`
branch, a HEAD that is not the pushed tip, a version not greater than the latest
tag, and a red gate. Ship 2.2.x patches as they arrive; security fixes ship alone.

---

## Out of scope for 2.2 (deliberate)

Client tenancy and the audit log (2.3.0) · alert thresholds and notification
channels (2.4.0) · generic OIDC + discovery and SAML (2.4.0) · self-metrics and
`/metrics` (2.5.0) · pacman/zypper and macOS rollback (2.5.0).
