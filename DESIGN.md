# OurWay Console — Design Contract

Direction: **canon** — the category standard, chosen by the owner as the standing exit from a
rolled hand of seven derived worlds (Impeccable Operate, code-led, seed `e4d24b3c9d1222e5`).
Rewritten 2026-11-16 against the built console. This replaces the previous contract, which
described a letterpress "service decal sheet" world (Archivo Narrow, cream plates, stamped ink,
2px radii) that no longer exists in the codebase.
Scope: the whole web console — shell, monitoring, management, administration (15 routes).

Craft bar, in the owner's words: **NinjaOne / Atera / JumpCloud** for convention, **Grafana +
Stripe Dashboard** for information design and table/form discipline, **Linear + Vercel** for
interaction polish. The ambition of this world is not novelty; it is that nothing in it is rough.

---

## 1. Thesis

The console is the RMM console an operator already knows. Its idea is **finish as the
differentiator** — conventions embraced completely, executed above the level the peers reach.
What needs the operator is on the first screen, grouped and ranked; scope and sign-in are always
stated; every row can be acted on without leaving it.

The category default it refuses: four rounded stat cards, a device grid of coloured dots, a blue
accent doing decorative work, and pages of generic cards.

## 2. The world

Day is the reference; night is the same system at night luminance, not a second design. Values
are `web/src/index.css` — `:root` and `.dark`. If this table and the CSS disagree, the CSS is
right and this file is a bug.

| Role | Day | Night | Token |
|---|---|---|---|
| Canvas | `#f6f8fa` | `#0e1319` | `--bg-primary` |
| Sheet / panel | `#ffffff` | `#161d26` | `--bg-card` |
| Hairline rule | `#e3e8ee` | `#262f3b` | `--rule`, `--border` |
| Strong rule (inputs, switch track) | `#cbd5e1` | `#384454` | `--rule-strong` |
| Text — primary | `#0f172a` | `#e6edf3` | `--text-primary` |
| Text — secondary | `#475569` | `#9fabba` | `--text-secondary` |
| Text — caption/muted | `#5b6678` | `#8793a3` | `--text-muted` |
| Nav rail (the one committed field) | `#101725` | `#0a0f16` | `--rail-bg` |
| Rail text / muted | `#d7dfe9` / `#7d8da0` | same | `--rail-text*` |
| Rail active item | `#e6edf3` on `#0f172a` text | same | `--rail-active-*` |
| Action accent (chrome, rings, links) | `#2563eb` | `#4c8dff` | `--accent`, `--focus` |
| Service blue (primary actions) | `#1d4ed8` | `#5593ff` | `--service` |
| Error / stamp | `#b91c1c` | `#f36367` | `--error`, `--stamp` |
| Due / warning | `#92400e` | `#d99a2b` | `--due`, `--warning` |
| Online | `#166534` | `#3fb96f` | `--online` |
| Offline | `#94a3b8` | `#8b98a8` | `--offline` |
| Field fill | `#f8fafc` | `#111820` | `--field` |
| Row hover / selected | `rgba(15,23,42,.035)` / `rgba(37,99,235,.06)` | `rgba(230,237,243,.045)` / blue wash | `--row-*` |

Non-negotiable rules of the world:

- **State colour is semantic only.** Red, amber, green and their pills mean a device or job
  state. They never decorate, never tint a card border, never mean "important".
- **One field of colour.** The nav rail is the only committed dark mass in day mode. Nothing
  else in the content column gets a coloured background except a selected row, a stamp wash,
  and a primary button.
- **Depth is one soft offset elevation** (`--elev-1/2/3`). No zero-blur block shadows, no glass,
  no blur decoration, no gradients-as-structure.
- **No coloured border-left above 1px** on any card or row. The 2px tab underline on the active
  rail item is the single accepted place a coloured edge appears.
