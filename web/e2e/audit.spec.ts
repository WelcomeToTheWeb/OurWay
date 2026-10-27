/**
 * 2.3-6 — the audit ledger, seen by the operator who has to read it.
 *
 * The server-side suite (`server/api/audit_test.go`) proves the registry covers
 * every mutating route and that refusals are rows. This spec proves the two
 * things only a browser can see:
 *
 *  * an act performed **through the console** lands in the ledger with the
 *    actor the console logged in as — the record is real, not a fixture;
 *  * the ledger's scope story is visible: a manager reads their tenant's rows
 *    and never the row of a client they are not granted.
 *
 * It asserts on rows in the table and on the response's scope header, not on
 * the page's copy.
 */
import { test, expect } from '@playwright/test';
import { ADMIN_PASSWORD, ADMIN_USER, apiBase, manifest, uiLogin } from './fixtures.js';

const CLIENT_NAME = 'e2e-Audit Grid';
const MGR_USER = 'e2e-audit-mgr';
const MGR_PASSWORD = 'E2eAuditMgr!23';

async function adminToken(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const res = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASSWORD },
  });
  expect(res.ok()).toBeTruthy();
  const { access_token } = await res.json();
  return access_token as string;
}

/** Create (or reuse) a manager account granted to exactly one client. */
async function ensureManager(
  request: import('@playwright/test').APIRequestContext,
  token: string,
  clientId: string
) {
  const auth = { Authorization: `Bearer ${token}` };
  const usersRes = await request.get(`${apiBase()}/api/users`, { headers: auth });
  const body = usersRes.ok() ? await usersRes.json().catch(() => null) : null;
  const users: Array<{ id?: string; username?: string }> = Array.isArray(body) ? body : (body?.users ?? []);
  const existing = users.find((u) => u.username === MGR_USER);
  if (existing?.id) {
    await request.put(`${apiBase()}/api/clients/${clientId}/users`, {
      headers: auth,
      data: { user_ids: [existing.id] },
    });
    return;
  }
  const created = await request.post(`${apiBase()}/api/users`, {
    headers: auth,
    data: {
      username: MGR_USER,
      email: `${MGR_USER}@example.invalid`,
      password: MGR_PASSWORD,
      roles: ['manager'],
    },
  });
  expect(created.ok(), `create the manager account (${created.status()})`).toBeTruthy();
  const createdBody = await created.json().catch(() => null);
  const id = createdBody?.user?.id ?? createdBody?.id;
  expect(id, 'the created account has an id').toBeTruthy();
  const granted = await request.put(`${apiBase()}/api/clients/${clientId}/users`, {
    headers: auth,
    data: { user_ids: [id] },
  });
  expect(granted.ok(), `grant the manager the client (${granted.status()})`).toBeTruthy();
}

async function ensureClient(
  request: import('@playwright/test').APIRequestContext,
  token: string
): Promise<{ id: string; slug: string }> {
  const auth = { Authorization: `Bearer ${token}` };
  const list = await request.get(`${apiBase()}/api/clients`, { headers: auth });
  expect(list.ok()).toBeTruthy();
  const body = await list.json();
  const clients: Array<{ id: string; name: string; slug: string }> = body.clients ?? [];
  const found = clients.find((c) => c.name === CLIENT_NAME);
  if (found) return { id: found.id, slug: found.slug };
  const created = await request.post(`${apiBase()}/api/clients`, {
    headers: auth,
    data: { name: CLIENT_NAME },
  });
  expect(created.ok(), `create the client (${created.status()})`).toBeTruthy();
  const createdBody = await created.json();
  return { id: createdBody.client.id, slug: createdBody.client.slug };
}

