import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Layout } from './Layout';
import { rememberScope, resetScopeHeader } from '../api/client';
import { resetFleetPoller, useFleetStore } from '../stores/fleet';
import type { Client } from '../api/clients';

/**
 * 2.3-5: the shift bar states the tenant scope the server applied.
 *
 * The scope is decided server-side on every read (2.3-1) and the console has no
 * independent way to know it — which is exactly why the support ticket this
 * phase is measured by ("the technician cannot find the unit") is so easy to
 * misdiagnose. The bar is mounted on every page, so this is the one place the
 * answer is always in view, and it is the server's statement, not the
 * console's guess: it is captured from `X-OurWay-Client-Scope` on the reads.
 */

let roles: string[] = ['admin'];
const clients: Client[] = [
  { id: 'c-a', name: 'Alpha Co', slug: 'alpha-co', notes: '', disabled: false, created_at: '', updated_at: '' },
  { id: 'c-b', name: 'Beta Co', slug: 'beta-co', notes: '', disabled: false, created_at: '', updated_at: '' },
];

vi.mock('../auth/context', () => ({
  useAuth: () => ({
    user: { id: 'u', username: 'ops', roles },
    roles,
    accessToken: 't',
    logout: vi.fn(),
    hasAnyRole: (r: string[]) => r.some((x) => roles.includes(x)),
    hasRole: (r: string) => roles.includes(r),
  }),
}));
vi.mock('../hooks/useWebSocket', () => ({ useWebSocket: () => ({ connected: true }) }));
vi.mock('../api/system', () => ({
  getServerVersion: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('../api/patching', () => ({
  listPolicies: vi.fn(() => Promise.resolve([])),
  getPatchOverview: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('../api/clients', () => ({
  getClients: vi.fn(async () => clients),
  getClientRollups: vi.fn(() => Promise.resolve([])),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn(() => Promise.resolve([])) }));

/**
 * Deliver a scope header the way a read delivers one, and let React commit the
 * resulting re-render before the assertion runs: the header arrives from the
 * network, not from a click, so it is not inside React's own batch.
 */
function applyScope(value: string) {
  act(() => rememberScope({ 'x-ourway-client-scope': value }));
}

function renderBar() {
  return render(
    <MemoryRouter>
      <Layout />
    </MemoryRouter>,
  );
}

describe('Layout shift bar - tenant scope (2.3-5)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    roles = ['admin'];
    resetFleetPoller();
    resetScopeHeader();
    useFleetStore.getState().setCollection('clients', clients);
    useFleetStore.getState().setCollection('patchPolicies', []);
  });

  it('names the tenants the server said this read covers', () => {
    renderBar();
    // The header is a server answer, so the test hands the console one the way
    // a read does: through the capture point the interceptor uses.
    applyScope('c-a,c-b');
    expect(screen.getByText('Alpha Co, Beta Co')).toBeInTheDocument();
  });

  it('says "all tenants" to an admin, in words that do not read like a filter', () => {
    renderBar();
    applyScope('all');
    expect(screen.getByText('all tenants')).toBeInTheDocument();
  });

  it('says "no tenants" to an account with no grants, which is not the same as an empty fleet', () => {
    renderBar();
    applyScope('none');
    const stamp = screen.getByText('no tenants');
    expect(stamp).toBeInTheDocument();
    // The title is the sentence the operator reads when a unit is missing.
    expect(stamp.getAttribute('title')).toContain('A unit you cannot find is usually a tenant you are not granted');
  });

  it('stays off the bar before the server has said anything, so the bar never states a scope it does not know', () => {
    renderBar();
    expect(screen.queryByText(/tenants$/)).toBeNull();
  });

  it('renders no raw locale key path on the bar', () => {
    renderBar();
    applyScope('c-a');
    expect(document.body.textContent).not.toMatch(/\b(layout|clients|devices|common)\.[a-zA-Z.]+\b/);
  });
});