- **Contrast floor.** Text clears 4.5:1 (3:1 at ≥24px). A stamp paints 12% of its own colour
  behind that same colour, so **every status ink is scored against its own wash**, not against
  the bare card. Light inks that passed on white failed there; the day values above are the
  result. Verified by `.impeccable/tmp/audit.mjs` across every route × theme.

## 3. Typography

- **Public Sans** everywhere, self-hosted (`web/src/assets/fonts/`), no CDN.
- **Overpass Mono**, self-hosted, only for data: identifiers, IPs, URLs, token prefixes, JSON,
  durations, timestamps. Never for prose, never for a heading.
- Numerals are tabular (`--ow-num`, `font-variant-numeric: tabular-nums`) on every scanned
  number: counts, percentages, bytes, windows, durations, timestamps.
- Page title: `ow-display text-[22px]` — one weight, tight tracking. Section heads are `panel-head`
  small caps at 11px with modest tracking. Body copy 13px; captions 11–12px.
- No kicker/eyebrow line above a heading. No all-caps sentences.

## 4. Geometry, density, motion

- Radii: 4 / 6 / 8px (`--radius-sm/-/--radius-lg`). Pills are 999px. Nothing rounder.
- Density is **fixed**, not user-selectable — no comfortable/compact switch ships. Row heights
  32–40px in ledgers, 32px minimum on every control.
- Rail 240px, top bar 64px, content padding 16px; panels stack on a 16px rhythm.
- One curve, one length: `--ease-press: cubic-bezier(.2,0,0,1)`, `--motion: .15s`, applied to an
  already-visible element. The one authored motion moment is the live link's pulse in the top
  bar; everything else transitions at or below this budget. `prefers-reduced-motion` respected.

## 5. Component inventory

| Component | Contract |
|---|---|
| `.panel` / `.panel-head` | White sheet, hairline rule, 6px radius, `--elev-1`. Head is a bordered strip with the title left, controls right. |
| `table.ledger` | The workhorse. 11px small-caps heads, 13px rows, hairline row rules, `--row-hover`, right-aligned numerals. |
| `.ledger-scroll` | The box a ledger scrolls inside when its columns outnumber the viewport. `position: relative` is load-bearing, not decoration: without it Chromium propagates a scroll container's scrollable overflow to the viewport, and the whole page pans sideways so the same column is reachable two ways at once — measured on `/sso` and `/thresholds`, where the document panned 541px at 390 and 155px at 1024. Apply it to the element that holds the table (a panel that only existed to clip may wear it instead of `overflow-hidden`). |
| `.stamp` | State, never a dot. 12% wash of its own ink, 999px radius, 11px caps. Tone via `text-[var(--token)]`. |
| `.btn`, `.btn-primary`, `.btn-quiet` | One shared base so a lone `btn-primary` is never unstyled. Primary = `--service` fill; quiet = transparent until hover. **`.btn-quiet` is declared before `.btn-primary` on purpose**: a control carrying both (a filter chip that is quiet until active) must read as primary, and dark-theme primary ink on a transparent field would be invisible. |
| `.segmented` / `.segmented-item` | One recessed `--field-alt` track, `2px` padding, the active segment raised as a `--bg-card` chip with `--elev-1`. State comes from `aria-pressed` alone — no conditional classes at the call site. On the rail, add `.segmented--rail`, which swaps to `--rail-bg-raised` / `--rail-active-bg`. Used by the device view switch and the theme switch. |
| `.switch` | `role="switch"` + `aria-checked`, 2.25rem × 1.25rem, `--rule-strong` track, 14px white knob with a real shadow, track turns `--service` from its own accessible state — never from a Tailwind arbitrary-value opacity, which silently emits nothing. **A switch is an instant-apply setting; a checkbox is a form value committed by Save, or a row selection.** Those are different decisions and the console keeps them visually distinct. |
| Nav rail / drawer | Static from `lg` up. Below `lg` the same element is a dialog: `role="dialog"`, `aria-modal`, `aria-label`, Escape closes it, focus moves in and returns to the trigger, navigating closes it, and a closed drawer is `visibility: hidden` so it leaves the tab order and the accessibility tree without snapping the slide. |
| Tabs | `role="tablist"` with roving `tabIndex`, Arrow/Home/End travel, `aria-selected`, and a `role="tabpanel"` that names its tab back via `aria-labelledby`. The 2px underline is the only coloured edge a tab may wear. |
| `MicroField` | Line + area sparkline (100-unit viewBox, non-scaling stroke, 9% area, dashed onset line, dot on the newest sample). It shows recent history behind a summary figure; it is not decoration. |
| Filter chips | Bordered `btn`, not `btn-quiet` — a count you can click is a control and must look like one. Active = primary. |
| Rail | Grouped nav with counts, operator identity at the foot, 2px active tab underline. |
| Top bar | Search, scope, next maintenance window, UTC clock, live link, operator. **It sheds in a stated order rather than overflowing** — see §8. Cluster gaps collapse at `md`/`xl`; instrument labels carry `shrink-0 whitespace-nowrap` so nothing wraps mid-word. |

