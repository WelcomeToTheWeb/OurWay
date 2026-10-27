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
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
});
