/**
 * 2.2-0 — e2e fixtures.
 *
 * Everything here seeds the console through **the API the product itself
 * exposes**, not by writing to the database:
 *
 *   - units arrive by `POST /api/agent/register` (the agent's own enrollment call)
 *   - metrics arrive by `POST /api/agent/metrics` (which runs the alert engine,
 *     so alerts exist because the engine made them, not because a fixture wrote rows)
 *   - updates arrive by `POST /api/agent/updates` (the agent's patch reporter)
 *   - the admin account is the **first registration** of the install, which is
 *     the product's own bootstrap path (`server/api/auth.go`: the first account
 *     gets admin, everyone after it gets viewer)
 *
 * Review-scaffolding rule (`PRODUCT.md`): this data is test scaffolding. It is
 * never shipped in the bundle, never presented as product data, and the
 * generated manifest is gitignored.
 */
import { writeFileSync, readFileSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { APIRequestContext } from '@playwright/test';

export const API_URL = process.env.OURWAY_E2E_API_URL ?? 'http://localhost:9090';
/** The API origin the harness seeds through and asserts against. */
export const apiBase = (): string => API_URL;
export const VIEWER_USER = 'e2e-viewer';
export const VIEWER_PASSWORD = 'E2eViewerPass!23';
export const ADMIN_USER = process.env.OURWAY_E2E_ADMIN_USER ?? 'e2e-admin';
export const ADMIN_PASSWORD = process.env.OURWAY_E2E_ADMIN_PASSWORD ?? 'E2e-Admin-Password-2026!';
export const ADMIN_EMAIL = process.env.OURWAY_E2E_ADMIN_EMAIL ?? 'e2e-admin@example.invalid';

/** Units the suite seeds. Fixed names keep assertions stable and greppable. */
export const SEED_UNITS = [
  {
    name: 'E2E-WORKSTATION-01',
    hostname: 'e2e-ws-01',
    os: 'windows',
    arch: 'amd64',
    privateIP: '10.90.0.11',
    // cpu 95 crosses the critical threshold (90) in server/alerts/engine.go,
    // so the alert ledger has a real critical row to acknowledge.
    metrics: { cpu: 95, ram: 40, ram_used: 4_000_000_000, ram_total: 16_000_000_000, disk_usage: 45, disk_used: 45_000_000_000, disk_total: 100_000_000_000, net_in: 1_000, net_out: 1_000, uptime: 86_400, processes: 180 },
    critical: true,
    updates: [
      { source: 'windows', title: 'E2E Security Update 001', version: '1.0.1', severity: 'critical', kb: 'KB5000001' },
      { source: 'windows', title: 'E2E Optional Update 002', version: '1.0.2', severity: 'optional', kb: 'KB5000002' },
    ],
  },
  {
    name: 'E2E-SERVER-01',
    hostname: 'e2e-srv-01',
    os: 'linux',
    arch: 'amd64',
    privateIP: '10.90.0.12',
    // disk_usage 97 crosses the critical disk threshold (95).
    metrics: { cpu: 12, ram: 96, ram_used: 15_000_000_000, ram_total: 16_000_000_000, disk_usage: 97, disk_used: 97_000_000_000, disk_total: 100_000_000_000, net_in: 2_000, net_out: 2_000, uptime: 1_728_000, processes: 240 },
    critical: true,
    updates: [{ source: 'apt', title: 'E2E kernel patch', version: '6.9.9', severity: 'important', kb: '' }],
  },
  {
    name: 'E2E-MAC-01',
    hostname: 'e2e-mac-01',
    os: 'darwin',
    arch: 'arm64',
    privateIP: '10.90.0.13',
    // Healthy unit: gives the fleet view a unit that needs nothing.
    metrics: { cpu: 8, ram: 22, ram_used: 3_000_000_000, ram_total: 16_000_000_000, disk_usage: 30, disk_used: 30_000_000_000, disk_total: 100_000_000_000, net_in: 500, net_out: 500, uptime: 3_600, processes: 90 },
    critical: false,
    updates: [],
  },
] as const;

export interface SeededUnit {
  name: string;
  hostname: string;
  os: string;
  id: string;
  deviceKey: string;
}

export interface SeedManifest {
  adminUser: string;
  adminPassword: string;
  units: SeededUnit[];
  seededAt: string;
}

// `__dirname` does not exist in ESM (web/package.json is "type": "module").
const MANIFEST_PATH = join(dirname(fileURLToPath(import.meta.url)), '.seed-manifest.json');

/**
 * Run-scoped suffix: re-registering a known (hostname, private_ip) requires the
 * device key or the enrollment secret (the C4 remediation), so a second run
 * against the same database cannot re-register yesterday's units. A fresh
 * database (CI) always takes the clean names; a shared database (local dev)
 * gets a verifiable fleet it owns. Both paths are exercised.
 */
export const RUN_SUFFIX = process.env.OURWAY_E2E_RUN_ID ? `-R${process.env.OURWAY_E2E_RUN_ID}` : '';

/** The exact title the seeder reported for a fixture update. */
export function seededTitle(unitIndex: number, updateIndex: number): string {
  return `${SEED_UNITS[unitIndex].updates[updateIndex].title}${RUN_SUFFIX}`;
}

async function req<T>(path: string, init: RequestInit & { token?: string } = {}): Promise<{ status: number; body: T }> {
  const headers: Record<string, string> = { 'content-type': 'application/json', ...((init.headers as Record<string, string>) ?? {}) };
  if (init.token) headers.authorization = `Bearer ${init.token}`;
  const { token: _drop, ...rest } = init;
  const res = await fetch(`${API_URL}/api${path}`, { ...rest, headers });
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  return { status: res.status, body: body as T };
}

/** Wait for the server to answer on /health before touching anything. */
export async function waitForServer(timeoutMs = 90_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError = 'unknown';
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${API_URL}/health`);
      if (res.ok) return;
      lastError = `HTTP ${res.status}`;
    } catch (e) {
      lastError = (e as Error).message;
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`e2e setup: server never became healthy at ${API_URL}/health (${lastError})`);
}

/**
 * Bootstrap the admin account through the product's own first-run path.
 * Idempotent: an existing install (local dev) already has users, and
 * registration is closed after the first account (C1), so log in first.
 */
/** Escape a literal for use in a RegExp: unit names and titles are data. */
export function reFor(literal: string): RegExp {
  return new RegExp(literal.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
}

/** Log in through the API and return the access token (for API assertions). */export async function apiLogin(username: string, password: string): Promise<string> {
  const res = await req<{ access_token: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
  if (res.status !== 200 || !res.body?.access_token) {
    throw new Error(`e2e setup: API login failed for ${username} (${res.status})`);
  }
  return res.body.access_token;
}

export async function ensureAdmin(): Promise<string> {
  const login = await req<{ access_token: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username: ADMIN_USER, password: ADMIN_PASSWORD }),
  });
  if (login.status === 200 && login.body?.access_token) return login.body.access_token;

  const reg = await req<{ access_token?: string }>('/auth/register', {
    method: 'POST',
    body: JSON.stringify({ username: ADMIN_USER, email: ADMIN_EMAIL, password: ADMIN_PASSWORD }),
  });
  if (reg.status !== 200 && reg.status !== 201) {
    throw new Error(`e2e setup: could not bootstrap the admin account (${reg.status}) ${JSON.stringify(reg.body)}`);
  }

  const relog = await req<{ access_token: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username: ADMIN_USER, password: ADMIN_PASSWORD }),
  });
  if (relog.status !== 200) {
    throw new Error(`e2e setup: login after registration failed (${relog.status}) ${JSON.stringify(relog.body)}`);
  }
  return relog.body.access_token;
}

/**
 * Seed units, metrics and updates the way an agent fleet does.
 *
 * Registration is idempotent per (hostname, private_ip), and re-registering a
 * known device requires its device key (the C4 guard). So a run-scoped fleet
 * must look like *different machines*: with OURWAY_E2E_RUN_ID set, the host name
 * and private address move with the run name, and every unit enrolls cleanly.
 */
export async function seedFleet(token: string): Promise<SeedManifest> {
  const runSuffix = RUN_SUFFIX;
  const runTag = process.env.OURWAY_E2E_RUN_ID ?? '';
  const units: SeededUnit[] = [];

  for (const [index, unit] of SEED_UNITS.entries()) {
    const name = runSuffix ? `${unit.name}${runSuffix}` : unit.name;
    const hostname = runTag ? `${unit.hostname}-r${runTag}` : unit.hostname;
    const privateIP = runTag ? `10.61.${(Number(runTag) % 251) + 1}.${index + 1}` : unit.privateIP;
    const reg = await req<{ device: { id: string }; device_key: string }>('/agent/register', {
      method: 'POST',
      body: JSON.stringify({
        name,
        hostname,
        os: unit.os,
        arch: unit.arch,
        agent_version: '2.1.0',
        public_ip: '203.0.113.10',
        private_ip: privateIP,
      }),
    });
    if (reg.status === 401 && !runSuffix) {
      throw new Error(
        `e2e setup: ${name} is already enrolled on this database and re-registration needs its device key (the C4 guard). ` +
          'Set OURWAY_E2E_RUN_ID to seed a run-scoped fleet, or point OURWAY_E2E_API_URL at a fresh database.',
      );
    }
    if (reg.status !== 200 && reg.status !== 201) {
      throw new Error(`e2e setup: registering ${name} failed (${reg.status}) ${JSON.stringify(reg.body)}`);
    }
    const seeded: SeededUnit = {
      name,
      hostname,
      os: unit.os,
      id: reg.body.device.id,
      deviceKey: reg.body.device_key,
    };
    units.push(seeded);

    // Heartbeat marks the unit online (store.UpdateLastSeen), so the fleet
    // view shows it as connected.
    await req('/agent/heartbeat', { method: 'POST', headers: { 'x-device-key': seeded.deviceKey }, body: '{}' });

    // Metrics run the alert engine, so the critical units produce real alerts.
    const metrics = await req('/agent/metrics', {
      method: 'POST',
      headers: { 'x-device-key': seeded.deviceKey },
      body: JSON.stringify(unit.metrics),
    });
    if (metrics.status !== 200) {
      throw new Error(`e2e setup: metrics for ${unit.name} failed (${metrics.status}) ${JSON.stringify(metrics.body)}`);
    }

    for (const update of unit.updates) {
      await req('/agent/updates', {
        method: 'POST',
        headers: { 'x-device-key': seeded.deviceKey },
        body: JSON.stringify({
          device_id: seeded.id,
          source: update.source,
          title: `${update.title}${runSuffix}`,
          version: update.version,
          severity: update.severity,
          kb: update.kb,
          category: 'e2e',
          size_bytes: 1_048_576,
        }),
      });
    }
  }

  const manifest: SeedManifest = { adminUser: ADMIN_USER, adminPassword: ADMIN_PASSWORD, units, seededAt: new Date().toISOString() };
  writeFileSync(MANIFEST_PATH, JSON.stringify(manifest, null, 2));

  // The alert engine runs on the metrics request, and the console reads the
  // ledger over REST: confirm the critical rows really exist before specs run.
  const alerts = await req<{ alerts: { device_id: string; severity: string }[] }>('/alerts', { token });
  if (alerts.status !== 200) {
    throw new Error(`e2e setup: alert ledger unreadable (${alerts.status})`);
  }
  const criticalUnits = SEED_UNITS.map((u, i) => (u.critical ? units[i].id : null)).filter(
    (id): id is string => id !== null,
  );
  const withCritical = alerts.body.alerts.filter((a) => a.severity === 'critical').map((a) => a.device_id);
  if (!criticalUnits.every((id) => withCritical.includes(id))) {
    throw new Error('e2e setup: seeded units did not produce the expected critical alerts — the engine contract changed');
  }

  return manifest;
}

export function readManifest(): SeedManifest {
  if (!existsSync(MANIFEST_PATH)) {
    throw new Error(`e2e: missing ${MANIFEST_PATH} — global setup did not run`);
  }
  return JSON.parse(readFileSync(MANIFEST_PATH, 'utf8')) as SeedManifest;
}

/**
 * Lazily-loaded manifest for specs.
 *
 * Specs must not read the manifest at module top level: Playwright loads every
 * spec file during test *collection*, before global setup has seeded anything,
 * so a top-level read makes `--list` and `--grep` fail with a setup error that
 * has nothing to do with the test being run.
 */
let manifestCache: SeedManifest | null = null;
export function manifest(): SeedManifest {
  if (!manifestCache) manifestCache = readManifest();
  return manifestCache;
}

/** Console login through the UI (not by stuffing tokens into localStorage). */
export async function uiLogin(page: import('@playwright/test').Page, user = ADMIN_USER, password = ADMIN_PASSWORD): Promise<void> {
  await page.goto('/login');
  await page.getByLabel('Username').fill(user);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: /sign in/i }).click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 15_000 });
}

const VIEWER = { username: VIEWER_USER, password: VIEWER_PASSWORD, email: 'e2e-viewer@example.invalid' };

/**
 * Ensure the viewer account the role-gate test uses exists.
 *
 * Idempotent by inspection rather than by status code: `POST /api/users`
 * answers 500 for a duplicate e-mail (only Register/UpdateProfile received the
 * 409 treatment in L5), so the seeder asks who is already there before creating
 * anyone. That 500 is recorded in buglist.md.
 */
export async function ensureViewer(adminToken: string, request: APIRequestContext): Promise<void> {
  const auth = { Authorization: `Bearer ${adminToken}` };
  const list = await request.get(`${API_URL}/api/users`, { headers: auth });
  if (list.ok()) {
    const body = await list.json().catch(() => null);
    const users: Array<{ username?: string }> = Array.isArray(body) ? body : (body?.users ?? []);
    if (users.some((u) => u.username === VIEWER.username)) return;
  }
  const res = await request.post(`${API_URL}/api/users`, {
    headers: auth,
    data: { username: VIEWER.username, email: VIEWER.email, password: VIEWER.password, roles: ['viewer'] },
  });
  if (!res.ok()) {
    throw new Error(
      `e2e setup: creating the viewer account failed (${res.status}). ` +
        'The suite needs a viewer account to prove the role gates hold.',
    );
  }
}
