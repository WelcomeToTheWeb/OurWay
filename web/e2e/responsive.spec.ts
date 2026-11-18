/**
 * The width contract, asserted.
 *
 * Why this file exists: a change to the console once passed a 90-surface
 * geometry audit and still failed `Console end-to-end`, because a geometry audit
 * measures *overflow* while DESIGN.md §8 makes **promises** — the server version
 * and the request budget belong to the operator wherever the bar can hold them,
 * and the operator's own name and the way out never give way to an instrument.
 * A promise needs a test. Measuring pixels cannot find a control that is gone.
 *
 * Two distinct things are asserted here:
 *
 *   1. **Nothing is unreachable.** The page never pans sideways (the Chromium
 *      rule behind `.ledger-scroll`, DESIGN.md §5), and no box on the shift bar
 *      hides content that has no way to reach it. Truncation is allowed where it
 *      is announced by a `title`; a silent cut is not.
 *   2. **Which instruments are on the bar, at which width** — in both
 *      directions: present where promised, and *gone* where they are meant to
 *      yield, because a half-shown instrument is worse than a shed one.
 *
 * The suite runs at 1280×720, now pinned in `playwright.config.ts`. The bar's
 * instruments are the point of those numbers: `srv`, `budget` and the tenant
 * scope arrive at `xl` (1280), `UTC` at `sm` (640), `Next window` only at `2xl`
 * (1536) — the widest instrument yields first, and the search field, the
 * identity cluster and the sign-out never do.
 */
import { test, expect, type Page } from '@playwright/test';
import { uiLogin } from './fixtures.js';

/** The widths DESIGN.md §8 names, plus one that clears every breakpoint. */
const WIDTHS = [1440, 1280, 1024, 768, 390] as const;

/** Ledger-heavy surfaces an admin can always reach. /sso and /thresholds are
 *  here deliberately: they are the two ledgers whose columns were found panning
 *  the whole document 541px and 353px before `.ledger-scroll` existed. */
const ROUTES = ['/', '/devices', '/audit', '/alerts', '/patches', '/thresholds', '/sso'] as const;

/**
 * The offset the document moves when asked to scroll sideways. Any number above
 * zero means a scroller is leaking its overflow to the page, so the same column
 * is reachable two ways at once (DESIGN.md §5).
 */
async function panOffset(page: Page): Promise<number> {
  return page.evaluate(() => {
    window.scrollTo(0, 0);
    window.scrollTo(600, 0);
    const offset = window.scrollX;
    window.scrollTo(0, 0);
    return offset;
  });
}

/**
 * Content on the shift bar that is cut off with no way to reach it.
 *
 * A box counts as a violation only when it hides pixels and the operator has no
 * way to get them: not when the truncation is announced by a `title` (§8 allows
 * "announced or reachable"), not when a real scroller holds the overflow, and not
 * for a box whose overflow escapes to the page — the pan check owns that case.
 * The bar's own clipping wrappers are inspected as well, since they sit above
 * `header` and would swallow the overflow instead of showing it.
 */
async function unreachableBarContent(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const header = document.querySelector('header');
    if (!header) return ['the console shell rendered without a header bar'];

    const label = (el: Element) => {
      const cls = typeof el.className === 'string' ? el.className.trim().split(/\s+/)[0] : '';
      return `${el.tagName.toLowerCase()}${cls ? `.${cls}` : ''}`;
    };
    const announced = (el: Element) => {
      for (let node: Element | null = el, depth = 0; node && depth < 4; node = node.parentElement, depth += 1) {
        if (node.getAttribute('title')) return true;
      }
      return false;
    };
    const insideScroller = (el: Element) => {
      for (let node = el.parentElement; node; node = node.parentElement) {
        const overflowX = getComputedStyle(node).overflowX;
        if (overflowX === 'auto' || overflowX === 'scroll') return true;
      }
      return false;
    };

    const boxes: Element[] = [];
    for (let node: Element | null = header.parentElement, depth = 0; node && depth < 2; node = node.parentElement, depth += 1) {
      boxes.push(node);
    }
    boxes.push(header, ...Array.from(header.querySelectorAll('*')));

    const found: string[] = [];
    for (const box of boxes) {
      const el = box as HTMLElement;
      if (el.getClientRects().length === 0) continue;
      const style = getComputedStyle(el);
      if (style.overflowX !== 'hidden') continue;
      if (el.scrollWidth <= el.clientWidth + 1) continue;
      if (style.textOverflow === 'ellipsis' && announced(el)) continue;
      if (insideScroller(el)) continue;
      found.push(`${label(el)} cuts off ${el.scrollWidth - el.clientWidth}px with nothing to reach it`);
    }

    // The two controls the bar may never lose, and may never push off itself.
    for (const selector of ['button[aria-label="Sign out"]', 'input[type="search"]']) {
      const el = header.querySelector(selector);
      if (!el) {
        found.push(`the shift bar has no ${selector}`);
        continue;
      }
      const bar = header.getBoundingClientRect();
      const box = el.getBoundingClientRect();
      if (box.width <= 0 || box.height <= 0) {
        found.push(`${selector} has no box on the bar`);
        continue;
      }
      const outside = Math.round(Math.max(0, box.right - bar.right, bar.left - box.left));
      if (outside > 1) found.push(`${selector} sits ${outside}px outside the header box`);
    }

    return found;
  });
}

