import { test, expect, type Page } from '@playwright/test';
import { uiLogin } from './fixtures.js';

/**
 * The shift bar's shed order and the shell's overflow contract — the
 * machine-readable half of DESIGN.md §8, and the reason the hand-run geometry
 * audits it replaces are not enough.
 *
 * Two failure shapes are in scope, and they are opposite:
 *
 *   - The bar **pans the document** when an overflow leaks out of a scroller
 *     that is not positioned (DESIGN.md §5). The page slides sideways under a
 *     fixed header, and the same column becomes reachable two ways.
 *   - The bar **clips content with nothing to reach it**. An instrument pushed
 *     past the edge of the instrument box is not shed, it is gone: there is no
 *     scroller on the bar and no keyboard path to hidden pixels. A half-cut
 *     instrument also reads as a wrong value rather than a moved one.
 *
 * The suite runs at 1280×720, pinned in `playwright.config.ts`. The bar's
 * instruments are the point of those numbers: from `xl` the server version, the
 * rate instrument and the tenant scope arrive; `2xl` adds the next maintenance
 * window, the widest instrument and therefore the first to yield. UTC and the
 * live-link indicator hold from `sm`/`md`. The search field, the identity
 * cluster and the sign-out never yield at any width.
 */

const WIDTHS = [1600, 1536, 1440, 1280, 1024, 768, 390];
const ROUTES = ['/', '/devices', '/audit', '/alerts', '/patches', '/thresholds', '/sso'];

/**
 * The instruments arrive from three independent sources — the version call, the
 * first scoped read's `X-OurWay-Client-Scope` header, and the patch-policy
 * poller — so a measurement taken as soon as `<main>` paints runs against a bar
 * that is still half empty, and an empty bar never overflows. Wait for the
 * instruments this width is supposed to carry before measuring it.
 */
async function settleBar(page: Page, width: number) {
  await expect(page.locator('#main-content')).toBeVisible();
  if (width >= 768) {
    await expect(page.getByTitle(/WebSocket connected|WebSocket down/).first()).toBeVisible({ timeout: 15_000 });
  }
  if (width >= 1280) {
    // The version instrument is the first thing the version call puts on the
    // bar, and the rate instrument and scope follow from the same two reads.
    await expect(page.getByTitle(/console build/i)).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTitle(/request budget|rate limit reached/i)).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('fleet', { exact: true })).toBeVisible({ timeout: 15_000 });
  }
  if (width >= 1536) await expect(page.getByText('Next window', { exact: true })).toBeVisible({ timeout: 15_000 });
}

/**
 * Walks the header for a box that hides ink with nothing to reach it.
 *
 * Allowed, in the words of DESIGN.md §8: a real scroller (`overflow-x: auto`)
 * that the user can move, or a truncation that is *announced* — an ellipsis that
 * actually paints, and the full value on that same element or the one wrapping
 * it, so a hover says what was cut. `text-overflow` does not inherit, so an
 * ellipsis declared on an `inline-flex` pill does not reach the text inside it;
 * that is why the check tests the clipped element itself and not a distant
 * ancestor, and why a `title` three levels up does not excuse a silent cut.
 */
async function unreachableBarContent(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const header = document.querySelector('header');
    if (!header) return ['no header rendered'];
    const CLIPS = new Set(['hidden', 'clip', 'container']);
    const boxes = new Set<Element>([header.parentElement?.parentElement ?? header, header.parentElement ?? header, header]);
    header.querySelectorAll('*').forEach((el) => boxes.add(el));

    const srOnly = (el: Element) => el.classList.contains('sr-only');
    const insideScroller = (el: Element) => {
      // Bounded to the header subtree: an app-wide horizontal scroller would
      // otherwise make this check stop checking without anyone noticing.
      for (let p = el.parentElement; p && header.contains(p); p = p.parentElement) {
        const sx = getComputedStyle(p).overflowX;
        if (sx === 'auto' || sx === 'scroll') return true;
      }
      return false;
    };
    const announced = (el: Element, style: CSSStyleDeclaration) => {
      if (style.textOverflow !== 'ellipsis') return false;
      const holder = [el, el.parentElement].find((n) => n && n.hasAttribute('title'));
      return Boolean(holder);
    };

    const findings: string[] = [];
    for (const el of boxes) {
      const style = getComputedStyle(el);
      const xClips = CLIPS.has(style.overflowX);
      const yClips = CLIPS.has(style.overflowY);
      if (!xClips && !yClips) continue;
      if (srOnly(el) || insideScroller(el)) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width < 2 || rect.height < 2) continue;
      const overX = xClips ? el.scrollWidth - el.clientWidth : 0;
      const overY = yClips ? el.scrollHeight - el.clientHeight : 0;
      if (overX <= 1 && overY <= 1) continue;
      if (announced(el, style)) continue;
      const label = (el.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 24);
      findings.push(`${el.tagName.toLowerCase()}.${el.className.toString().split(' ').slice(0, 2).join('.')} cuts ${Math.max(overX, overY)}px with nothing to reach it ("${label}")`);
    }
    return findings;
  });
}

