/**
 * 2.3-8 — the tenancy flow, end to end.
 *
 * The scale suite (`server/api/scale_test.go`) measures the *cost* of the scoped
 * read, and the scoping matrix proves a route refuses an out-of-scope caller.
 * What neither can see is the operator's own sequence, the one an MSP helpdesk
 * performs ten times a shift:
 *
 *   sign in as a technician granted one client → the fleet ledger is their
 *   tenant → open a unit → do something to it → the audit ledger shows the act
 *   with their name on it.
 *
 * So this spec performs that sequence in the production bundle and asserts on
 * visible rows. It also re-reads the two surfaces 2.3-8 moved into SQL (the
 * alert ledger and the fleet patch sheet) and proves the scoped answer is the
 * tenant's answer, not the installation's answer trimmed in the handler — the
 * failure 2.3-8 exists to close is invisible to a correctness test, and a page
 * that quietly lists a foreign unit is the same failure seen from the console.
 *
 * Unit ownership: the fleet is split between specs (`e2e-North Grid` takes unit
 * 0, `e2e-Audit Grid` the last one), and a unit belongs to exactly one client,
 * so this spec claims the middle unit and no other spec's.
 */
import { test, expect } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";
import {
  ADMIN_PASSWORD,
  ADMIN_USER,
  apiBase,
  manifest,
  reFor,
  RUN_SUFFIX,
  seededTitle,
  uiLogin,
} from "./fixtures.js";

// Names carry the run id (see the note in `clients.spec.ts`): every one of these
// fixtures is create-if-missing, so a shared database keeps them and each run
// adds to them — a client with one more unit than the spec asserts, a technician
// with one more grant, an accessible name that resolves to two elements.
const CLIENT_NAME = `e2e-Tenancy Grid${RUN_SUFFIX}`;
const TECH_USER = `e2e-tenancy-tech${RUN_SUFFIX}`;
const TECH_PASSWORD = "E2eTenancyTech!23";

/** The unit this spec owns: the fleet's middle unit (see the header). */
const ownedUnit = () => manifest().units[1];
const foreignUnits = () =>
  manifest().units.filter((u) => u.hostname !== ownedUnit().hostname);

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASSWORD },
  });
  expect(res.ok(), `the admin login failed (${res.status()})`).toBeTruthy();
  const { access_token } = await res.json();
  return access_token as string;
}

/** Create (or reuse) the tenant, and return its id and slug. */
async function ensureClient(
  request: APIRequestContext,
  token: string,
): Promise<{ id: string; slug: string }> {
  const auth = { Authorization: `Bearer ${token}` };
  const list = await request.get(`${apiBase()}/api/clients`, { headers: auth });
  expect(list.ok(), `the client list failed (${list.status()})`).toBeTruthy();
  const clients: Array<{ id: string; name: string; slug: string }> =
    (await list.json())?.clients ?? [];
  const found = clients.find((c) => c.name === CLIENT_NAME);
  if (found) return { id: found.id, slug: found.slug };
  const created = await request.post(`${apiBase()}/api/clients`, {
    headers: auth,
    data: { name: CLIENT_NAME },
  });
  expect(
    created.ok(),
    `creating the tenant failed (${created.status()})`,
  ).toBeTruthy();
  const client = (await created.json())?.client;
  expect(client?.id, "the created tenant has an id").toBeTruthy();
  return { id: client.id, slug: client.slug };
}

