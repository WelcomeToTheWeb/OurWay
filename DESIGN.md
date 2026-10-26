# OurWay Console — Design Contract

Direction: **The Fleet Service Decal Sheet** (Impeccable Operate, code-led, seed `f54a5861c7ddb02a`).
Scope: the whole web console — shell, monitoring, management, administration.

---

## 1. Thesis

Every device in this product is a **serviced unit with a stamped record**: last done, next
due, interval, signed off. The console is the shop's service sheet, not a dashboard of
gauges. That single idea decides what a screen is for, how dense it may be, and what a
state looks like.

The category default it replaces was: four rounded stat cards, a device grid of coloured
dots, a blue accent, and a page of generic cards. Nothing from the incumbent look was
preserved by obligation; the product's facts were.

## 2. The world

| Role | Day | Night | Notes |
|---|---|---|---|
| Ground (label stock / graphite) | `#EFE7CB` | `#16181C` | `--bg-primary` |
| Sheet (a raised panel) | `#F5EFDD` | `#1D2026` | `--bg-card`, no shadow |
| Ink (nav rail, primary button) | `#1A1A17` | `#E8E4D8` | `--ink`, `--ink-plate` |
| Text | `#1A1A17` | `#E8E4D8` | `--text-primary` |
| Rule (hairline) | `#C9BE9C` | `#33373F` | `--rule`, `--rule-strong` |
| Field (ruled box fill) | `#E7DFC6` | `#22262D` | `--field` |
| Stamp red (destructive, breach) | `#C8102E` | `#EF6A61` | `--stamp` |
| Service blue (acting, links) | `#1B4F9C` | `#7FB2E5` | `--service` |
| Due amber (scheduled, pending) | `#E0A200` | `#E0A200` | `--due` |
| Online / offline / warning / error | green / grey / amber / red inks | same, brighter | `--online`, `--offline`, `--warning`, `--error` |

Rules of the world, non-negotiable:

- **No faux paper.** No textures, no gradients-as-paper, no drop shadows. Depth comes from
  hairline rules and field fills only.
- **A state is a stamp, never a dot.** Status is typeset in a bordered, small-caps stamp
  (`Stamp`, `StatusBadge`, `LinkStamp`); severity, compliance, sign-off and link state all
  read as stamps.
- **Numerals are tabular** (JetBrains Mono) everywhere a number is scanned: counts,
  percentages, bytes, windows, durations, timestamps.
- **Captions are condensed small caps** (Archivo Narrow) with `0.09em` tracking, used for
  field labels, table heads, units, and secondary copy.
- **Times are stated in 24-hour UTC or ISO dates.** No `toLocaleString()` in a ledger.
- Corners are 2–4px (letterpress), never pill-shaped controls.

## 3. Type

| Use | Face | Where |
|---|---|---|
| Body | Barlow 400/600 | prose, buttons, form text |
| Captions | Archivo Narrow 500/600, small caps | labels, heads, stamps, guide words |
| Numerals | JetBrains Mono (tabular) | every figure, ledger columns, charts |
| Display | Barlow 700 (tight) | page titles (26px) |

Self-hosted WOFF2 in `web/src/assets/fonts/` (`fonts.css`), latin + latin-ext subsets,
loaded from Google Fonts; all three faces are SIL Open Font License 1.1. No remote font
requests at runtime — the console is usually on a LAN with no internet.

## 4. Shell

- **Ink nav rail** (`Sidebar.tsx`): brand plate, three caption groups — Monitor, Manage,
  Admin — role-filtered rows, keyboard hints printed on the row (`g h`, `g d`, …), and the
  DAY / NIGHT / AUTO light control at the foot.
- **Shift bar** (`Layout.tsx`): what a technician on shift needs before any figure — global
  fleet search, next maintenance window as a stamped field (policy-derived, `nextWindow`),
  UTC clock, link state (`LinkStamp`), rate-limit stamp when polling rests, signed-in
  operator and role, sign out.
- Keyboard: `g` + letter navigation, `/` to search (see `useKeyboardShortcuts`).

## 5. Sheet anatomy