## 6. Copy

**Copy is fixed content for this redesign.** User-visible strings, i18n keys (en/de/fr in
lockstep), `data-*` attributes and DOM order are load-bearing: ~200 assertions depend on them,
and `table.ledger` / `div.field` are live test selectors. Additive accessibility attributes
(`aria-label`, `role`, `aria-checked`, `title`) are fixes, not copy changes. Data-derived labels
(API enums such as `alert` → `Alert`) are formatted at render, never re-authored.

## 7. Accessibility floor

4.5:1 text (measured, not assumed — see §2). Every control has an accessible name. Switches
report state. Focus is a visible ring on `--focus` everywhere, including the rail. Theme choice
applies to every surface the app renders. Virtualised grids stay keyboard- and screen-reader
navigable. Anything that behaves like a dialog must say so and behave so: `role="dialog"`,
`aria-modal`, a label, Escape to dismiss, focus in on open and back to the trigger on close —
the confirm sheet and the mobile nav drawer are the two reference cases, both verified in a
browser rather than by reading the markup.

## 8. Truncation is a decision, not an accident

There are two honest outcomes and no third. A value that cannot fit is either **announced** — cut
with an ellipsis and the full text in the element's `title` — or made **reachable** by a scroller.
A value cut off with no ellipsis and no title is a defect, and so is a page that pans sideways to
reach a column a box could have scrolled.

- Device cards, the `In view … → …` guide bar and dense rows clip with a `title` carrying the full
  value (`Linux · amd64 · nwl-srv-01`, `weekly · 01:30–04:30 America/New_York`). This matches the
  density of the named peers.
- A ledger wider than its column scrolls inside `.ledger-scroll` (§5). Columns stay reachable; the
  document does not grow.
- The audit therefore holds **zero** rather than carrying a list of accepted exceptions: it counts a
  titled ellipsis as announced — the ellipsis must actually paint, and the `title` must be on the cut
  element or the one wrapping it, not on a distant ancestor that happens to be titled — and it
  ignores content inside a horizontal scroller, because that
  content is reachable by design.
- **The shift bar sheds in a stated order.** The way out never yields, and neither does the thing
  that says *whose* fleet this is — the avatar is on the bar at every width, with the operator's name
  beside it from `sm` and spoken to assistive tech below that, where the search field needs the room.
  A name shed by breakpoint is a decision; a name cut off mid-glyph is the defect this section is
  about. Below identity the order is: UTC clock from `sm`; the link indicator from `md`, because it is
  the truth about whether the figures on screen are live; the server version, the per-user rate figure
  and the tenant scope from `xl`; and the next maintenance window only at `2xl` — it is the widest
  instrument and below that it cannot say the whole window. That last one is a real trade, not a free
  win: the version and the rate figure are what the console's own end-to-end suite promises an
  operator at 1280×720, and the first cut of this order hid them there and failed that suite. The
  window is not lost information — the dashboard and the patch-policy pages both name it.
