/**
 * 2.2-0 — login, the console shell, and the role gates.
 *
 * This is the suite's floor: if login or the shell breaks, every other surface
 * is unreachable. The role gate is asserted because 2.3 (tenancy) and 2.4
 * (identity linking) both depend on "viewer means read-only" being true.
 *
 * 2.3-1 added the other axis: scope decides *which* fleet a readable account
 * can read. The viewer here holds no client grants, so its fleet is empty —
 * asserted below, because an empty fleet is the answer tenancy owes an
 * ungranted account, and a console that quietly showed the fleet to it would
 * pass every role-gate test in this file.
 */
import { test, expect } from '@playwright/test';
import { ADMIN_USER, ADMIN_PASSWORD, VIEWER_USER, VIEWER_PASSWORD, apiBase, ensureViewer, manifest, uiLogin } from './fixtures.js';

test('signing in reaches the console and keeps the session across a reload', async ({ page }) => {
  await uiLogin(page);

  // The sign-in landed on the console, not on a form that accepted the click.
  await expect(page).toHaveURL(/\/$/);
  const shell = page.locator('h1').first();
  await expect(shell).toBeVisible();

  // The session is a real session: reload keeps the operator inside the console.
  await page.reload();
  await expect(page).not.toHaveURL(/\/login/);
  await expect(shell).toBeVisible();
});

test('a signed-in operator sees the console shell, the server version and the request budget', async ({ page, request }) => {
  await uiLogin(page);

  const { version } = await (await request.get(`${apiBase()}/api/agent/version`)).json();

  // The shift bar states the server version from the server's own answer
  // (docs/versioning.md), and the per-user request budget the console is
  // designed to live inside (2.2-3).
  const versionCell = page.getByTitle(/console build/i);
  await expect(versionCell).toBeVisible();
  await expect(versionCell).toContainText(version);

  const budgetCell = page.getByTitle(/request budget/i);
  await expect(budgetCell).toBeVisible();
  await expect(page.getByText(/\d+\/min/)).toBeVisible();

  // The shell's own navigation proves the operator is inside the console.
  await expect(page.getByRole('link', { name: /devices/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /alerts/i })).toBeVisible();
});

test('an unauthenticated visit to a console surface is bounced to login', async ({ page }) => {
  await page.goto('/alerts');
  await expect(page).toHaveURL(/\/login/);
  // The bounced page is the product's login, not a generic error.
  await expect(page.getByRole('heading', { level: 1, name: /sign in/i })).toBeVisible();
});

test('a viewer reads the fleet their scope names and is not offered destructive controls', async ({ page, request }, testInfo) => {
  // The viewer account exists through the API a console operator would use.
  const admin = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASSWORD },
  });
  const { access_token: token } = await admin.json();
  await ensureViewer(token, request);

  await uiLogin(page, VIEWER_USER, VIEWER_PASSWORD);

  await page.goto('/devices');
  await expect(page.getByRole('searchbox').first()).toBeVisible();

  // 2.3-1: a caller's fleet is their clients' devices, and a console account
  // with no client grants has **no** fleet — "no rows granted" must never read
  // as "sees everything". The seeded units belong to no client, so the viewer
  // is answered with the empty state, not with the fleet and not with an error.
  // This is the half of tenancy the role gate cannot express: viewer means
  // read-only, and scope means which fleet is readable.
  const seeded = manifest().units[0];
  await expect(page.getByText(seeded.hostname)).toHaveCount(0);
  await expect(page.getByText('No devices found')).toBeVisible();

  // A viewer is not offered the controls that change the fleet.
  await expect(page.getByRole('button', { name: /scan/i })).toHaveCount(0);
  void testInfo;
});
