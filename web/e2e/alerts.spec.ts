/**
 * 2.2-0 — the alert ledger, and the one interaction it must survive.
 *
 * The alerts here were produced by the server's own engine from seeded
 * thresholds (see fixtures.ts), so this asserts the engine → ledger → console
 * chain, which no unit test covers. 2.2-4 later changes *which* alert a
 * threshold produces, and this is the suite that notices.
 */
import { test, expect } from '@playwright/test';
import { ADMIN_USER, ADMIN_PASSWORD, apiBase, apiLogin, reFor, uiLogin } from './fixtures.js';

test.beforeEach(async ({ page }) => {
  await uiLogin(page);
  await page.goto('/alerts');
});

test('the alert ledger shows the alerts the engine produced', async ({ page, request }) => {
  // Ask the engine's own output (the API), then require the ledger to show it.
  // Scoped to rows: the status filter's <option value="critical"> also reads
  // "Critical", and the unit list in the sidebar names the same units.
  const token = await apiLogin(ADMIN_USER, ADMIN_PASSWORD);
  const list = await request.get(`${apiBase()}/api/alerts?limit=100`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(list.status(), `reading the alert ledger failed (${list.status()})`).toBe(200);
  const { alerts } = await list.json();
  expect(alerts.length, 'the seeded fleet should leave alerts').toBeGreaterThan(0);

  // The Alerts ledger is a <ul> of <li> rows (web/src/pages/Alerts.tsx), so
  // listitem is the row role here. Severity stamps read lowercase in the
  // status filter and capitalized in the ledger, hence the case-insensitive text.
  await expect(page.getByRole('listitem').filter({ hasText: /critical/i }).first()).toBeVisible();

  const namedUnits: string[] = [...new Set<string>(alerts.map((a: { device_name: string }) => a.device_name))];
  for (const name of namedUnits) {
    await expect(page.getByRole('listitem').filter({ hasText: reFor(name) }).first()).toBeVisible();
  }
});

test('acknowledging an alert is durable across a reload', async ({ page, request }) => {
  // Take an open alert from the API, acknowledge it through the UI, and prove
  // it is still acknowledged after a reload — acknowledge is the one write the
  // ledger must not lose.
  const token = await apiLogin(ADMIN_USER, ADMIN_PASSWORD);
  const list = await request.get(`${apiBase()}/api/alerts?limit=100`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(list.status(), `reading the alert ledger failed (${list.status()})`).toBe(200);
  const { alerts } = await list.json();
  const target = alerts.find((a: { resolved: boolean; acknowledged: boolean }) => !a.resolved && !a.acknowledged);
  expect(target, 'the seeded fleet should leave an open alert').toBeTruthy();

  const row = page.getByRole('listitem').filter({ hasText: target.message.slice(0, 24) }).first();
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /acknowledge|ack$/i }).click();

  await expect(row).toContainText(/acknowledged/i, { timeout: 10_000 });

  // Durable, not optimistic: a fresh read of the ledger re-renders it as
  // acknowledged.
  await page.reload();
  await page.goto('/alerts');
  await expect(
    page.getByRole('listitem').filter({ hasText: target.message.slice(0, 24) }).first(),
  ).toContainText(/acknowledged/i);
});

/**
 * Read every named surface once, and keep reading until they agree.
 *
 * These specs ask one population to answer through several surfaces, so the
 * invariant is agreement, not a number: the population itself moves while the
 * suite runs, and a fixed expected count encodes a moment that may already have
 * passed. Reports the values it actually saw, because "they disagreed" without
 * them is no use to whoever reads this on a runner at 3am.
 */
