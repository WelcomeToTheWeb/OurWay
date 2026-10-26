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

test("the dashboard alert figure is the ledger figure, and both move on the same events", async ({
  page,
  request,
}) => {
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

  const serverOpen = await openFromServer();
  expect(serverOpen, 'the seeded fleet should leave open alerts').toBeGreaterThan(0);

  // The dashboard tile.
  await page.goto('/');
  const tile = page.locator('div.field').filter({ hasText: /Active Alerts/ }).first();
  await expect(tile).toBeVisible();
  await expect(tile, "the tile counts the ledger population").toContainText(
    new RegExp('Active Alerts' + serverOpen)
  );

  // The ledger, same numbers, stated.
  await page.goto('/alerts');
  const header = page.locator('p').filter({ hasText: /open ·/ }).first();
  await expect(header).toContainText(new RegExp(`^${serverOpen} open ·`));
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

  const row = page.getByRole('listitem').filter({ hasText: open1.message.slice(0, 24) }).first();
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /acknowledge|ack$/i }).click();
  await expect(row).toContainText(/acknowledged/i, { timeout: 10_000 });

  await expect(header, 'acknowledging does not close an alert').toContainText(new RegExp(`^${serverOpen} open ·`));
  await expect(
    page.getByRole('button', { name: new RegExp(`^${serverOpen} Open$`) }),
    'the chip keeps counting the acknowledged alert as open',
  ).toBeVisible();

  // Resolving closes it: the tile and the chips move together, by one.
  await row.getByRole('button', { name: /^resolve$/i }).click();
  const closed = serverOpen - 1;
  await page.goto('/');
  await expect(
    page.locator('div.field').filter({ hasText: /Active Alerts/ }).first(),
    'the dashboard figure follows the ledger'
  ).toContainText(new RegExp('Active Alerts' + closed), { timeout: 15_000 });

  await page.goto('/alerts');
  await expect(
    page.locator('p').filter({ hasText: /open ·/ }).first(),
    'the ledger figure is the same number'
  ).toContainText(new RegExp(`^${closed} open ·`), { timeout: 15_000 });
});
