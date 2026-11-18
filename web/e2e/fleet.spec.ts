/**
 * 2.2-2 — the shared fleet read model, measured in the browser.
 *
 * The finding this task closes (design-time debt #1) is a
 * request-count finding, so the assertions are request counts: what a mount
 * costs, and what opening one more surface on the same data costs (nothing).
 *
 * The counts are read off the network, not off the store: the claim is about
 * what the server pays for.
 *
 * Method note: a mount is measured on a **cold console** — a fresh browser
 * context carrying the signed-in tokens. Measuring right after signing in
 * counts the landing page's mount as well, and measuring after a reload counts
 * a mount the store answers from its own cache. Neither is the number the
 * finding is about.
 */
import { test, expect, type Page } from '@playwright/test';
import { manifest, reFor, uiLogin } from './fixtures.js';

/** GETs the console issued against the API. Auth POSTs are not reads. */
function countReads(page: Page) {
  const reads: string[] = [];
  page.on('request', (req) => {
    const url = new URL(req.url());
    if (url.pathname.startsWith('/api') && req.method() === 'GET') reads.push(url.pathname);
  });
  return {
    reads,
    reset: () => (reads.length = 0),
    total: () => reads.length,
    byPath: () =>
      reads.reduce<Record<string, number>>((acc, path) => ({ ...acc, [path]: (acc[path] ?? 0) + 1 }), {}),
  };
}

/** A signed-in, cold console: the state a technician's browser is in. */
async function coldConsole(testInfo: {
  context: import('@playwright/test').BrowserContext;
  browser: import('@playwright/test').Browser;
}): Promise<Page> {
  const { context, browser } = testInfo;
  const first = await context.newPage();
  await uiLogin(first);
  const signedIn = await context.storageState();
  await first.close();

  const page = await (await browser.newContext({ storageState: signedIn })).newPage();
  return page;
}

async function expectDashboard(page: Page) {
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible({ timeout: 15_000 });
}

test('a cold dashboard mount stays inside the read budget (≤ 10)', async ({ context, browser }) => {
  const page = await coldConsole({ context, browser });
  const counter = countReads(page);

  await page.goto('/');
  await expectDashboard(page);
  await page.waitForTimeout(1_500);

  const total = counter.total();
  const breakdown = JSON.stringify(counter.byPath());
  // 2.1.0 measured 14 reads on mount, and a 429 on arrival at the default
  // 100/min budget. The shared model's contract: one read per collection,
  // whatever the number of surfaces on the page.
  expect(total, `dashboard mount issued ${total} reads: ${breakdown}`).toBeLessThanOrEqual(10);
  for (const path of ['/api/devices', '/api/patch/overview', '/api/patch/policies', '/api/sessions']) {
    expect(counter.byPath()[path] ?? 0, `${path} must be read once per mount, got ${breakdown}`).toBeLessThanOrEqual(1);
  }
  // The fleet data is on screen: the budget was spent, not skipped.
  //
  // This was `getByRole('heading', { name: /needs attention/i })`, and it could
  // not fail: `Dashboard.tsx` prints that `<h2>` in the panel head *above* the
  // branch, so it is visible with the "nothing is asking for anyone" empty state
  // just as much as with rows. A mount that fetched no fleet at all would have
  // scored 0 reads — comfortably inside the budget — and still passed here.
  //
  // What can only exist after the device read lands is a card for one of
  // *this run's* units: the decal grid is `devices.slice(0, 12)`, and a cold
  // context has no cached fleet to draw it from.
  const unit = manifest().units[0];
  await expect(
    page.getByRole('link', { name: reFor(unit.name) }).first(),
    `the mount must put ${unit.name} on the screen; a budget met by fetching nothing is not met`,
  ).toBeVisible({ timeout: 15_000 });

  await page.context().close();
});

