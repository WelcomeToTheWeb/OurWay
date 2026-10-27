/**
 * 2.2-0 — global teardown: stop the fake agent the run started.
 *
 * Left running, it would hold device connections open against a shared
 * database forever, and the next run would inherit its units in a state it
 * did not create.
 */
import { existsSync, readFileSync, unlinkSync } from 'node:fs';

import { FAKE_AGENT_PID } from './global-setup.js';

export default async function globalTeardown(): Promise<void> {
  if (!existsSync(FAKE_AGENT_PID)) return;
  const pid = Number(readFileSync(FAKE_AGENT_PID, 'utf8'));
  try {
    if (pid > 0) process.kill(-pid, 'SIGTERM'); // detached spawn → process group
  } catch {
    try {
      if (pid > 0) process.kill(pid, 'SIGTERM');
    } catch {
      // already gone
    }
  }
  unlinkSync(FAKE_AGENT_PID);
}
