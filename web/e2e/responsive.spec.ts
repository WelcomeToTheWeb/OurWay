import { test, expect, type Locator, type Page, type APIRequestContext } from '@playwright/test';
import {
  ADMIN_PASSWORD,
  ADMIN_USER,
  RUN_SUFFIX,
  apiBase,
  manifest,
  uiLogin,
} from './fixtures.js';

/**
 * DESIGN.md §5 and §8, enforced on the running console.
 *
 * Two rules, both of which failed silently on shipped code and stayed
 * green through a full CI cycle, for one reason: the audit measured a bar that
 * had not been populated yet, and it measured an operator whose scope is the
 * fixed string `all tenants`.
 *
 *   - Nothing on the shell is cut off where nothing can reach what was cut. An
 *     instrument may be shed by breakpoint — that is what the ladder in §8 is
 *     for — but a shed instrument is absent, not half-visible.
 *   - A truncation counts as announced only if the ellipsis actually paints and
 *     the full value hangs off the cut element, because `text-overflow` neither
 *     inherits nor reaches an anonymous flex item.
 *
 * So the bar is measured **as a tenant**, not only as a fleet admin: a
 * technician granted one client whose name is long on purpose. That is the only
 * state in which the scope chip truncates, and the only state in which the
 * original defect — a tenant name pushing the live-link indicator past the edge
 * of the box — can occur at all. Everything else in the suite seeds tenants for
 * its own purposes; an admin's chip reads `all tenants`, which fits under its
 * cap at every width, so an admin-only sweep cannot see the truncation branch
 * run even once.
 *
 * The instruments arrive from three independent reads. Nothing here measures the
 * bar until the instrument that width promises has drawn its *value*, not its
 * caption: the caption `fleet` and the placeholder version `dev` are both on
 * screen before any read has answered.
 *
 * NOT guaranteed here: the live rate-limited state. CI raises the per-minute
 * budget to 2000, which usually keeps the suite under it — but the suite has
 * tripped it anyway, so the rate instrument's tests assert whichever of its two
 * states is on the bar rather than assuming one, and the component test drives
 * the flag directly.
 */

const WIDTHS = [1600, 1536, 1440, 1280, 1024, 768, 390];

/** Routes a granted technician may open. The header is identical on all of them. */
const ROUTES = ['/', '/devices', '/alerts', '/patches', '/audit'];

/** Long on purpose: this string is what makes the scope chip truncate. */
const TENANT = `Northwind Legal & Compliance Group${RUN_SUFFIX}`;
const ADMIN_SCOPE = 'all tenants';
const TECH_USER = `e2e-bar-tech${RUN_SUFFIX}`;
const TECH_PASSWORD = 'E2e-Bar-Tech!23';
const POLICY = `E2E bar maintenance window${RUN_SUFFIX}`;

const auth = (token: string) => ({ Authorization: `Bearer ${token}` });

