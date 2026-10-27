/**
 * 2.2-0 — the patch ledger and the approve action.
 *
 * The updates arrive through `POST /api/agent/updates`, the agent reporter's
 * own call, so the ledger is exercised the way the product fills it. Approve is
 * the action 2.2-9/2.2-10 build on, so its durability is pinned here.
 */
import { test, expect } from '@playwright/test';
import { manifest, reFor, seededTitle, uiLogin } from './fixtures.js';

const unitIndex = 0;

/**
 * `/patches` is the fleet sheet (PatchHub); the per-unit ledger with the Device
 * selector lives behind the "By unit" tab (`?tab=device`). With no unit selected
 * it shows the "choose a device" empty state, so the harness selects the seeded
 * unit before asserting on its rows.
 */
async function openPatchesFor(page: import('@playwright/test').Page, unitId: string) {
  await page.goto('/patches?tab=device');
  // The label element wraps the caption and the select, so name the control
  // itself: an accessible-name query matches both.
  await page.locator('select[aria-label="Device"]').selectOption(unitId);
}

test('the patch ledger lists the updates detected on the seeded unit', async ({ page }) => {
  const unit = manifest().units[unitIndex];
  await uiLogin(page);
  await openPatchesFor(page, unit.id);

  await expect(page.getByRole('row', { name: reFor(seededTitle(unitIndex, 0)) })).toBeVisible();
  await expect(page.getByRole('row', { name: reFor(seededTitle(unitIndex, 1)) })).toBeVisible();
});

test('approving an update moves it to approved and stays approved', async ({ page }) => {
  const unit = manifest().units[unitIndex];
  const title = seededTitle(unitIndex, 0);

  await uiLogin(page);
  await openPatchesFor(page, unit.id);

  const row = page.getByRole('row', { name: reFor(title) }).first();
  await expect(row).toContainText('Detected');

  await row.getByRole('button', { name: /approve/i }).click();

  await expect(row).toContainText('Approved', { timeout: 10_000 });

  // Durable, not optimistic: a reload re-reads it from the server.
  await page.reload();
  await openPatchesFor(page, unit.id);
  await expect(page.getByRole('row', { name: reFor(title) }).first()).toContainText('Approved');
});
