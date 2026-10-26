/**
 * 2.2-0 — the fleet ledger, its filter, and a unit's own view.
 *
 * The ledger is the surface a technician lives in, and it is where 2.3 (client
 * scoping) will land, so its baseline behaviour is pinned here: every unit
 * appears, the filter narrows to the matching unit, and a unit view is legible
 * before any live sample arrives.
 *
 * The ledger has two views; these assertions use the table (`?view=table`),
 * where each unit is a row.
 */
import { test, expect } from '@playwright/test';
import { manifest, reFor, uiLogin } from './fixtures.js';

test.beforeEach(async ({ page }) => {
  await uiLogin(page);
});

test('the device ledger lists every seeded unit', async ({ page }) => {
  await page.goto('/devices?view=table');
  for (const unit of manifest().units) {
    await expect(page.getByRole('row', { name: reFor(unit.name) })).toBeVisible();
  }
});

test('filtering the ledger narrows it to the matching unit', async ({ page }) => {
  await page.goto('/devices?view=table');
  const [first, ...others] = manifest().units;

  await page.getByLabel('Search devices').fill(first.hostname);

  await expect(page.getByRole('row', { name: reFor(first.name) })).toBeVisible();
  for (const other of others) {
    await expect(page.getByRole('row', { name: reFor(other.name) })).toHaveCount(0);
  }
});

test('the unit view is legible before the next live sample arrives', async ({ page }) => {
  const unit = manifest().units[0];
  await page.goto('/devices?view=table');
  await page.getByRole('row', { name: reFor(unit.name) }).getByRole('link').first().click();

  await expect(page).toHaveURL(new RegExp(`/devices/${unit.id}`));
  // The detail view is a lazy chunk plus a device fetch. Do not wait on
  // networkidle here or anywhere in this suite: the console keeps a WebSocket
  // open for the whole session, so that event never settles.
  await expect(page.getByRole('heading', { level: 1, name: unit.name })).toBeVisible({ timeout: 15_000 });
  // The unit answers with its own identity, not a spinner: the name, the system
  // panel and the fields an operator reads first are present on first paint.
  // (The vitest suite owns the "sample is stale" branch, because it needs a
  // unit that is not reporting.)
  await expect(page.getByRole('heading', { level: 2, name: /system/i })).toBeVisible();
  for (const label of ['Agent version', 'Last seen', 'Public IP']) {
    await expect(page.getByText(label, { exact: true }).first()).toBeVisible();
  }
});

test('the unit view states the remote-control state the unit reported (2.2-1)', async ({ page }) => {
  // The Mac in the seed fleet reports `denied` + `capture_state: error` on the
  // metrics wire — the macOS case this task exists to close. The console must
  // name it on the unit view, before a technician is in front of the machine.
  const mac = manifest().units[2];
  await page.goto(`/devices/${mac.id}`);
  await expect(page.getByRole('heading', { level: 1, name: mac.name })).toBeVisible({ timeout: 15_000 });

  await expect(page.getByText('Remote input')).toBeVisible();
  await expect(page.getByText('Needs Accessibility')).toBeVisible({ timeout: 5_000 });
  await expect(page.getByText('Capture failing')).toBeVisible({ timeout: 5_000 });
  // The reason is stated, not implied by a colour.
  await expect(page.getByText(/ourway-agent --grant-input-permission|Accessibility/i).last()).toBeVisible();
});