for (const width of WIDTHS) {
  test(`nothing on the shell hides behind a clip at ${width}px`, async ({ page }) => {
    test.setTimeout(120_000);
    await uiLogin(page);
    await page.setViewportSize({ width, height: 900 });

    for (const route of ROUTES) {
      await page.goto(route);
      // The route answered as itself, not bounced to login: an assertion that
      // never lands is how a width check quietly stops checking.
      await expect(page.locator('#main-content'), `${route} at ${width}px did not render the console shell`).toBeVisible();

      expect(await panOffset(page), `${route} at ${width}px: the document pans sideways, so a scroller is leaking its overflow (DESIGN.md §5)`).toBe(0);
      expect(await unreachableBarContent(page), `${route} at ${width}px: the shift bar hides content the operator cannot reach`).toEqual([]);
    }
  });
}

test('a ledger scrolls inside its own box while the page stands still', async ({ page }) => {
  await uiLogin(page);
  await page.setViewportSize({ width: 390, height: 900 });

  for (const route of ['/audit', '/thresholds']) {
    await page.goto(route);
    const scroller = page.locator('.ledger-scroll').first();
    await expect(scroller, `${route} at 390px: no .ledger-scroll box, so this test has stopped proving the fix`).toBeVisible();

    const box = await scroller.evaluate((el) => ({
      client: el.clientWidth,
      content: el.scrollWidth,
      offsetX: getComputedStyle(el).overflowX,
    }));
    expect(
      box.content,
      `${route} at 390px: the ledger is only ${box.content}px wide inside ${box.client}px — it is no wider than its box, so this test proves nothing`,
    ).toBeGreaterThan(box.client);
    expect(
      await scroller.evaluate((el) => Math.round(el.getBoundingClientRect().right) - Math.round(el.parentElement?.getBoundingClientRect().right ?? el.getBoundingClientRect().right)),
      `${route} at 390px: the ledger box itself breaks past the panel that holds it`,
    ).toBeLessThanOrEqual(1);

    expect(await panOffset(page), `${route} at 390px: the ledger's columns move the page instead of scrolling inside the ledger (DESIGN.md §5)`).toBe(0);
  }
});

test('the shift bar sheds its instruments in the stated order', async ({ page }) => {
  await uiLogin(page);

  const version = page.getByTitle(/console build/i);
  const budget = page.getByTitle(/request budget/i);
  const nextWindow = page.getByText('Next window', { exact: true });

  // Promised wherever the bar can hold them, so they arrive a whole breakpoint
  // earlier than the widest instrument (docs: DESIGN.md §8).
  for (const width of [1600, 1440, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(version, `the server version must be on the bar at ${width}px`).toBeVisible();
    await expect(budget, `the request budget must be on the bar at ${width}px`).toBeVisible();
  }

  // The widest instrument is the one that yields: it says the whole window only
  // where the bar can hold the whole window.
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(nextWindow, 'the next-window instrument belongs to 2xl (1536), not 1280').toBeHidden();
  await page.setViewportSize({ width: 1600, height: 900 });
  await expect(nextWindow, 'at 2xl the bar must say the next maintenance window').toBeVisible();

  // Shedding must be total. A half-cut instrument reads as a value that is
  // wrong rather than one that is elsewhere.
  for (const width of [1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(version, `the server version must be shed, not cut, at ${width}px`).toBeHidden();
    await expect(budget, `the request budget must be shed, not cut, at ${width}px`).toBeHidden();
  }

  // What never yields: the search field and the way out, at every width,
  // including the width where an instrument was once allowed to push them off.
  for (const width of [1600, 1280, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByRole('searchbox', { name: 'Search all devices' }), `the search field must survive at ${width}px`).toBeVisible();
    await expect(page.getByRole('button', { name: 'Sign out' }), `the way out must survive at ${width}px`).toBeVisible();
  }
});