test('opening a unit view after the dashboard adds no list read', async ({ page }) => {
  const unit = manifest().units[0];
  await uiLogin(page);
  await page.goto('/');
  await expectDashboard(page);

  const counter = countReads(page);
  counter.reset();
  // An operator opens a unit by clicking it: a client-side route change, with
  // the store already holding the fleet. A full page load is a mount and is
  // measured by the test above.
  await page.getByRole('link', { name: new RegExp(unit.name) }).first().click();
  await expect(page.getByRole('heading', { level: 1, name: unit.name })).toBeVisible({ timeout: 15_000 });
  await page.waitForTimeout(1_500);

  const breakdown = JSON.stringify(counter.byPath());
  // The unit view's own reads (this unit's record, this unit's metric history)
  // are legitimate and expected. Everything shared is not: the dashboard just
  // read the fleet list, the policy list and the compliance view, and this
  // view renders all three from the store.
  for (const shared of ['/api/devices', '/api/patch/policies', '/api/patch/overview']) {
    expect(counter.byPath()[shared] ?? 0, `${shared} must come from the store, got ${breakdown}`).toBe(0);
  }
  // And the view is real, not a cached blank: this unit's own record landed.
  expect(
    counter.reads.some((p) => p === `/api/devices/${unit.id}`),
    `the unit's own record must be read, got ${breakdown}`,
  ).toBe(true);
});

test('the fleet pollers keep polling, and stay far under the documented 100 req/min budget, with two surfaces open', async ({
  context,
  browser,
}) => {
  // The measurement window is the test: two pages, 30 s of steady state.
  test.setTimeout(90_000);
  // The criterion is "two console tabs open for 10 minutes trip no 429 on a
  // 100 req/min budget". Ten minutes is not a CI test; the rate is, so this
  // measures a steady-state rate and asserts the headroom.
  const page = await coldConsole({ context, browser });
  const dash = countReads(page);
  await page.goto('/');
  await expectDashboard(page);

  const second = await page.context().newPage();
  const ledger = countReads(second);
  await second.goto('/devices?view=table');
  // The ledger renders as a table or as cards, and the view is operator state
  // carried in storage; the unit being named on screen is the assertion. It is
  // the *filtered* ledger that names it: an installation accumulates units
  // (every suite run on a shared database seeds a run-scoped fleet), and a
  // text assertion on the unfiltered page measures the page's window, not the
  // console's ability to show a unit. The filter's read is issued before the
  // counters are reset, so it is not part of the measured rate.
  await second.getByLabel('Search devices').fill(manifest().units[0].hostname);
  await expect(second.getByRole('row', { name: reFor(manifest().units[0].name) })).toBeVisible({
    timeout: 15_000,
  });

  dash.reset();
  ledger.reset();
  // The window is one full device-poll period longer than the cadence it
  // measures, so "at least one poll happened" is a property of the window and
  // not of when the counters happened to be reset: whatever the mount time was,
  // any window of `POLL_MS.devices + 15 s` contains at least one interval tick.
  const DEVICE_POLL_MS = 15_000; // POLL_MS.devices, web/src/stores/fleet.ts
  const windowMs = 30_000;
  await page.waitForTimeout(windowMs);

  const breakdown = JSON.stringify({ ...dash.byPath(), ...ledger.byPath() });
  // Normalise to a rate: 30 s of steady state is half a minute's worth.
  const perMinute = (dash.total() + ledger.total()) * (60_000 / windowMs);

  // An upper bound alone is not a budget: `perMinute < 60` was satisfied by 0,
  // so a poller that stopped polling — the actual 2.2-2 regression, a surface
  // that goes quiet and reads as a healthy fleet — passed it with the best
  // score on the board. Both edges are asserted now: a real poll happened on
  // each surface, and the polls that happened respect the cadence.
  for (const [surface, counter] of [['dashboard', dash], ['devices ledger', ledger]] as const) {
    const polls = counter.byPath()['/api/devices'] ?? 0;
    expect(polls, `${surface} polled nothing in ${windowMs / 1000} s: ${breakdown}`).toBeGreaterThanOrEqual(1);
    // One tick per period, plus one for an invalidate-triggered re-read (a WS
    // device-status event), plus one for a window boundary. A cadence that has
    // run away — a 5 s interval would leave 6 here — still fails.
    const ticks = Math.ceil(windowMs / DEVICE_POLL_MS) + 2;
    expect(polls, `${surface} issued ${polls} device polls in ${windowMs / 1000} s: ${breakdown}`).toBeLessThanOrEqual(ticks);
  }

  // 2.2-3 made the budget a dial; the documented default is 100/min. Two
  // surfaces of shared polls must sit far under it, with room left for the
  // operator's own clicks.
  expect(perMinute, `two surfaces issued ~${perMinute} reads/min: ${breakdown}`).toBeLessThan(60);

  await page.context().close();
});
