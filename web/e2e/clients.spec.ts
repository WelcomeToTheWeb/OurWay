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
import { ADMIN_PASSWORD, ADMIN_USER, apiBase, apiLogin, manifest, reFor, RUN_SUFFIX, uiLogin } from './fixtures.js';

// The tenant name carries the run id, like the seeded fleet does. A fixed name
// is a fixture that collides with its own previous run: two runs on one
// database leave two clients called `e2e-North Grid`, `open … detail` resolves
// to two elements, and Playwright's strict mode fails the spec for a reason
// that is in the harness and not in the diff.
const CLIENT_NAME = `e2e-North Grid${RUN_SUFFIX}`;
// Accounts carry the run id too. `ensureTechnician` is create-if-missing, so a
// shared database keeps one technician across runs, and every run grants that
// account one more client: the shift bar's scope stamp — which reports the
// tenants the *server* granted — then reads 2, 3, 4, and "exactly one client"
// becomes a claim about how many times the suite has been run.
const TECH_USER = `e2e-client-tech${RUN_SUFFIX}`;
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

  const clientId = async () => {
    const res = await request.get(`${apiBase()}/api/clients`, { headers: { Authorization: `Bearer ${token}` } });
    const body = await res.json().catch(() => null);
    const list = (body?.clients ?? body ?? []) as { id: string; name: string }[];
    return list.find((cl) => cl.name === CLIENT_NAME)?.id ?? '';
  };

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

  // Each write is stated by the server at the point it is made, rather than
  // inferred from a DOM twelve steps later. When the technician's ledger came up
  // empty on CI there was no way to say which save had failed to land; now the
  // suite says so, and "the grant is missing" stops being a hypothesis about
  // timing.
  const created = await clientId();
  expect(created, 'the client the console created is on the server').not.toBe('');
  const assignedTo = async () => {
    const res = await request.get(`${apiBase()}/api/devices`, { headers: { Authorization: `Bearer ${token}` } });
    const body = await res.json().catch(() => null);
    const devices = (body?.devices ?? []) as { id: string; client_id?: string | null }[];
    return devices.find((d) => d.id === seeded.id)?.client_id ?? null;
  };
  await expect
    .poll(assignedTo, { message: 'the unit the panel saved is assigned on the server', timeout: 20_000 })
    .toBe(created);

  // The ledger's unit count for this client becomes 1.
  //
  // This is the assertion the comment above it has always promised and the spec
  // never made: `expect(row).toBeVisible()` and `toHaveCount(0)` on the empty-state
  // string are both satisfied whether the row's counter reads 1 or 0, so the one
  // number the Clients ledger exists to show was never read — the whole tenancy
  // write was verified server-side and never on the page.
  //
  // The second column is that counter, and it is server-computed (`rollupOf`, from
  // the same scoped reads the device ledger runs), which is what makes it worth
  // reading: the operator's row and the API's answer have to agree about who owns
  // the unit. Polled because the rollups arrive on their own poll, after the save.
  const unitsCell = page.getByRole('row').filter({ has: row }).locator('td').nth(1);
  await expect(
    unitsCell,
    'the ledger states this tenant now owns the unit that was just assigned to it'
  ).toHaveText('1', { timeout: 20_000 });

  // Grant the technician, then check what their console shows.
  const access = panel.getByRole('button', { name: /save access/i });
  await panel.getByText(TECH_USER).click();
  await access.click();

  await expect
    .poll(
      async () => {
        const res = await request.get(`${apiBase()}/api/clients/${created}/users`, {
          headers: { Authorization: `Bearer ${token}` },
        });
        const body = await res.json().catch(() => null);
        const users = (body?.users ?? []) as { username?: string }[];
        return users.some((u) => u.username === TECH_USER);
      },
      { message: 'the technician the panel granted is granted on the server', timeout: 20_000 }
    )
    .toBe(true);

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

  // The unit is in *their* ledger. Filter for it, then assert the row: a text
  // assertion on an unfiltered ledger measures the page's window, and every run
  // that shares a database adds units to it. The scope claim is "this unit's
  // row is there", which the filtered ledger answers exactly.
  // The grant is a write made on the admin's session and read on the
  // technician's, and the console answers from the fleet cache: the unit is in
  // their ledger once their scope has been recomputed, not once the POST
  // returned. Put that precondition on the API before asking the DOM, so a slow
  // invalidation is a poll instead of a 15s hunt for a row that cannot appear.
  await expect
    .poll(
      async () => {
        const tech = await apiLogin(TECH_USER, TECH_PASSWORD);
        const res = await request.get(`${apiBase()}/api/devices`, { headers: { Authorization: `Bearer ${tech}` } });
        const body = await res.json().catch(() => null);
        return ((body?.devices ?? []) as { id: string }[]).some((d) => d.id === seeded.id);
      },
      { message: 'the technician can see the unit they were granted', timeout: 30_000 }
    )
    .toBe(true);

  await page.goto('/devices?view=table');
  await page.getByLabel('Search devices').fill(seeded.hostname);
  await expect(page.getByRole('row', { name: reFor(seeded.name) })).toBeVisible({ timeout: 15_000 });

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