async function adminToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${apiBase()}/api/auth/login`, {
    data: { username: ADMIN_USER, password: ADMIN_PASSWORD },
  });
  expect(res.ok(), `the admin login failed (${res.status()})`).toBeTruthy();
  return (await res.json()).access_token as string;
}

/** The scope chip, by its value — never by its caption, which is painted on frame one. */
const scopeChip = (page: Page, value: string): Locator => page.locator('header .stamp', { hasText: value });

/**
 * The tenant, the technician, and a policy with a real window.
 *
 * The policy matters as much as the tenant: `nextWindow()` answers null when no
 * policy has a window, and the bar then shows its `Unscheduled` placeholder at
 * 1536+ — the widest instrument on the bar, measured only in the state where it
 * says nothing at all. It is deleted afterwards so it cannot leak into the specs
 * that run after this one.
 */
let policyId = '';

test.beforeAll(async ({ request }) => {
  const token = await adminToken(request);

  const clients = await request.get(`${apiBase()}/api/clients`, { headers: auth(token) });
  expect(clients.ok(), `the client list failed (${clients.status()})`).toBeTruthy();
  const known: Array<{ id: string; name: string }> = (await clients.json())?.clients ?? [];
  let clientId = known.find((c) => c.name === TENANT)?.id ?? '';
  if (!clientId) {
    const created = await request.post(`${apiBase()}/api/clients`, { headers: auth(token), data: { name: TENANT } });
    expect(created.ok(), `creating the tenant failed (${created.status()})`).toBeTruthy();
    clientId = (await created.json())?.client?.id ?? '';
  }
  expect(clientId, 'the fixture tenant exists').toBeTruthy();

  // A unit inside the tenant, so the technician's ledger is not empty and a
  // zero-row pass is not available to the geometry checks below.
  const unit = manifest().units[2] ?? manifest().units[0];
  const assign = await request.put(`${apiBase()}/api/clients/${clientId}/devices`, {
    headers: auth(token),
    data: { device_ids: [unit.id] },
  });
  expect(assign.ok(), `assigning ${unit.hostname} failed (${assign.status()})`).toBeTruthy();

  const users = await request.get(`${apiBase()}/api/users`, { headers: auth(token) });
  const listed = users.ok() ? await users.json().catch(() => null) : null;
  const rows: Array<{ id?: string; username?: string }> = Array.isArray(listed) ? listed : (listed?.users ?? []);
  let techId = rows.find((u) => u.username === TECH_USER)?.id ?? '';
  if (!techId) {
    const created = await request.post(`${apiBase()}/api/users`, {
      headers: auth(token),
      data: { username: TECH_USER, email: `${TECH_USER}@example.invalid`, password: TECH_PASSWORD, roles: ['technician'] },
    });
    expect(created.ok(), `creating the technician failed (${created.status()})`).toBeTruthy();
    const body = await created.json().catch(() => null);
    techId = body?.user?.id ?? body?.id ?? '';
  }
  expect(techId, 'the fixture technician exists').toBeTruthy();

  // The grant is a full set, so a re-run re-states it rather than appending.
  const grant = await request.put(`${apiBase()}/api/clients/${clientId}/users`, {
    headers: auth(token),
    data: { user_ids: [techId] },
  });
  expect(grant.ok(), `granting the tenant failed (${grant.status()})`).toBeTruthy();

  const policy = await request.post(`${apiBase()}/api/patch/policies`, {
    headers: auth(token),
    data: { name: POLICY, scope: 'all', schedule: 'daily', window_start: '02:00', window_hours: 2, timezone: 'UTC' },
  });
  expect(policy.ok(), `creating the maintenance window failed (${policy.status()})`).toBeTruthy();
  policyId = (await policy.json())?.policy?.id ?? '';
  expect(policyId, 'the fixture policy has an id').toBeTruthy();
});

test.afterAll(async ({ request }) => {
  if (!policyId) return;
  const token = await adminToken(request);
  await request.delete(`${apiBase()}/api/patch/policies/${policyId}`, { headers: auth(token) });
});

/**
 * Wait until this width's instruments have drawn their values.
 *
 * Every wait names a thing that cannot exist before the read that fills it: the
 * server version once it is no longer the `dev` placeholder, the rate figure,
 * the scope chip's value, the window's `… · in 3h 12m`. A caption is never
 * acceptable here, because a caption is painted on the first frame.
 */
async function settleBar(page: Page, width: number, scopeValue: string): Promise<void> {
  await expect(page.locator('#main-content')).toBeVisible({ timeout: 15_000 });
  // The scope read is what the chip is waiting on, and it is the only wait that
  // works below `md` where the whole instrument box is hidden: attachment does
  // not care about `display: none`, visibility does.
  const chip = scopeChip(page, scopeValue);
  await expect(chip, `the scope read has landed (${scopeValue})`).toBeAttached({ timeout: 15_000 });
  if (width >= 768) {
    await expect(page.getByTitle(/WebSocket connected|WebSocket down/).first()).toBeVisible({ timeout: 15_000 });
  }
  if (width >= 1280) {
    await expect(chip).toBeVisible({ timeout: 15_000 });
    // One rate instrument, two states: the budget figure or the warning. Either
    // one is proof the version read has answered, because the figure renders
    // only from `rate_limit_per_minute` in that same response. The version
    // *string* cannot settle anything here: a CI server is built without release
    // ldflags, so it legitimately reports `dev` forever, and an assertion that
    // it stops saying so is a test that can only fail.
    await expect(page.getByTitle(/request budget|polling rests/i).first()).toBeVisible({ timeout: 15_000 });
  }
  if (width >= 1536) {
    // `formatWindow(policy) · untilLabel(at)`, and untilLabel is always "now" or
    // "in …" — the only text in the header with that shape.
    await expect(page.locator('header').getByText(/·\s*(now|in \d)/)).toBeVisible({ timeout: 15_000 });
  }
}

/**
 * Every box in the header that hides ink with nothing to reach it.
 *
 * Allowed, in the words of §8: a real scroller the user can move, or a
 * truncation that is *announced* — and announced means the ellipsis sits on an
 * element that can paint one. `.stamp` is an `inline-flex` pill: declaring
 * `text-overflow: ellipsis` on it sets the property and paints nothing, because
 * the text lives in an anonymous flex item the property does not reach. An
 * element is therefore trusted only if it is not a flex container and it owns a
 * text node — which is exactly the difference between the markup that shipped
 * and the markup that replaced it, and the reason this function reads the way it
 * does. Reverting the fix must turn this function red; that is its purpose.
 */
async function clippedBoxes(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const header = document.querySelector('header');
    if (!header) return ['no header rendered'];
    const findings: string[] = [];
    const CLIPS = new Set(['hidden', 'clip', 'container']);
    const classes = (el: Element) => (el.className || '').toString();
    const describe = (el: Element) =>
      `${el.tagName.toLowerCase()}.${classes(el).split(' ')[0]}: "${(el.textContent ?? '').trim().slice(0, 40)}"`;
    const ownsText = (el: Element) =>
      Array.from(el.childNodes).some((n) => n.nodeType === 3 && (n.textContent ?? '').trim() !== '');
    const canPaintEllipsis = (el: Element) => {
      const s = getComputedStyle(el);
      if (s.textOverflow !== 'ellipsis') return false;
      if (s.display.includes('flex')) return false; // paints nothing for text it does not own
      return ownsText(el);
    };
    const announces = (el: Element) => {
      // The element doing the clipping must be one that can paint an ellipsis —
      // that is the whole regression test, since the pill that shipped declared
      // `truncate` it could not paint and painted nothing. The full value may
      // hang off it or off the wrapper that sized it, because the wrapper is the
      // thing the operator's cursor is over.
      if (!canPaintEllipsis(el)) return false;
      return !!el.getAttribute('title') || !!el.parentElement?.getAttribute('title');
    };

    for (const el of [header, ...Array.from(header.querySelectorAll('*'))]) {
      if (classes(el).split(' ').includes('sr-only') || el.closest('.sr-only')) continue;
      let scrollable = false;
      for (let p = el.parentElement; p && p !== header.parentElement; p = p.parentElement) {
        const o = getComputedStyle(p).overflowX;
        if (o === 'auto' || o === 'scroll') scrollable = true;
      }
      if (scrollable) continue;

      const rect = el.getBoundingClientRect();
      const overX = el.scrollWidth - el.clientWidth;
      const overY = el.scrollHeight - el.clientHeight;
      if (overX <= 1 && overY <= 1) continue;
      const s = getComputedStyle(el);
      if (!CLIPS.has(s.overflowX) && !CLIPS.has(s.overflowY)) continue;

      // A box collapsed to almost nothing is the documented symptom, not a box
      // to skip: it is what an absorber does to a chip when the bar runs out,
      // and an empty pill is a value the operator does not have.
      if (rect.width < 4 || rect.height < 4) {
        findings.push(
          `collapsed to ${Math.round(rect.width)}×${Math.round(rect.height)} at ${Math.round(rect.x)},${Math.round(rect.y)}: ${describe(el)}`,
        );
        continue;
      }
      if (announces(el)) continue;
      const axis = overX > 1 && overY > 1 ? 'both axes' : overX > 1 ? 'horizontally' : 'vertically';
      findings.push(
        `${describe(el)} clipped ${axis} by ${Math.max(overX, overY)}px at ${Math.round(rect.x)},${Math.round(rect.y)} (${s.overflowX}/${s.overflowY}: no ellipsis it can paint, no title on it or its wrapper)`,
      );
    }
    return findings;
  });
}

/** The invariant the layout comment claims: the last instrument in the box is whole. */
async function linkWholeInBox(page: Page): Promise<string> {
  return page.evaluate(() => {
    const stamp = Array.from(document.querySelectorAll('header .stamp')).find((el) =>
      /WebSocket (connected|down)/.test(el.getAttribute('title') ?? ''),
    );
    if (!stamp) return 'no link indicator rendered';
    if (getComputedStyle(stamp).display === 'none') return 'shed';
    const box = stamp.closest('.overflow-hidden');
    if (!box) return 'no clipping box around the link indicator';
    const over = stamp.getBoundingClientRect().right - box.getBoundingClientRect().right;
    return over > 1 ? `link indicator runs ${Math.round(over)}px past the edge of the box` : 'inside';
  });
}

/** Sideways panning: the document, and the scrolling content column itself. */
async function panPixels(page: Page): Promise<number> {
  return page.evaluate(() => {
    const main = document.getElementById('main-content');
    const mainOver = main ? main.scrollWidth - main.clientWidth : 0;
    const docOver = document.documentElement.scrollWidth - document.documentElement.clientWidth;
    window.scrollTo(600, 0);
    return Math.max(mainOver, docOver, window.scrollX);
  });
}

test.describe('the shift bar as a tenant, which is the only state where it runs out of room', () => {
  test.use({ viewport: { width: 1280, height: 720 } });

  test('at every width, on every route a tenant can open, nothing on the bar is unreachable', async ({ page }) => {
    test.setTimeout(300_000);
    await uiLogin(page, TECH_USER, TECH_PASSWORD);
    const findings: string[] = [];
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 720 });
      for (const route of ROUTES) {
        await page.goto(route, { waitUntil: 'domcontentloaded' });
        await settleBar(page, width, TENANT);
        for (const f of await clippedBoxes(page)) findings.push(`${width} ${route}: ${f}`);
        const link = await linkWholeInBox(page);
        if (link !== 'inside' && link !== 'shed') findings.push(`${width} ${route}: ${link}`);
        const pan = await panPixels(page);
        if (pan > 1) findings.push(`${width} ${route}: the shell pans ${pan}px sideways`);
      }
    }
    expect(findings, `bar content hidden at ${WIDTHS.join('/')}px, as a granted tenant`).toEqual([]);
  });

  test('the scope chip shows a recognisable prefix of the tenant, never an empty pill', async ({ page }) => {
    await uiLogin(page, TECH_USER, TECH_PASSWORD);
    for (const width of [1280, 1440]) {
      await page.setViewportSize({ width, height: 720 });
      await page.goto('/devices', { waitUntil: 'domcontentloaded' });
      await settleBar(page, width, TENANT);
      const chip = scopeChip(page, TENANT);
      await expect(chip).toBeVisible();
      const box = await chip.boundingBox();
      expect(box, `the scope chip at ${width}px`).not.toBeNull();
      // Measured at ~90px for this fixture name at 1280. The floor exists to go
      // red if the absorber ever starves the chip into the empty pill that
      // started this branch: truncated-but-empty still passes the clip audit
      // above, because an empty pill announces itself perfectly.
      expect(box!.width, `the tenant name is legible at ${width}px`).toBeGreaterThanOrEqual(40);
    }
  });

  test('the bar sheds in the order DESIGN.md §8 states', async ({ page }) => {
    test.setTimeout(240_000);
    await uiLogin(page, TECH_USER, TECH_PASSWORD);
    const instruments: Array<{ label: string; gate: number; locator: (p: Page) => Locator }> = [
      // The caption `fleet` is what the chip is called; the tenant value is what
      // it says. Only the value is an instrument.
      { label: 'the tenant scope', gate: 1280, locator: (p) => scopeChip(p, TENANT) },
      { label: 'the live link', gate: 768, locator: (p) => p.getByTitle(/WebSocket connected|WebSocket down/).first() },
      // The clock's caption and figure are separate flex children, so the
      // element's text content is "UTC04:24:51" with no space in it and a regex
      // on the copy can never match. `<time datetime>` is locale-proof.
      { label: 'the UTC clock', gate: 640, locator: (p) => p.locator('header time[datetime]').first() },
      { label: 'the server version', gate: 1280, locator: (p) => p.getByTitle(/console build/i) },
      { label: 'the next maintenance window', gate: 1536, locator: (p) => p.locator('header').getByText(/·\s*(now|in \d)/) },
    ];
    for (const { label, gate, locator } of instruments) {
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 720 });
        await page.goto('/devices', { waitUntil: 'domcontentloaded' });
        await settleBar(page, width, TENANT);
        const found = locator(page);
        if (width >= gate) {
          await expect(found, `${label} at ${width}px`).toBeVisible({ timeout: 15_000 });
        } else {
          // An instrument that was never rendered is not "shed". A passing
          // toBeHidden() has to mean the CSS hid it, so it must be in the DOM.
          await expect(found, `${label} at ${width}px is missing, not shed`).toBeAttached({ timeout: 15_000 });
          await expect(found, `${label} at ${width}px`).toBeHidden();
        }
      }
    }
  });
});

test.describe('the shell as a fleet admin', () => {
  test('a ledger keeps its own overflow', async ({ page }) => {
    test.setTimeout(120_000);
    await uiLogin(page);
    for (const route of ['/audit', '/thresholds']) {
      await page.setViewportSize({ width: 390, height: 720 });
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      const scroller = page.locator('.ledger-scroll').first();
      await expect(scroller).toBeVisible({ timeout: 15_000 });
      // The claim is about a ledger with content in it: an empty table can be
      // wider than its column and prove nothing about reachability.
      await expect
        .poll(async () => scroller.locator('tbody tr').count(), { timeout: 15_000 })
        .toBeGreaterThan(0);
      const box = await scroller.evaluate((el) => {
        const s = getComputedStyle(el);
        const parent = el.parentElement!;
        return {
          content: el.scrollWidth,
          client: el.clientWidth,
          overflowX: s.overflowX,
          position: s.position,
          pastParent: Math.max(0, Math.round(el.getBoundingClientRect().right - parent.getBoundingClientRect().right)),
        };
      });
      const pan = await panPixels(page);
      expect(box.content, `${route} at 390px: the ledger must actually be wider than its box`).toBeGreaterThan(box.client);
      expect(box.overflowX, `${route} at 390px: .ledger-scroll scrolls, it does not clip`).toBe('auto');
      expect(
        ['relative', 'absolute', 'fixed', 'sticky'],
        `${route} at 390px: an unpositioned scroller leaks its overflow to the viewport, so .ledger-scroll is positioned`,
      ).toContain(box.position);
      expect(box.pastParent, `${route} at 390px: the ledger must not grow past its column`).toBeLessThanOrEqual(1);
      expect(pan, `${route} at 390px: the document must not pan to reach a column`).toBeLessThanOrEqual(1);
    }
  });

  test('the operator keeps their own identity and the way out at every width', async ({ page }) => {
    test.setTimeout(240_000);
    await uiLogin(page);
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 720 });
      await page.goto('/devices', { waitUntil: 'domcontentloaded' });
      await settleBar(page, width, ADMIN_SCOPE);

      // From `sm` the name is drawn beside the avatar; below it the avatar stays
      // and the name moves into the accessibility tree instead of vanishing.
      // Drawn means drawn: the sr-only copy is a 1px box, so measure instead of
      // asking Playwright whether something clipped is "visible".
      const widths = await page
        .locator('header')
        .getByText(ADMIN_USER, { exact: true })
        .evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().width)));
      const drawn = widths.filter((w) => w > 20);
      expect(widths.length, `the operator's name is somewhere on the bar at ${width}px`).toBeGreaterThan(0);
      if (width >= 640) {
        expect(drawn.length, `the operator's name is drawn at ${width}px`).toBeGreaterThan(0);
      } else {
        expect(drawn, `the name is shed below the sm breakpoint, not squeezed out, at ${width}px`).toHaveLength(0);
      }

      await expect(page.getByRole('searchbox', { name: /search all devices/i }), `the search field at ${width}px`).toBeVisible();
      const signOut = page.getByRole('button', { name: /sign out/i });
      await expect(signOut, `the way out at ${width}px`).toBeVisible();
      const where = await signOut.evaluate((el) => {
        const h = document.querySelector('header')!.getBoundingClientRect();
        const r = el.getBoundingClientRect();
        return r.left >= h.left - 1 && r.right <= h.right + 1 && r.top >= h.top - 1 && r.bottom <= h.bottom + 1
          ? 'inside'
          : 'outside';
      });
      expect(where, `the way out is inside the header at ${width}px`).toBe('inside');
    }
  });
});

