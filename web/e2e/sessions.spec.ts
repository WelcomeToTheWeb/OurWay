/**
 * 2.2-0 — presence, a remote session, and the sessions ledger.
 *
 * The seeded units hold real device WebSockets for the duration of the run
 * (`e2e/support/fake-agent.mjs`, started by global setup), so presence and
 * session start have a peer the way an installed fleet provides one.
 *
 * The handshake check below runs from a **browser** page, which agents never
 * exercise (they send no Origin): it is the path the C5 origin gate and the
 * `ourway-auth` subprotocol negotiation live on, and it is where every
 * localhost console was silently getting 403.
 */
import { test, expect } from '@playwright/test';
import { ADMIN_PASSWORD, ADMIN_USER, apiBase, apiLogin, manifest, reFor, uiLogin } from './fixtures.js';

test('a browser may open the device socket the agent protocol uses', async ({ page }) => {
  const probe = manifest().units[1];
  await uiLogin(page);

  const opened = await page.evaluate(
    ({ url, deviceKey }) =>
      new Promise<boolean>((resolve) => {
        const ws = new WebSocket(url, ['ourway-auth', deviceKey]);
        ws.onopen = () => {
          // Open then close, as fast as possible. This *does* displace the
          // fake agent's connection for that key — the hub allows one client
          // per device key, so the later arrival wins — which is why the
          // support agent treats a clean close as a displacement and
          // re-registers in milliseconds, and why the session tests wait for
          // the unit to be online before they act on it.
          ws.close(1000, 'handshake probe');
          resolve(true);
        };
        ws.onerror = () => resolve(false);
        window.setTimeout(() => resolve(ws.readyState === WebSocket.OPEN), 5_000);
      }),
    { url: `ws://${new URL(page.url()).host}/ws`, deviceKey: probe.deviceKey },
  );

  expect(opened, 'the console origin was refused by the WS origin gate').toBe(true);
});

/**
 * A session needs a peer. The harness's fake agent is displaced whenever a test
 * opens the same device socket, and it re-registers in milliseconds — clicking
 * before the unit is back online starts nothing, and the ledger is correctly
 * empty. So the suite waits for the state its assertion depends on.
 */
async function expectUnitOnline(deviceId: string) {
  const token = await apiLogin(ADMIN_USER, ADMIN_PASSWORD);
  await expect
    .poll(
      async () => {
        const res = await fetch(`${apiBase()}/api/devices/${deviceId}`, {
          headers: { authorization: `Bearer ${token}` },
        });
        const body = await res.json();
        return (body.device as { status?: string } | undefined)?.status;
      },
      { message: 'the unit must be connected before it can be remote-controlled', timeout: 30_000 }
    )
    .toBe('online');
}

test('a connected unit can start a session, and the ledger shows it', async ({ page }) => {
  const unit = manifest().units[0];
  await uiLogin(page);

  await page.goto(`/devices/${unit.id}`);
  await expectUnitOnline(unit.id);
  await page.getByRole('button', { name: /remote session/i }).click();

  await page.goto('/sessions');
  // The ledger's Session column carries the session id, its Unit column the
  // unit id prefix (web/src/pages/Sessions.tsx).
  const row = page.getByRole('row').filter({ hasText: reFor(unit.id.slice(0, 8)) }).first();
  await expect(row, 'the started session should appear in the ledger').toBeVisible({ timeout: 10_000 });
  // Pending until the agent reports a frame, per server/api/sessions.go.
  await expect(row).toContainText(/pending|active/);

  // The session survives a reload: it is server state, not page state.
  await page.reload();
  await expect(row).toBeVisible();

  // Leaving the fleet tidy for the next run. Ending a session is gated by a
  // native confirm() dialog (web/src/pages/Sessions.tsx:33 — DESIGN.md bans
  // browser dialogs for destructive actions; see buglist), and Playwright
  // dismisses dialogs by default, so the harness has to accept it explicitly.
  page.on('dialog', (dialog) => dialog.accept());
  await row.getByRole('button', { name: /^end$/i }).click();
  await page.reload();
  await expect(page.getByRole('row').filter({ hasText: reFor(unit.id.slice(0, 8)) })).toHaveCount(0, {
    timeout: 10_000,
  });
});