/** Create (or reuse) the technician and grant them exactly this tenant. */
async function ensureTechnician(
  request: APIRequestContext,
  token: string,
  clientId: string,
): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  const usersRes = await request.get(`${apiBase()}/api/users`, {
    headers: auth,
  });
  const body = usersRes.ok() ? await usersRes.json().catch(() => null) : null;
  const users: Array<{ id?: string; username?: string }> = Array.isArray(body)
    ? body
    : (body?.users ?? []);
  const existing = users.find((u) => u.username === TECH_USER);
  const id = existing?.id;
  if (!id) {
    const created = await request.post(`${apiBase()}/api/users`, {
      headers: auth,
      data: {
        username: TECH_USER,
        email: `${TECH_USER}@example.invalid`,
        password: TECH_PASSWORD,
        roles: ["technician"],
      },
    });
    expect(
      created.ok(),
      `creating the technician failed (${created.status()})`,
    ).toBeTruthy();
    const createdBody = await created.json().catch(() => null);
    const newId = createdBody?.user?.id ?? createdBody?.id;
    expect(newId, "the created technician has an id").toBeTruthy();
    const grant = await request.put(
      `${apiBase()}/api/clients/${clientId}/users`,
      {
        headers: auth,
        data: { user_ids: [newId] },
      },
    );
    expect(
      grant.ok(),
      `granting the tenant failed (${grant.status()})`,
    ).toBeTruthy();
    return;
  }
  // Idempotent by inspection: the grant is a full set, so a re-run re-states it.
  const grant = await request.put(
    `${apiBase()}/api/clients/${clientId}/users`,
    {
      headers: auth,
      data: { user_ids: [id] },
    },
  );
  expect(
    grant.ok(),
    `re-granting the tenant failed (${grant.status()})`,
  ).toBeTruthy();
}

/**
 * Swap the browser's session.
 *
 * Park on `/login` **before** dropping the tokens: a mounted console page has
 * polls in flight, and a 401 answered by `window.location.replace('/login')`
 * steals the spec's own navigation. The login page is same-origin (so
 * `localStorage` is readable) and mounts no pollers.
 */
async function swapSession(
  page: Page,
  user: string,
  password: string,
): Promise<void> {
  await page.goto("/login");
  await page.evaluate(() => localStorage.clear());
  await uiLogin(page, user, password);
}

/**
 * The scope header the server applied to this page's requests.
 *
 * The scheme is spelled exactly `Bearer`: `server/api/middleware.go:21` trims
 * the prefix with `strings.TrimPrefix(authHeader, "Bearer ")`, so a lowercase
 * `bearer` leaves the token prefixed, the read answers 401, and a header
 * assertion on the 401 is an assertion on an empty string — which is exactly
 * how this spec's shape check reads as "passed" while testing nothing.
 */
async function scopeHeader(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const token = localStorage.getItem("ourway_access_token") ?? "";
    const res = await fetch("/api/devices", {
      headers: { authorization: `Bearer ${token}` },
    });
    if (res.status !== 200) return `http ${res.status}`;
    return res.headers.get("x-ourway-client-scope") ?? "";
  });
}

/** No raw i18n key path is on screen — a missing key renders its own path. */
async function expectNoKeyPaths(page: Page): Promise<void> {
  const text = await page.locator("body").innerText();
  expect(text).not.toMatch(
    /\b(devices|clients|common|sheet|alerts|audit|patches)\.[a-zA-Z]/,
  );
}