/** The document must not be pannable sideways, nor wider than its viewport. */
async function horizontalPanning(page: Page): Promise<number> {
  const intrinsic = await page.evaluate(() => {
    const root = document.scrollingElement ?? document.documentElement;
    return root.scrollWidth - root.clientWidth;
  });
  await page.evaluate(() => window.scrollTo(600, 0));
  const scrolled = await page.evaluate(() => window.scrollX);
  await page.evaluate(() => window.scrollTo(0, 0));
  return Math.max(intrinsic, scrolled);
}

test.describe('the shell holds its overflow inside itself', () => {
  test('nothing on the shell hides behind a clip', async ({ page }) => {
    // 7 routes × 7 widths, each with a fresh load and a settle wait for three
    // async sources; the default 30s per test is not enough on a cold runner.
    test.setTimeout(300_000);
    await uiLogin(page);

    for (const width of WIDTHS) {
      for (const route of ROUTES) {
        await page.setViewportSize({ width, height: 900 });
        await page.goto(`${route}`, { waitUntil: 'domcontentloaded' });
        await settleBar(page, width);

        const pan = await horizontalPanning(page);
        expect(pan, `${route} at ${width}px lets the document pan sideways`).toBeLessThanOrEqual(1);

        const clipped = await unreachableBarContent(page);
        expect(clipped, `${route} at ${width}px hides content behind a clip`).toEqual([]);
      }
    }
  });

  test('a ledger scrolls inside its own box while the page stands still', async ({ page }) => {
    test.setTimeout(120_000);
    await uiLogin(page);

    for (const route of ['/audit', '/thresholds']) {
      await page.setViewportSize({ width: 390, height: 900 });
      await page.goto(`${route}`, { waitUntil: 'domcontentloaded' });
      const scroller = page.locator('.ledger-scroll').first();
      await expect(scroller).toBeVisible();

      const box = await scroller.evaluate((el) => {
        const parent = el.parentElement;
        return {
          client: el.clientWidth,
          content: el.scrollWidth,
          pastParent: parent ? Math.round(el.getBoundingClientRect().right - parent.getBoundingClientRect().right) : 0,
          overflowX: getComputedStyle(el).overflowX,
          position: getComputedStyle(el).position,
        };
      });
      // The table is genuinely wider than the phone, the box really is the
      // scroller, and it is positioned — which is what keeps its scrollable
      // overflow from propagating to the viewport (DESIGN.md §5).
      expect(box.content).toBeGreaterThan(box.client);
      expect(box.overflowX).toBe('auto');
      expect(['relative', 'absolute', 'fixed', 'sticky']).toContain(box.position);
      expect(box.pastParent, `${route} leaks its ledger past its own box`).toBeLessThanOrEqual(1);

      expect(await horizontalPanning(page), `${route} pans the document at 390px`).toBeLessThanOrEqual(1);
    }
  });
});

test.describe('the shift bar sheds in the order DESIGN.md §8 states', () => {
  test.beforeEach(async ({ page }) => {
    test.setTimeout(120_000);
    await uiLogin(page);
    await page.goto('/devices', { waitUntil: 'domcontentloaded' });
  });

  const instruments = [
    { label: 'the live link', gate: 768, locator: (page: Page) => page.getByTitle(/WebSocket connected|WebSocket down/).first() },
    { label: 'the UTC clock', gate: 640, locator: (page: Page) => page.getByText(/^UTC\s/).first() },
    { label: 'the server version', gate: 1280, locator: (page: Page) => page.getByTitle(/console build/i) },
    { label: 'the rate instrument', gate: 1280, locator: (page: Page) => page.getByTitle(/request budget|rate limit reached/i) },
    { label: 'the tenant scope', gate: 1280, locator: (page: Page) => page.getByText('fleet', { exact: true }) },
    { label: 'the next maintenance window', gate: 1536, locator: (page: Page) => page.getByText('Next window', { exact: true }) },
  ];

  // One test per instrument, looping the widths inside it: each test costs a
  // UI login, and a 36-test matrix would dominate the job for no extra reach.
  for (const { label, gate, locator } of instruments) {
    test(`${label} is on the bar from ${gate}px and shed below it`, async ({ page }) => {
      test.setTimeout(120_000);
      for (const width of [1600, 1536, 1280, 1024, 768, 390]) {
        await page.setViewportSize({ width, height: 900 });
        const found = locator(page);
        // `toBeVisible` polls, so this waits for the instrument to arrive
        // instead of measuring the bar before the read that fills it lands.
        if (width >= gate) await expect(found, `${label} at ${width}px`).toBeVisible({ timeout: 15_000 });
        else await expect(found, `${label} at ${width}px`).toBeHidden();
      }
    });
  }

  test('the operator’s identity and the way out are never shed', async ({ page }) => {
    test.setTimeout(120_000);
    for (const width of [1600, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await settleBar(page, width);
      const search = page.getByRole('searchbox', { name: /search all devices/i });
      const signOut = page.getByRole('button', { name: /sign out/i });
      await expect(search, `search at ${width}px`).toBeVisible();
      await expect(signOut, `sign-out at ${width}px`).toBeVisible();

      const where = await signOut.evaluate((el) => {
        const header = document.querySelector('header');
        if (!header) return 'no header';
        const box = header.getBoundingClientRect();
        const r = el.getBoundingClientRect();
        return r.right <= box.right + 1 && r.left >= box.left - 1 ? 'inside' : `outside (${Math.round(r.right)} vs ${Math.round(box.right)})`;
      });
      expect(where, `sign-out at ${width}px`).toBe('inside');
    }
  });
});