- **The yield order is also the squeeze order.** The instruments sit in one shrinkable box; identity
  and the way out sit outside it and are `shrink-0`. Inside the box, every instrument is a flex item
  with `min-width: auto` and so refuses to shrink below its own text — only the truncating instruments
  carry `min-w-0`. Any deficit is therefore paid by those, as an ellipsis with the full value beside
  it, and never by the instrument at the right-hand end, which a clipped bar cannot show at all. That
  is the whole mechanism, and it makes the ladder load-bearing in two directions: from `md` to `lg` the
  box holds exactly the clock and the link, with nothing left over and nothing that can absorb a
  squeeze, which is precisely why nothing else is on the bar there.
- **The rate-limit warning is the one instrument with no breakpoint.** The *figure* — the per-user
  budget — waits for `xl` like the other rates, because it is a number and the bar can do without a
  number. The *warning* replaces that figure when the server starts refusing us, and it is never shed
  and carries no words that could be cut: an icon, a `title`, and a sentence for assistive tech. The
  reason is §9: the condition it states is the one that makes every other figure on the bar stale, the
  poller skips its tick silently, and the link indicator beside it will still read **Live**, because
  the socket is a different transport. A warning that yielded with the trivia would let this bar
  assert liveness while nothing was being read.
- The budget figure is the bar's only instrument without a caption, against §5's rule, on purpose and
  by arithmetic: the caption costs more than the scope chip needs to show a recognisable tenant name,
  and `100/min` is the only bare value on the bar so there is nothing for it to be confused with. If a
  fifth rate ever joins it, caption them all or caption none.
- **A truncation is announced only if the ellipsis paints.** `text-overflow` does not inherit and does
  not reach an anonymous flex item, so `truncate` on the inline-flex `.stamp` pill is dead: the value
  is painted straight through the pill's edge, clipped by the bar, with nothing to say so. The idiom is
  a block child that owns the text — `<Stamp className="max-w-[…] min-w-0"><span
  className="block min-w-0 truncate">…</span></Stamp>` — and the full value hangs off the pill, with
  the sentence spelled out in an `.sr-only` span too, because a `title` on a span nobody can focus is a
  hover tooltip, not an announcement. This was not hypothetical: the first fix for the cut link
  indicator put `truncate` on the pill, and the tenant chip became a 20px pill holding one visible
  character with 108px of the name painted outside it.
- **`e2e/responsive.spec.ts` enforces this, as a tenant.** The bar is measured with a technician
  granted one client whose name is long on purpose, because that is the only state in which the scope
  chip truncates and therefore the only state in which the original defect — a tenant name pushing the
  link indicator past the edge of the box — can occur. An admin's chip reads `all tenants`, which fits
  under its cap at every width, so an admin-only sweep cannot see the truncation branch run even once;
  that is how the first version of this spec passed CI while guarding nothing. Concretely it asserts,
  at 1600 / 1536 / 1440 / 1280 / 1024 / 768 / 390: that no box in the header hides ink that cannot be
  reached (on five tenant routes); that the link indicator's own right edge is inside the clipping box
  (the invariant, measured rather than reasoned about); that nothing pans the document *or* the
  scrolling column; that each instrument is present at its gate and *attached-but-hidden* below it, so
  an instrument that stopped rendering cannot masquerade as one that shed; that the maintenance window
  shows a real seeded window and not its `Unscheduled` placeholder; and that the operator's name is
  drawn from `sm` and present in the accessibility tree below it. It measures nothing until each
  width's instrument has drawn its **value**, not its caption — the caption `fleet` and the placeholder
  version `dev` both paint on the first frame, and an unpopulated bar never overflows.
  What is not guaranteed: the live rate-limited state. CI raises the per-minute budget to 2000, which
  usually keeps the suite under it — and the suite has tripped it anyway, so the rate instrument is
  tested as a pair of states rather than by assuming one, and the component test drives the flag.
  The evaluator is written so that reverting the truncation markup turns it red — a green sweep that
  cannot go red is worth nothing, and this suite has shipped one of those.