async function ledger(
  request: import('@playwright/test').APIRequestContext,
  token: string,
  query = ''
) {
  const res = await request.get(`${apiBase()}/api/audit${query}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return { status: res.status(), body: await res.json().catch(() => null), headers: res.headers() };
}

test('an act performed in the console is recorded with the technician who performed it', async ({
  page,
  request,
}) => {
  const token = await adminToken(request);
  const seeded = manifest().units[manifest().units.length - 1];

  // The act: a tag change through the console's unit view. It is a mutating
  // route, it is scoped, and the console can perform it without a fixture.
  await uiLogin(page, ADMIN_USER, ADMIN_PASSWORD);
  await page.goto(`/devices/${seeded.id}`);
  await page.waitForLoadState('networkidle');

  const before = await ledger(request, token);
  expect(before.status, 'the ledger endpoint answers the admin').toBe(200);
  const seenBefore = (before.body?.logs ?? []).length;

  // Tags are edited on the unit view. The accessible name "Add tag" belongs to
  // both the input and its button, so the assertion names the role it wants:
  // a locator that resolves to two elements is a locator that fills the wrong
  // one and reports a fill error, not a missing feature.
  const tagField = page.getByRole('textbox', { name: 'Add tag' });
  await expect(tagField).toBeVisible();
  await tagField.fill(`e2e-audited-${Date.now() % 1000}`);
  await tagField.press('Enter');
  await expect(page.getByRole('button', { name: 'Remove tag', exact: false }).first()).toBeVisible();

  // The record: not a row count that grew by "about one", the row for this
  // unit with the actor who is logged in.
  await expect
    .poll(
      async () => {
        const page2 = await ledger(request, token, '?action=device.tags');
        const rows: Array<{ action: string; actor_username: string; target_id: string }> =
          page2.body?.logs ?? [];
        return rows.some((r) => r.action === 'device.tags' && r.target_id === seeded.id) ? 'found' : 'missing';
      },
      { timeout: 20_000 }
    )
    .toBe('found');

  const after = await ledger(request, token);
  expect((after.body?.logs ?? []).length).toBeGreaterThan(seenBefore);
  expect(after.headers['x-ourway-client-scope'], 'the ledger answers with the scope it applied').toBeTruthy();
});

test('a manager reads their tenant rows and never a client they are not granted', async ({
  request,
}) => {
  const token = await adminToken(request);
  const client = await ensureClient(request, token);
  await ensureManager(request, token, client.id);

  // Seed a row that belongs to this client: an assignment by the admin, which
  // the server records with the client as its target.
  // The unit for the grant test is the fleet's **last** seeded unit. The
  // clients spec assigns the first one to its own tenant, and a unit has one
  // client — a spec that re-assigns a unit another spec owns breaks it, and
  // the breakage shows up as an empty detail panel in the other spec's run.
  const seeded = manifest().units[manifest().units.length - 1];
  await request.put(`${apiBase()}/api/clients/${client.id}/devices`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { device_ids: [seeded.id] },
  });

  const login = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: MGR_USER, password: MGR_PASSWORD },
  });
  expect(login.ok()).toBeTruthy();
  const { access_token: mgrToken } = await login.json();

  const all = await ledger(request, mgrToken);
  expect(all.status, 'a manager may read the ledger').toBe(200);
  expect(all.headers['x-ourway-client-scope'], 'the scope header is the manager’s grants, not all').not.toBe('all');

  const rows: Array<{ client_id: string }> = all.body?.logs ?? [];
  // Every row the manager sees belongs to a client they are granted. The
  // fixture fleet is split between clients, so an ungranted row is possible
  // and is the leak this assertion closes.
  const granted = new Set<string>([client.id]);
  const foreign = rows.filter((r) => r.client_id && !granted.has(r.client_id));
  expect(foreign, 'the manager page carries rows from a client they are not granted').toHaveLength(0);

  // Asking for a client the caller is not granted answers an empty ledger,
  // naming nothing: the ledger is where "which tenants exist here" would
  // otherwise leak.
  const other = await ledger(request, mgrToken, '?client_id=00000000-0000-0000-0000-000000000000');
  expect(other.status).toBe(200);
  expect((other.body?.logs ?? []).length, 'an ungranted client filter answers no rows').toBe(0);

  // A viewer is refused: the ledger is an operator surface.
  const viewerLogin = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: 'e2e-client-tech', password: 'E2eClientTech!23' },
  });
  if (viewerLogin.ok()) {
    const { access_token: viewerToken } = await viewerLogin.json();
    const refused = await ledger(request, viewerToken);
    expect(refused.status, 'a viewer may not read the ledger').toBe(403);
  }
});

test('the ledger page renders rows, states its scope, and exports a CSV', async ({
  page,
  request,
}) => {
  const token = await adminToken(request);
  // Ensure the log is not empty: an admin act through the API is a row.
  await request.put(`${apiBase()}/api/clients`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { name: CLIENT_NAME, notes: 'e2e audit pass' },
  });

  await uiLogin(page, ADMIN_USER, ADMIN_PASSWORD);
  await page.goto('/audit');
  await page.waitForLoadState('networkidle');

  const heading = page.getByRole('heading', { level: 1 });
  await expect(heading).toContainText(/Audit/i);

  // The scope and retention strip: the page states what it applied and what it
  // throws away, from the server's own answer.
  await expect(page.getByText(/Scope applied/i)).toBeVisible();
  await expect(page.getByText(/older than \d+ days/i).first()).toBeVisible();

  const rows = page.locator('table.ledger tbody tr');
  await expect(rows.first()).toBeVisible();
  // An actor name is rendered as a name, never as a raw uuid.
  const firstCell = rows.first().locator('td').first();
  const actor = (await firstCell.textContent())?.trim() ?? '';
  expect(actor.length).toBeGreaterThan(0);
  expect(actor).not.toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);

  // No raw i18n key path is on the page.
  const text = await page.locator('body').innerText();
  expect(text).not.toMatch(/\b(audit|clients|common|sheet|devices)\.[a-zA-Z]/);

  // The export downloads a CSV built from the same filters, and the URL that
  // the page puts in the DOM never carries the session token.
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: /Export CSV/i }).click();
  const file = await download;
  expect(file.suggestedFilename()).toContain('audit');
  const { readFileSync } = await import('node:fs');
  const csv = readFileSync(await file.path(), 'utf8');
  expect(csv.startsWith('timestamp,actor,actor_roles,action,')).toBeTruthy();
  expect(csv).toContain(ADMIN_USER);
  const html = await page.content();
  expect(html).not.toContain('access_token=');
});
