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
