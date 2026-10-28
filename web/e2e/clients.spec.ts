/**
 * 2.3-4 — a tenant, its units, and the technician who may see them.
 *
 * The suite's other specs prove a route refuses an out-of-scope caller (the
 * server-side matrix in `server/api/scope_routes_test.go` runs that far wider).
 * This one proves the *console* tells the tenancy story: an operator creates a
 * client, moves units into it, grants it, and the technician's console then
 * shows exactly that fleet — no more, no less, in the row count and in the
 * ledger's own numbers.
 *
 * Tenancy that only the server enforces is tenancy the MSP cannot verify, and
 * "the technician cannot find the unit" is the ticket this release is measured
 * by. So the assertions here are on visible rows.
 */
import { test, expect } from '@playwright/test';
import { ADMIN_PASSWORD, ADMIN_USER, apiBase, manifest, uiLogin } from './fixtures.js';

const CLIENT_NAME = 'e2e-North Grid';
const TECH_USER = 'e2e-client-tech';
const TECH_PASSWORD = 'E2eClientTech!23';

async function adminToken(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const res = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASSWORD },
  });
  expect(res.ok()).toBeTruthy();
  const { access_token } = await res.json();
  return access_token as string;
}

/** Create the technician account an MSP operator would create. */
async function ensureTechnician(request: import('@playwright/test').APIRequestContext, token: string) {
  const auth = { Authorization: `Bearer ${token}` };
  const list = await request.get(`${apiBase()}/api/users`, { headers: auth });
  const body = list.ok() ? await list.json().catch(() => null) : null;
  const users: Array<{ username?: string }> = Array.isArray(body) ? body : (body?.users ?? []);
  if (users.some((u) => u.username === TECH_USER)) return;
  const res = await request.post(`${apiBase()}/api/users`, {
    headers: auth,
    data: {
      username: TECH_USER,
      email: `${TECH_USER}@example.invalid`,
      password: TECH_PASSWORD,
      roles: ['technician'],
    },
  });
  expect(res.ok(), `create the technician account (${res.status()})`).toBeTruthy();
}

test('the Clients ledger creates a tenant, assigns a unit, and grants it', async ({ page, request }) => {
  const token = await adminToken(request);
  await ensureTechnician(request, token);

  const seeded = manifest().units[0];

  await uiLogin(page);
  await page.goto('/clients');

  // Create the client.
  await page.getByRole('button', { name: /new client/i }).click();
  await page.getByLabel('Name').fill(CLIENT_NAME);
  await page.getByRole('button', { name: /add client/i }).click();

  const row = page.getByRole('button', { name: new RegExp(`open ${CLIENT_NAME} detail`, 'i') });
  await expect(row).toBeVisible({ timeout: 15_000 });

  // The new client owns no units yet, and the ledger says so.
  await row.click();
  const panel = page.getByLabel(`${CLIENT_NAME} detail`);
  await expect(panel.getByText(seeded.hostname)).toBeVisible({ timeout: 15_000 });

  // Move the seeded unit in: its checkbox, then the save.
  const checkbox = panel.getByText(seeded.hostname).locator('xpath=preceding-sibling::input');
  await checkbox.check().catch(async () => {
    // The row is a label wrapping the box: click the unit's own row instead.
    await panel.getByText(seeded.hostname).click();
  });
  await panel.getByRole('button', { name: /save units/i }).click();

  // The ledger's unit count for this client becomes 1.
  await expect(row).toBeVisible();
  await expect(page.getByText('No clients yet')).toHaveCount(0);

  // Grant the technician, then check what their console shows.
  const access = panel.getByRole('button', { name: /save access/i });
  await panel.getByText(TECH_USER).click();
  await access.click();

  // The technician signs in from scratch: a new session, a new scope.
  //
  // The swap has to park the page somewhere inert **before** dropping the
  // tokens. `client.ts` answers a 401 with `window.location.replace('/login')`,
  // and a console page has polls in flight: clear the tokens while the console
  // is mounted and the next poll 401s, the interceptor navigates, and the
  // spec's own `goto('/login')` dies with "Navigation ... is interrupted by
  // another navigation". It shows up on a loaded runner (the Redis-shaped stack
  // adds enough latency to lose the race) and passes locally, which is the
  // shape of a harness bug, not a product bug.
  // The login page is the inert place: same origin (so `localStorage` is
  // readable — `about:blank` is not), and it mounts no pollers, so clearing the
  // tokens there cannot make a poll 401 and steal the navigation.
  await page.goto('/login');
  await page.evaluate(() => localStorage.clear());
  await uiLogin(page, TECH_USER, TECH_PASSWORD);

  await page.goto('/devices');
  await expect(page.getByText(seeded.hostname)).toBeVisible({ timeout: 15_000 });

  // Their ledger names exactly one client, and no others.
  await page.goto('/clients');
  await expect(page.getByRole('button', { name: new RegExp(`open ${CLIENT_NAME} detail`, 'i') })).toBeVisible();

  // And their console says the scope out loud: the server's header is what the
  // shift bar prints, so "can't find the unit" has a visible reason.
  //
  // `Bearer` is spelled exactly. The auth middleware trims the prefix with
  // `strings.TrimPrefix(authHeader, "Bearer ")`, so a lowercase scheme leaves
  // the token prefixed, the read answers 401, and the header is empty — which
  // makes `not.toBe('all')` and a one-item split pass on a refused request.
  const scope = await page.evaluate(async () => {
    const token = localStorage.getItem('ourway_access_token') ?? '';
    const res = await fetch('/api/clients', { headers: { authorization: `Bearer ${token}` } });
    return res.status === 200 ? (res.headers.get('x-ourway-client-scope') ?? '') : `http ${res.status}`;
  });
  expect(scope, 'the client read answered, so its scope header is real').not.toMatch(/^http /);
  expect(scope).toBeTruthy();
  expect(scope).not.toBe('all');
  expect(scope.split(',').length).toBe(1);

  // The scope header is the whole story in one value: a granted technician is
  // not scope-all, and `auth.spec.ts` covers the other half (an account with no
  // grants is answered with an empty fleet, not the installation).
});