test('the rate instrument is one instrument with two states, and the warning has no breakpoint', async ({
  page,
}) => {
  // The figure is a rate and waits for `xl` with the other rates. The warning
  // that replaces it when the server starts refusing us is the one bar
  // instrument with no breakpoint, because the condition it states is the one
  // that makes every other figure stale while the link chip beside it still
  // reads "Live". Which of the two is on the bar depends on the server, so this
  // asserts the pair instead of picking a side: exactly one of them is ever
  // present, the figure never below `xl`, and the warning — if the suite happens
  // to trip a limit at 390px, which it has done — is visible there too.
  test.setTimeout(180_000);
  await uiLogin(page);
  const figure = page.getByTitle(/request budget/i);
  const warning = page.getByTitle(/polling rests/i);
  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: 720 });
    await page.goto('/devices', { waitUntil: 'domcontentloaded' });
    await settleBar(page, width, ADMIN_SCOPE);
    if (width < 1280) {
      await expect(figure, `the budget figure is shed below xl at ${width}px`).toBeHidden();
    }
    if ((await warning.count()) > 0) {
      await expect(warning, `the rate-limit warning is never shed, and ${width}px is not too narrow`).toBeVisible();
    } else if (width >= 1280) {
      await expect(figure, `the budget figure at ${width}px`).toBeVisible();
    }
  }
});