## 9. Failure is a state; zero is a claim

The console answers questions about a fleet it cannot always reach. A figure is a claim about
the world, so when the read failed the figure must not be printed. `0 open alerts` and `could
not read the alert ledger` are opposite sentences; this build previously showed the first.

- **Never render a count that could not be obtained.** Titles, captions and chips drop the
  number when their read failed (`devices.countUnavailable`, `alerts.countUnavailable`,
  `common.countUnavailable`). The control stays clickable; the claim goes away.
- **Two error weights, not one.** A surface that could not load renders `FetchError`: a panel in
  `--error` ink naming the failure, the transport's own sentence kept as secondary text (it is
  the line an operator reads aloud to whoever owns the server), and a Retry that provably
  re-issues the request. A single field failing inside a working view keeps the small `cap`
  stamp it has always used.
- **The store does not lie on a collector's behalf.** `collectors` in `stores/fleet.ts` used to
  end alerts, sessions and recordings with `.catch(() => [])`, which turned a 500 into an empty
  but successful-looking read. Genuinely optional collections may still fall back; the three an
  operator reads as a safety signal do not.
- **A gate explains itself.** `RoleRoute` bounced the operator to the dashboard in silence. It
  now names the missing roles in their server spelling and leaves the address in the bar,
  because that address is the thing they hand to an administrator. `/settings` is not
  role-gated at all: profile, password and preferences belong to every authenticated operator
  (`GET/PUT /api/auth/profile` carries no role), and the admin sections inside guard themselves.
- **An unknown address is a page.** `<Route path="*">` renders `NotFound`. A shell around an
  empty `<main>` made a stale bookmark indistinguishable from a crashed report.
- **The shift bar yields in order** (§8). When the bar runs out of room it is an instrument that
  gives — first the trivia, then, below `xl`, the window label itself — never the way out of the
  console.

## 10. Debt, stated plainly

- **Rate-limit-aware polling is still per-page.** Not a visual concern, unchanged by this pass.
- No comfortable/compact density switch ships (§4). The rail's counts are static per fetch.
- **`text-[var(--token)]` appears inline in 27 files (~130 sites) where a Tailwind colour
  utility already exists.** Visually identical and token-backed, so this pass deliberately did
  not churn ~130 call sites for an invisible gain; a follow-up should migrate them to
  `text-service`, `text-error`, and friends so the utilities are the only spelling.
- `panel-head` adoption is not yet uniform across the administrative pages, and filter-row
  rhythm varies by a step or two between pages. Segmented controls and the tab pattern now ship
  as primitives (§5); the residual work is applying them everywhere the role recurs.
