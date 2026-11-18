import { defineConfig } from '@playwright/test';

/**
 * 2.2-0 — end-to-end harness.
 *
 * The suite runs against the **production bundle** (`vite preview`), not a dev
 * server, and against a **real Go server + PostgreSQL**: every flow below is a
 * flow the console must survive in a real deployment, so nothing here mocks the
 * product. `web/e2e/global-setup.ts` bootstraps the install through the real
 * API (first registration → admin) and seeds units, metrics and updates
 * through the **agent-facing** API, so the data the console renders arrived
 * the way real data arrives.
 *
 * Base URLs (all overridable for CI):
 *   OURWAY_E2E_BASE_URL  console       default http://localhost:3000
 *   OURWAY_E2E_API_URL   REST root     default http://localhost:9090
 */
export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  outputDir: '../test-results/e2e',
  timeout: 30 * 1000,
  expect: { timeout: 5 * 1000 },
  // One worker: the request budget is per user, and the suite shares one admin
  // account. The budget is now a dial (RATE_LIMIT_PER_MINUTE, set generously in
  // the e2e job), so this is about determinism and shared state, not starvation.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL: process.env.OURWAY_E2E_BASE_URL ?? 'http://localhost:3000',
    // Pinned, not inherited. The suite has always run at Playwright's default
    // 1280×720, and that number is load-bearing: the shift bar carries the
    // server version and the request budget from `xl` (1280) up, so one pixel
    // narrower and `auth.spec.ts` asserts controls that the console correctly
    // does not show. A default that a library may change under us is not a
    // contract; `web/e2e/responsive.spec.ts` asserts the shed order at the
    // widths DESIGN.md §8 names.
    viewport: { width: 1280, height: 720 },
    // Pinned for the same reason. The console detects a language from the
    // browser and caches it in `localStorage`, and the assertions in
    // `responsive.spec.ts` and `auth.spec.ts` are on English copy and English
    // `title` text. Today every run is English only because Playwright hands
    // each test a fresh context; the day someone adds a `storageState` to skip
    // the UI login, the language starts travelling with it and the assertions
    // quietly stop matching the controls they mean.
    locale: 'en',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
});