test("the tenancy flow: scoped sign-in, filtered ledger, unit action, audit row", async ({
  page,
  request,
}) => {
  const token = await adminToken(request);
  const client = await ensureClient(request, token);
  const unit = ownedUnit();

  // The admin does the two things the upgrade guide tells an operator to do
  // once: put the unit in a client, and grant a technician that client.
  const assign = await request.put(
    `${apiBase()}/api/clients/${client.id}/devices`,
    {
      headers: { Authorization: `Bearer ${token}` },
      data: { device_ids: [unit.id] },
    },
  );
  expect(
    assign.ok(),
    `assigning the unit failed (${assign.status()})`,
  ).toBeTruthy();
  await ensureTechnician(request, token, client.id);

  // 1. Scoped sign-in. A new session, a new scope — not the admin's session
  //    with a smaller filter.
  await swapSession(page, TECH_USER, TECH_PASSWORD);

  // 2. The filtered ledger: their tenant's unit, and no one else's.
  await page.goto("/devices?view=table");
  await expect(page.getByRole("row", { name: reFor(unit.name) })).toBeVisible({
    timeout: 15_000,
  });
  for (const foreign of foreignUnits()) {
    await expect(
      page.getByRole("row", { name: reFor(foreign.name) }),
    ).toHaveCount(0);
  }

  // The answer is the server's scope, stated on the response — the page is not
  // hiding rows the caller was handed.
  const scope = await scopeHeader(page);
  expect(
    scope,
    "the fleet read carries the scope the server applied",
  ).toBeTruthy();
  expect(
    scope,
    "the read must not have been refused — a 401 answers no scope header",
  ).not.toMatch(/^http /);
  expect(scope).not.toBe("all");
  expect(
    scope.split(",").length,
    "a technician granted one client is scoped to one client",
  ).toBe(1);
  await expectNoKeyPaths(page);

  // 3. A unit outside the tenant is not findable: the unit view answers the
  //    same way it answers a unit that does not exist.
  const foreign = foreignUnits()[0];
  await page.goto(`/devices/${foreign.id}`);
  await expect(
    page.getByRole("heading", { level: 1, name: foreign.name }),
  ).toHaveCount(0);
  await expect(page.getByRole("alert")).toContainText(/unavailable/i);

  // 4. The act on their own unit: a tag, through the unit view.
  await page.goto(`/devices/${unit.id}`);
  await expect(
    page.getByRole("heading", { level: 1, name: unit.name }),
  ).toBeVisible({ timeout: 15_000 });
  const tag = `tenant-scope-${Date.now() % 100000}`;
  const tagField = page.getByRole("textbox", { name: "Add tag" });
  await expect(tagField).toBeVisible();
  await tagField.fill(tag);
  await tagField.press("Enter");
  await expect(
    page.getByRole("button", { name: "Remove tag", exact: false }).first(),
  ).toBeVisible({ timeout: 10_000 });

  // 2.3-6's deep link is for the roles that can read the ledger. A technician
  // standing on this page has a machine and no ledger - `GET /api/audit` is a 403
  // for them - so the unit view must not offer the link. (The positive half is in
  // `audit.spec.ts`, followed through the admin's own click.)
  await expect(
    page.getByRole("link", { name: /Open the audit ledger for/ }),
  ).toHaveCount(0);

  // 5. The record. The writer runs on the answer, so poll the ledger: the row
  //    for this unit, performed by this technician, answered ok.
  const techLogin = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: TECH_USER, password: TECH_PASSWORD },
  });
  expect(
    techLogin.ok(),
    `the technician login failed (${techLogin.status()})`,
  ).toBeTruthy();
  const techToken = (await techLogin.json()).access_token as string;

  // The technician's own read of the ledger is a refusal — the log is an
  // operator surface, and the flow ends where the operator can see it.
  const refused = await request.get(`${apiBase()}/api/audit`, {
    headers: { Authorization: `Bearer ${techToken}` },
  });
  expect(refused.status(), "a technician may not read the audit ledger").toBe(
    403,
  );

  await expect
    .poll(
      async () => {
        const res = await request.get(
          `${apiBase()}/api/audit?action=device.tags`,
          {
            headers: { Authorization: `Bearer ${token}` },
          },
        );
        if (!res.ok()) return `status ${res.status()}`;
        const rows: Array<{
          action: string;
          actor_username: string;
          target_id: string;
          outcome: string;
        }> = (await res.json())?.logs ?? [];
        const row = rows.find(
          (r) => r.action === "device.tags" && r.target_id === unit.id,
        );
        if (!row) return "no row for this unit";
        if (row.actor_username !== TECH_USER)
          return `actor is ${row.actor_username}`;
        return row.outcome;
      },
      { timeout: 20_000 },
    )
    .toBe("ok");

  // The row is on the page, not only on the wire: an operator filtering the
  // ledger to tag changes finds the technician's name next to this unit's act.
  await swapSession(page, ADMIN_USER, ADMIN_PASSWORD);
  await page.goto("/audit?action=device.tags");
  const rows = page
    .locator("table.ledger tbody tr")
    .filter({ hasText: TECH_USER });
  await expect(rows.first()).toBeVisible({ timeout: 15_000 });
  await expect(rows.first()).toContainText(/device\.tags/);
  await expect(page.getByText(/Scope applied/i)).toBeVisible();
  await expectNoKeyPaths(page);
});