async function untilAgreed(reads: Array<() => Promise<number>>, label: string, timeout = 20_000): Promise<number> {
  const deadline = Date.now() + timeout;
  let seen: number[] = [];
  for (;;) {
    seen = await Promise.all(reads.map((read) => read()));
    if (seen.every((n) => n > 0 && n === seen[0])) return seen[0];
    if (Date.now() > deadline) {
      throw new Error(`${label} still disagree after ${timeout / 1000}s (${seen.join(' vs ')})`);
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

test("the dashboard alert figure is the ledger figure, and both move on the same events", async ({
  page,
  request,
}) => {
  // Two of the agreement reads remount the dashboard to get an honest tile, so
  // each costs a reload plus a fetch. The suite's 30s default is smaller than
  // that arithmetic, and a test killed by the harness fails as "Target page,
  // context or browser has been closed" - a misleading verdict for "the surfaces
  // were still converging". Budget the wait, so any failure that does come names
  // the surfaces and their numbers instead of blaming the browser.
  test.setTimeout(120_000);

  // 2.2-4. The old failure was three populations answering "how many alerts are
  // open": the tile counted units whose status is `alert`, the ledger counted
  // the server-filtered page it had just fetched, and a third count ignored
  // acknowledged alerts. Now one collection, one definition: open is
  // unresolved, acknowledged included.
  const openFromServer = async () => {
    const token = await apiLogin(ADMIN_USER, ADMIN_PASSWORD);
    const res = await request.get(`${apiBase()}/api/alerts?limit=200`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res.status()).toBe(200);
    const { alerts } = await res.json();
    return (alerts as { resolved: boolean }[]).filter((a) => !a.resolved).length;
  };

  // The open-alert population is not static, and these surfaces are not one
  // read. A seeded breach auto-resolves while the suite runs - measured on a
  // local run: 3 open at +43s, 2 open from +49s - and the tile is a cached read,
  // not a live one (see tileCount). A number captured once at the top of this
  // test is therefore a statement about a moment that may already have gone, and
  // every assertion that pins a live surface to it is a race the suite only wins
  // on a fast runner: which is precisely how this spec went red on CI (`Expected
  // "Active Alerts2"`, `Received "Active Alerts3"`, the tile steady at 3 across
  // 34 retries) and green at a desk. What 2.2-4 promised is one collection and
  // one definition, so assert that instead: all three surfaces, same number, and
  // each of them read now.
  const dash = await page.context().newPage();
  // Case and whitespace are not stable across how a number reaches us: innerText
  // gives the rendered "ACTIVE ALERTS\n12\nunresolved", textContent gives
  // "Active Alerts12unresolved". Strip both, or the parser reads -1 and every
  // assertion below silently becomes a timeout - which is a worse failure than
  // the one being fixed, because it looks like the product regressed.
  const tileCount = async () => {
    // The tile is a cached fleet read (`useFleet`): refreshed when the dashboard
    // mounts, or when a mutation on that page invalidates the entry - never by
    // polling. So the number it shows is the population as of its last mount,
    // which is the whole race in one line: read the API, then look at a surface
    // that stopped listening some time ago. Reading the tile honestly means
    // remounting it, and the loop below tolerates the fetch that follows.
    const field = dash.locator('div.field').filter({ hasText: /Active Alerts/ }).first();
    await dash.reload();
    try {
      await field.waitFor({ state: 'visible', timeout: 5_000 });
    } catch {
      return -1;
    }
    const text = (await field.textContent()) ?? '';
    return Number(text.replace(/\s+/g, '').match(/activealerts(\d+)/i)?.[1] ?? -1);
  };
  const ledgerCount = async () => {
    const text = (await page.locator('p').filter({ hasText: /open ·/ }).first().textContent()) ?? '';
    return Number(text.replace(/\s+/g, '').match(/^(\d+)open/i)?.[1] ?? -1);
  };

  await page.goto('/alerts');
  await expect(page.locator('p').filter({ hasText: /open ·/ }).first()).toBeVisible();
  await dash.goto('/');
  await expect(dash.locator('div.field').filter({ hasText: /Active Alerts/ }).first()).toBeVisible();

  const serverOpen = await untilAgreed(
    [openFromServer, tileCount, ledgerCount],
    'the API, the dashboard tile and the ledger header'
  );
  expect(serverOpen, 'the seeded fleet should leave open alerts').toBeGreaterThan(0);
  await expect(page.getByRole('button', { name: new RegExp(`^${serverOpen} Open$`) })).toBeVisible();

  // Acknowledging is a sign-off, not a close: the open count must not move.
  const token = await apiLogin(ADMIN_USER, ADMIN_PASSWORD);
  const list = await request.get(`${apiBase()}/api/alerts?limit=200`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const { alerts } = await list.json();
  const target = (alerts as { id: string; message: string; resolved: boolean; acknowledged: boolean }[]).find(
    (a) => !a.resolved && !a.acknowledged
  );
  expect(target, 'the seeded fleet should leave an unacknowledged alert').toBeTruthy();
  const open1 = target!;

  // The baseline has to come from now, not from the top of the file: the claim
  // is "my write did not change the open count", and that needs a number the
  // population still holds.
  const beforeAck = await untilAgreed([openFromServer, ledgerCount], 'the API and the ledger, before the ack');

  const row = page.getByRole('listitem').filter({ hasText: open1.message.slice(0, 24) }).first();
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /acknowledge|ack$/i }).click();
  await expect(row).toContainText(/acknowledged/i, { timeout: 10_000 });

  const afterAck = await untilAgreed([openFromServer, ledgerCount], 'the API and the ledger, after the ack');
  expect(afterAck, 'acknowledging does not close an alert').toBe(beforeAck);
  await expect(
    page.getByRole('button', { name: new RegExp(`^${afterAck} Open$`) }),
    'the chip keeps counting the acknowledged alert as open',
  ).toBeVisible();

  // Resolving closes it, and every surface moves by exactly one - the tile
  // included, which the old version only compared once, at the end.
  await row.getByRole('button', { name: /^resolve$/i }).click();
  const closed = await untilAgreed([openFromServer, tileCount, ledgerCount], 'the figure after a resolve');
  expect(closed, 'a resolve closes exactly one alert').toBe(beforeAck - 1);

  await dash.close();
});
