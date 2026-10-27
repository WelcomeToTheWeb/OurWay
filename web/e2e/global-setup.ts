/**
 * 2.2-0 — global setup.
 *
 * Brings the world the suite asserts against into existence, through the
 * product's own APIs:
 *   1. wait for the server,
 *   2. bootstrap the admin account on the first-run registration path,
 *   3. register units, report heartbeats, metrics and updates as an agent does,
 *   4. hold the device WebSockets open with the fake agent (a real WS client),
 *      so presence and remote sessions have a peer for the duration of the run.
 *
 * See `web/e2e/fixtures.ts` for the seeding calls and the review-scaffolding
 * rule that keeps this data out of the product.
 */
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

import { API_URL, ensureAdmin, seedFleet, waitForServer } from './fixtures.js';

const here = dirname(fileURLToPath(import.meta.url));
export const FAKE_AGENT_PID = join(here, '.fake-agent.pid');

export default async function globalSetup(): Promise<void> {
  await waitForServer();

  const token = await ensureAdmin();
  const units = (await seedFleet(token)).units;

  // A page cannot be an agent: its socket dies on navigation. Keep one real
  // device connection per unit for the whole run so presence, session start
  // and frame forwarding have a peer.
  const wsURL = `${API_URL.replace(/^http/, 'ws')}/ws`;
  const child = spawn(
    process.execPath,
    [join(here, 'support', 'fake-agent.mjs'), '--ws', wsURL, '--keys', units.map((u) => u.deviceKey).join(',')],
    { stdio: 'inherit', detached: true },
  );
  writeFileSync(FAKE_AGENT_PID, String(child.pid));
  child.unref();

  // Give the fake agent a moment to register before the first presence check.
  await new Promise((resolve) => setTimeout(resolve, 2_000));
}