- Hardcoded English `aria-label` strings (the shift bar's menu button, the session viewer) sit
  outside `t(...)`, so screen-reader copy is not localised even though visible copy is. Recorded,
  not fixed here, because adding keys means writing the `de` and `fr` text with someone who
  reads them.
- **The audit holds zero findings, not an accepted list.** Every clipped, contrast,
  unnamed-control, raw-locale-key, mono-prose, element-overflow and document-overflow count is zero
  across 35 route × theme checks at 1440 and 90 route × width checks at 1440 / 1280 / 1024 / 768 /
  390 (§11). What remains unproven by it: mutation dialogs opened while the stress fixtures are
  loaded have not been driven, only the ledgers behind them.
- **Only four surfaces speak `FetchError`.** Devices, Alerts, Sessions and Recordings state a
  failed load properly. Audit, Clients, Files, Thresholds, Users, Webhooks and the patch views
  still print transport text — some in a header stamp, some as a floating line — and none offer
  the retry. Adopting `FetchError` there is mechanical: pass the message as `detail` and the
  page's reload as `onRetry`, and make sure the empty state does not also render.
- **A few counts are still printed from an empty collection** on pages whose read failed but
  which never asked the store for its `error` (the sessions ledger's dashboard neighbour). The
  store exposes it; the page has to use it.

## 11. How to verify a change to this world

```
node .impeccable/harness/gen-fixtures.mjs     # stubbed /api/* + /ws fixtures
node .impeccable/harness/server.mjs           # serves web/dist on http://127.0.0.1:4173
node .impeccable/tmp/audit.mjs                # all routes × light/dark → .impeccable/audit-report.md
node .impeccable/tmp/shoot.mjs                # 43 captures into .impeccable/shots/
```

Widths are swept the same way, by environment — the default is 1440 × light+dark:

```
$env:OURWAY_AUDIT_WIDTHS="1440,1280,1024,768,390"; $env:OURWAY_AUDIT_THEMES="light"
$env:OURWAY_AUDIT_SHOTS="1"; $env:OURWAY_AUDIT_OUT="../audit-widths.md"
node .impeccable/tmp/audit.mjs
```

`SHOTS=1` writes `shots/width-<w>-<theme>-<route>.png`. Read the report, not only the totals: the
element path and the pixel overhang are what say whether a finding is yours.

The instrument box gets its own measurement, because a page-level audit cannot see an instrument
box that is quietly eating a chip:

```
node .impeccable/tmp/bar.mjs   # per width: search width, each instrument's px, px cut, and the
                               # version / budget / N-per-min assertions e2e/auth.spec.ts makes
```

The end-to-end suite runs at **1280×720** — Playwright's default, now *pinned* in
`e2e/playwright.config.ts` rather than inherited, because that width is part of the contract:
`e2e/auth.spec.ts` asserts the server version and the rate figure are visible there. `locale: 'en'`
is pinned for the same kind of reason: the console detects a language and caches it, so a suite that
asserts on English copy is one `storageState` optimisation away from asserting on German. The rest of
§8 is asserted by `e2e/responsive.spec.ts`, which sweeps 1600 / 1536 / 1440 / 1280 / 1024 / 768 / 390
and checks the shed order **in both directions** — an instrument present where it is promised, and
attached-but-hidden where it yields, so that an instrument which stopped rendering at every width
cannot report itself as having shed correctly. It measures the bar as a granted tenant with a long
client name, because that is the only state where the truncation branch runs. The same spec asserts
that no route pans the document or the scrolling column, and that a ledger's overflow stays inside its
own box — the latter on two ledger pages at 390px, which is a sample, not a census: the remaining
`.ledger-scroll` sites are covered by the §5 audit in `bar.mjs`, not by CI. It waits for the bar's
instruments before measuring, because the version, the scope and the window arrive from three separate
reads and a probe run as soon as the page paints measures an empty bar, and an empty bar never
overflows. Change the bar's breakpoints and that spec in the same commit: it is the machine-readable
half of §8, and a page-level audit cannot see it.

The failure conditions in §9 are manufactured, not imagined:

```
node .impeccable/harness/mutate.mjs --empty   # every list the console reads, emptied
node .impeccable/harness/mutate.mjs --stress  # 1,200 units, 200 alerts, and names that are
                                              # long, accented, emoji, CJK and RTL
$env:OURWAY_HARNESS_FIXTURES=...; $env:OURWAY_HARNESS_PORT=4181; node .impeccable/harness/server.mjs
node .impeccable/tmp/harden.mjs               # 500/403/401/slow/no-data/hostile-data/roles
```

Plus, from `web/`: `tsc --noEmit -p tsconfig.json`, `vitest run`, `vite build`. The repo's CI
gate (`npm ci`, `npm run build`, `scripts/coverage.sh`, `python3 scripts/i18n-parity.py`) was
not fully runnable in this environment — no npm and no python3 — so parity holds only because
no copy was changed; run the real gate before merging.