test("a scoped technician reads their tenant, not the installation trimmed", async ({
  page,
  request,
}) => {
  const token = await adminToken(request);
  // The tenant exists by the time this spec runs (the flow spec created it);
  // asking for it keeps the two specs on one tenant instead of minting a second.
  const client = await ensureClient(request, token);
  const unit = ownedUnit();
  const techLogin = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: TECH_USER, password: TECH_PASSWORD },
  });
  expect(
    techLogin.ok(),
    `the technician login failed (${techLogin.status()})`,
  ).toBeTruthy();
  const techToken = (await techLogin.json()).access_token as string;

  // Baseline first: prove the rows this spec later requires to be *absent*
  // exist on the installation. An assertion that passes because the row was
  // never there proves nothing.
  await uiLogin(page, ADMIN_USER, ADMIN_PASSWORD);

  await page.goto("/alerts");
  // The baseline names the unit that actually has alerts: the seed fleet gives
  // the workstation a critical load, and the Mac unit is healthy on purpose
  // (2.2-1 needs a unit that reports `denied`). Asserting a foreign unit's rows
  // are "gone" is only a result when they were there for the admin.
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: reFor(manifest().units[0].name) })
      .first(),
  ).toBeVisible({
    timeout: 15_000,
  });
  await page.goto("/patches");
  await expect(
    page.getByRole("row", { name: reFor(seededTitle(0, 0)) }),
  ).toBeVisible({ timeout: 15_000 });

  // The technician's reads.
  await swapSession(page, TECH_USER, TECH_PASSWORD);

  // The alert ledger: 2.3-8 moved this filter into SQL over the **client**, so
  // the page must show the tenant's alerts and none of the other tenants'.
  await page.goto("/alerts");
  await expect(
    page
      .getByRole("listitem")
      .filter({ hasText: reFor(unit.name) })
      .first(),
  ).toBeVisible({
    timeout: 15_000,
  });
  for (const foreign of foreignUnits()) {
    await expect(
      page.getByRole("listitem").filter({ hasText: reFor(foreign.name) }),
    ).toHaveCount(0);
  }
  const alertsRes = await request.get(`${apiBase()}/api/alerts?limit=200`, {
    headers: { Authorization: `Bearer ${techToken}` },
  });
  expect(
    alertsRes.ok(),
    `the technician's alert read failed (${alertsRes.status()})`,
  ).toBe(true);
  const { alerts } = await alertsRes.json();
  const foreignIds = new Set(foreignUnits().map((u) => u.id));
  const leaked = (alerts as Array<{ device_id: string }>).filter((a) =>
    foreignIds.has(a.device_id),
  );
  expect(
    leaked,
    "the alert ledger answers rows from a client this caller is not granted",
  ).toHaveLength(0);
  await expectNoKeyPaths(page);

  // The fleet patch sheet: compliance across "every unit" is the caller's
  // clients. `ListByStatusesScoped` is the same statement 2.3-8 reshaped.
  await page.goto("/patches");
  await expect(
    page.getByRole("row", { name: reFor(seededTitle(1, 0)) }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(
    page.getByRole("row", { name: reFor(seededTitle(0, 0)) }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("row", { name: reFor(seededTitle(0, 1)) }),
  ).toHaveCount(0);

  // And the clients ledger says out loud how many tenants this caller has:
  // exactly the one this flow granted, named by its slug. (No comparison with
  // the admin's count here — running this spec alone creates one tenant, and a
  // spec whose answer depends on which other specs ran before it is a spec that
  // fails for the wrong reason.)
  const techClients = await request.get(`${apiBase()}/api/clients`, {
    headers: { Authorization: `Bearer ${techToken}` },
  });
  expect(
    techClients.ok(),
    `the technician's client read failed (${techClients.status()})`,
  ).toBe(true);
  const techSees = (await techClients.json())?.clients ?? [];
  expect(
    techSees,
    "a technician granted one client reads exactly one tenant",
  ).toHaveLength(1);
  expect(
    techSees[0]?.slug,
    "the tenant they read is the one this flow granted",
  ).toBe(client.slug);

  const clientsRes = await request.get(`${apiBase()}/api/clients`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(clientsRes.ok()).toBe(true);
  const adminSees = ((await clientsRes.json())?.clients ?? []).length;
  expect(
    adminSees,
    "the admin reads at least the tenant this flow created",
  ).toBeGreaterThanOrEqual(1);
  await expectNoKeyPaths(page);
});

/**
 * 2.3-4c — the tenant's enrollment token, end to end.
 *
 * The flow an MSP operator runs: mint a credential for a tenant in the console,
 * hand it to the field tech, and the machine that enrolls with it is **in that
 * tenant** — no matching game in the unassigned pool, no window in which a unit
 * belongs to nobody.
 *
 * It is asserted through the console and the agent's own registration call,
 * because the claim is about the seam: the header the agent already sends, the
 * tenant column the server writes, and the ledger row that says who minted the
 * credential. The unit this enrolls is a machine of its own (its hostname
 * carries the run id), so it does not touch the units the other specs own.
 */
test("a tenant's enrollment token places a new unit in that tenant", async ({
  page,
  request,
}) => {
  const token = await adminToken(request);
  const client = await ensureClient(request, token);
  const hostname = `e2e-token-unit${RUN_SUFFIX.toLowerCase()}`;

  // The mint is a console act.
  await uiLogin(page, ADMIN_USER, ADMIN_PASSWORD);
  await page.goto("/clients");
  await page
    .getByRole("button", {
      name: new RegExp(`open ${CLIENT_NAME} detail`, "i"),
    })
    .click();
  const panel = page.getByLabel(`${CLIENT_NAME} detail`);
  // The heading, not a text match: "Enrollment tokens" is also inside the
  // empty state's sentence ("No enrollment tokens. …"), and a text locator that
  // resolves to two elements is Playwright's strict mode refusing to guess.
  await expect(
    panel.getByRole("heading", { name: "Enrollment tokens" }),
  ).toBeVisible();

  await panel.getByLabel("Token label").fill("field kit");
  await panel.getByRole("button", { name: "New token" }).click();
  const reveal = panel.locator("code").first();
  await expect(reveal).toBeVisible({ timeout: 10_000 });
  const secret = (await reveal.textContent())?.trim() ?? "";
  expect(secret, "the mint printed a token value").not.toBe("");
  // The prefix names which kind of secret an operator is pasting: `owk_` is an
  // API key that reads the installation, `owt_` places a machine in a tenant.
  expect(
    secret.startsWith("owt_"),
    `the token is not prefixed: ${secret}`,
  ).toBe(true);

  // The agent's side: the credential travels in the header it already sends.
  const registered = await request.post(`${apiBase()}/api/agent/register`, {
    headers: { "X-Enrollment-Secret": secret },
    data: {
      name: hostname,
      hostname,
      os: "linux",
      arch: "amd64",
      agent_version: "2.3.0",
      public_ip: "203.0.113.42",
      private_ip: `10.71.${RUN_SUFFIX.replace(/\D/g, "").slice(-3) || "1"}.42`,
    },
  });
  expect(
    registered.status(),
    `enrolling with a tenant token failed (${registered.status()})`,
  ).toBe(201);
  const enrolled = (await registered.json())?.device;
  expect(enrolled?.id, "the enrolled unit has an id").toBeTruthy();
  expect(
    enrolled?.client_id,
    "the unit enrolled with the tenant credential is in that tenant",
  ).toBe(client.id);

  // The console's tenant list shows the machine it now owns.
  await page.reload();
  await page
    .getByRole("button", {
      name: new RegExp(`open ${CLIENT_NAME} detail`, "i"),
    })
    .click();
  const reopened = page.getByLabel(`${CLIENT_NAME} detail`);
  // The panel names the machine twice — its own name and its hostname, both
  // carrying the string — so the assertion takes the first row it resolves to.
  await expect(reopened.getByText(hostname).first()).toBeVisible({
    timeout: 15_000,
  });

  // The secret is gone: the panel lists the credential, not its value. This is
  // the assertion a reviewer should expect a UI to fake, so it is stated
  // explicitly — the server stores a hash and has nothing to re-show.
  await expect(reopened.getByText(secret)).toHaveCount(0);

  // The mint is a record on the tenant, and it names no secret.
  const ledger = await request.get(
    `${apiBase()}/api/audit?action=client.token_create&client_id=${client.id}`,
    {
      headers: { Authorization: `Bearer ${token}` },
    },
  );
  expect(
    ledger.ok(),
    `the audit ledger refused the admin (${ledger.status()})`,
  ).toBe(true);
  const rows: Array<{
    action: string;
    client_id: string;
    details: string;
    outcome: string;
  }> = (await ledger.json())?.logs ?? [];
  const mintRow = rows.find(
    (r) => r.action === "client.token_create" && r.client_id === client.id,
  );
  expect(mintRow, "the mint wrote no audit row for this tenant").toBeTruthy();
  expect(mintRow?.outcome).toBe("ok");
  expect(
    mintRow?.details ?? "",
    "the audit row carries the secret",
  ).not.toContain(secret);

  await expectNoKeyPaths(page);
});