| Region | Component | Rule |
|---|---|---|
| Headline figures | `FieldBox` + `MicroField` | ≤6 ruled fields, each with a session history strip |
| Triage list | `table.ledger` + `GuideWordsHead` | long lists carry a guide-word head naming first/last row in view |
| Device grid | `DeviceCard` (decal) | identity line, stamp, tag decals, `ServiceRecord` (last / next / interval) |
| Unit record | `DeviceDetail` | decal header, `ServiceRecord`, ruled fields, ruled `Gauge`s, charts, ISO times |
| Sign-off list | `Alerts` | signal · sign-off · actions; "NOBODY ON IT" is a printed state |
| Admin lists | `Users`, `Webhooks`, `SSO`, `PatchPolicies`, `FileTransfer`, `Patches` | same ledger, stamps, `.btn` family |

Density rules: 13–14px body in ledgers, 11px captions, 26px page titles, 12px tabular
figures; 4px rhythm inside a sheet, 16px between sheets; tables scroll inside their panel
with the guide-word head pinned above.

## 6. Buttons and controls

`.btn` (field fill + hairline), `.btn-primary` (ink plate, bone text), `.btn-quiet`
(borderless, for row actions), `.input`, `.select`. Destructive actions are stamp-red text
with a confirm; "Delete" is never a filled block. Focus rings are stamp-red 2px outlines,
`outline-offset: 2px`; selection, caret and scrollbars are themed.

## 7. Motion

One curve, one length: `cubic-bezier(0.34, 1.56, 0.64, 1)` at 400ms (the press of a stamp).
`ow-rise` for a sheet entering, `ow-stamp` for a stamp landing. No looping animation, no
parallax, no scroll-driven storytelling. All motion is killed under
`prefers-reduced-motion`.

## 8. State legibility (the operating contract)

Every screen states: the link (live / down / rate-limited), who is signed in, the UTC clock,
and the next window. Data states are named, not decorative:

- `CONSOLE DATA UNAVAILABLE` — with the server's message and a Refresh control.
- `RATE LIMIT REACHED` — the server allows 100 requests/min/user with a burst of 10; the
  console shows a countdown in the shift bar, pauses every poll (`isRateLimited`), and says
  the sheet will refill itself.
- `NOBODY ON IT` — an unassigned alert.
- `waits for link`, `not scanned`, `never` — a record that has no data says so.

History strips are honest: they cover the console session, and the caption on the dashboard
says exactly that.

## 9. Scale

- Device grid virtualises past 20 units (`react-window`).
- Requests are queued 4-at-a-time for reads (`api/client.ts`), so a sheet with eight panels
  paints fully instead of burning the server's burst and returning 429 panels.
- Polls respect the rate-limit back-off (`isRateLimited` in every `setInterval`), and unit
  pages seed a day of recorded history from the server before live samples arrive.
- Guide words make a thousand-unit list navigable: the head names the first and last unit
  in view, recomputed on scroll.

## 10. i18n

`en`, `de`, `fr` are updated in lockstep (`web/src/i18n/locales/{en,de,fr}.json`); the
redesign's new copy lives under `record.*` and `sheet.*`. Remaining English literals are
concentrated in the admin surfaces (`Settings`, `PatchPolicies` form help, session/transfer
descriptive lines) — carried over from the incumbent console, tracked as debt, not as new
regressions.

## 11. Provenance

- Fonts: Barlow, Archivo Narrow, JetBrains Mono — Google Fonts, OFL 1.1, downloaded as
  WOFF2 subsets into `web/src/assets/fonts/` (self-hosted, listed in `fonts.css`).
- Icons: Lucide (ISC). Charts: Recharts (MIT). No raster images ship in the console; every
  figure is CSS/SVG, so there is nothing unattributed to prove.
- `web/.impeccable/review/*.png` are **review screenshots taken against synthetic fixture
  data** seeded into a throwaway PostgreSQL, not product claims; the seed SQL is not part of
  the repo.

## 12. Open debts

1. Rate-limit-aware polling is per-page; a shared fleet store (one poll for all sheets) is
   the next step and would remove most of the request pressure.
2. Alerts are polled per page, not shared; `GET /devices` carries alert state, so the
   dashboard's "flagged" count is a proxy for the alert ledger's "open" count.
3. Admin surfaces (`Settings` in particular) carry more hardcoded English than the
   monitoring surfaces.
4. History micro-fields show "no history" as an empty ruled slot; a stated `no samples yet`
   caption would read better at 390px.
