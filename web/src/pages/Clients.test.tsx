import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { useEffect } from 'react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';

import { Clients } from './Clients';
import { AuthProvider } from '../auth/context';
import type { Client, ClientRollup, ClientUser } from '../api/clients';
import type { Device } from '../types/device';
import type { UserWithRoles } from '../auth/types';

vi.mock('../api/clients', () => ({
  getClients: vi.fn(),
  getClientRollup: vi.fn(),
  getClientRollups: vi.fn(),
  createClient: vi.fn(),
  updateClient: vi.fn(),
  deleteClient: vi.fn(),
  assignClientDevices: vi.fn(),
  getClientUsers: vi.fn(),
  setClientUsers: vi.fn(),
  clientFromSearch: vi.fn((search: string) => new URLSearchParams(search).get('client') ?? ''),
  withClientParam: vi.fn((search: string, slug: string) => {
    const params = new URLSearchParams(search);
    if (slug === '') params.delete('client');
    else params.set('client', slug);
    const out = params.toString();
    return out ? `?${out}` : '';
  }),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn() }));
vi.mock('../api/users', () => ({ getUsers: vi.fn() }));

import {
  getClients,
  getClientRollup,
  getClientRollups,
  deleteClient,
  assignClientDevices,
  getClientUsers,
  setClientUsers,
  updateClient,
} from '../api/clients';
import { getDevices } from '../api/devices';
import { resetFleetPoller } from '../stores/fleet';
import { getUsers } from '../api/users';

const mockedGetClients = vi.mocked(getClients);
const mockedRollup = vi.mocked(getClientRollup);
const mockedRollups = vi.mocked(getClientRollups);
const mockedDelete = vi.mocked(deleteClient);
const mockedAssign = vi.mocked(assignClientDevices);
const mockedClientUsers = vi.mocked(getClientUsers);
const mockedSetUsers = vi.mocked(setClientUsers);
const mockedUpdate = vi.mocked(updateClient);
const mockedDevices = vi.mocked(getDevices);
const mockedUsers = vi.mocked(getUsers);

function client(id: string, name: string, slug: string, disabled = false): Client {
  return {
    id,
    name,
    slug,
    notes: '',
    disabled,
    created_at: '2026-10-27T10:00:00Z',
    updated_at: '2026-10-27T10:00:00Z',
  };
}

function rollup(c: Client, over: Partial<ClientRollup> = {}): ClientRollup {
  return {
    client_id: c.id,
    name: c.name,
    slug: c.slug,
    disabled: c.disabled,
    units: 3,
    units_online: 2,
    last_seen: '2026-10-27T10:29:40Z',
    alerts_open: 2,
    alerts_critical: 1,
    alerts_warning: 1,
    alerts_info: 0,
    patch_units: 3,
    patch_compliant: 1,
    patch_pending: 2,
    patch_critical: 1,
    patch_reboot_pending: 1,
    technicians: 2,
    ...over,
  };
}

const alpha = client('c-alpha', 'Alpha Co', 'alpha-co');
const beta = client('c-beta', 'Beta Co', 'beta-co');

// The fixture states each unit's tenant, because that is the fact the panel's
// candidate list is built from: a unit assigned to another client is not a
// unit this caller may move.
const devices: Device[] = [
  { id: 'd-a', name: 'unit-alpha', hostname: 'alpha', os: 'linux', arch: 'amd64', status: 'online', client_id: 'c-alpha' },
  { id: 'd-b', name: 'unit-beta', hostname: 'beta', os: 'linux', arch: 'amd64', status: 'online', client_id: 'c-beta' },
  { id: 'd-free', name: 'unit-free', hostname: 'free', os: 'linux', arch: 'amd64', status: 'online', client_id: null },
] as unknown as Device[];

const users: UserWithRoles[] = [
  { id: 'u-admin', username: 'boss', email: 'b@example.test', created_at: '', roles: ['admin'] },
  { id: 'u-tech', username: 'tyler', email: 't@example.test', created_at: '', roles: ['technician'] },
] as unknown as UserWithRoles[];

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={['/clients']}>
        <Routes>
          <Route path="/clients" element={<Clients />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}

// The ledger row and the filter chip carry the same name, so a test asks for
// the row explicitly: `tr` is the ledger's unit of meaning, and the chip is a
// button in the filter row above it.
function rowFor(name: string): HTMLElement {
  const rows = screen.getAllByText(name).map((el) => el.closest('tr')).filter(Boolean) as HTMLElement[];
  if (rows.length === 0) throw new Error(`no ledger row for ${name}`);
  return rows[0];
}

// The row's accessible name states the act ("Open Alpha Co detail"), so a test
// can ask for the row, the chip, and the delete button without disambiguation
// gymnastics — which is also what a screen reader hears.
async function openRow(name: string): Promise<HTMLElement> {
  await screen.findAllByText(name);
  const button = await screen.findByRole('button', { name: new RegExp(`open ${name} detail`, 'i') });
  const row = button.closest('tr');
  if (!row) throw new Error(`the ${name} row rendered outside a table row`);
  return row;
}

function LocationSpy({ seen }: { seen: string[] }) {
  const location = useLocation();
  useEffect(() => {
    seen.push(location.search);
  }, [location.search, seen]);
  return null;
}

function setRoles(roles: string[]) {
  localStorage.setItem('ourway_roles', JSON.stringify(roles));
}

describe('Clients ledger (2.3-4)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    // The ledger reads the shared fleet collections (2.3-5), and the store is
    // module state: a test starts with an empty one, so the page's own mount is
    // the read under test and not the previous test's cache.
    resetFleetPoller();
    setRoles(['admin']);
    mockedGetClients.mockResolvedValue([alpha, beta]);
    // 2.3-5: the ledger reads every tenant's figures in one call, the same
    // collection the dashboard's per-client rows subscribe to.
    mockedRollup.mockImplementation(async (id: string) => {
      const c = id === alpha.id ? alpha : beta;
      return rollup(c);
    });
    mockedRollups.mockResolvedValue([rollup(alpha), rollup(beta)]);
    mockedDevices.mockResolvedValue(devices);
    mockedUsers.mockResolvedValue(users);
    mockedClientUsers.mockResolvedValue([] as ClientUser[]);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders one row per client with the rollup numbers, not key paths', async () => {
    renderPage();

    await openRow('Alpha Co');
    // Row and filter chip both carry the tenant's name.
    expect((await screen.findAllByText('Beta Co')).length).toBeGreaterThanOrEqual(2);

    // Every rollup number is on screen: units, awake, alerts, compliance, access.
    const row = rowFor('Alpha Co');
    expect(row.textContent).toContain('3');
    expect(row.textContent).toContain('1/3');

    // No raw i18n key path renders anywhere on the page. A missing key shows
    // its own path on screen — this project's known failure mode, and the reason
    // the assertion is not scoped to one namespace: the panel's create button
    // shipped as `common.create` for exactly one commit, and it renders as
    // `common.create` to every operator who cannot see why.
    expect(document.body.textContent).not.toMatch(/\b[a-z][a-zA-Z]*\.[a-zA-Z][a-zA-Z]*\b/);
  });

  it('shows the empty state, not a blank ledger, when the server answers no clients', async () => {
    // A technician with no grants: the server's answer is an empty fleet list,
    // and the console must say so rather than render nothing.
    mockedGetClients.mockResolvedValue([]);
    mockedRollups.mockResolvedValue([]);
    renderPage();

    const empty = await screen.findByText(/No clients yet/i);
    expect(empty).toBeInTheDocument();
  });

  it('offers no destructive control to a manager, whose writes the server refuses', async () => {
    setRoles(['manager']);
    renderPage();

    await openRow('Alpha Co');
    expect(screen.queryByRole('button', { name: /^delete$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /new client/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /pause/i })).not.toBeInTheDocument();
  });

  it('deletes through ConfirmSheet, naming the units that move, and never a browser dialog', async () => {
    const spy = vi.spyOn(window, 'confirm').mockImplementation(() => true);
    mockedDelete.mockResolvedValue({ status: 'ok', devices_unassigned: 3 });
    renderPage();

    const row = await openRow('Alpha Co');
    fireEvent.click(within(row).getByRole('button', { name: /^delete$/i }));

    // The sheet states the consequence the server enforces: units survive,
    // unassigned.
    expect(
      await screen.findByText(/unassigned/i)
    ).toBeInTheDocument();
    expect(spy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /delete client/i }));
    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith('c-alpha'));
    spy.mockRestore();
  });

  it('assigns units and grants, and the grant save states what revoking does', async () => {
    mockedAssign.mockResolvedValue({ status: 'ok', assigned: 2 });
    mockedSetUsers.mockResolvedValue({ status: 'ok', users: 1 });

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /open Alpha Co detail/i }));

    // The detail panel offers the client's units plus the unassigned pool, and
    // never another client's unit.
    // The panel lists the client's units and the unassigned pool, and never
    // another client's unit — the same scoping the server enforces.
    const panel = await screen.findByLabelText(`${alpha.name} detail`);
    expect(within(panel).getByText('unit-alpha')).toBeInTheDocument();
    expect(within(panel).getByText('unit-free')).toBeInTheDocument();
    expect(within(panel).queryByText('unit-beta')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /save units/i }));
    await waitFor(() => expect(mockedAssign).toHaveBeenCalled());
    const sent = mockedAssign.mock.calls[0];
    expect(sent[0]).toBe('c-alpha');
    expect(sent[1]).toContain('d-a');

    // Grants are the accounts that are not scope-all by role.
    fireEvent.click(screen.getByRole('button', { name: /save access/i }));
    await waitFor(() => expect(mockedSetUsers).toHaveBeenCalledWith('c-alpha', []));

    // The hint states the live-token behaviour, which is what an MSP operator
    // needs to know before revoking a technician.
    expect(screen.getByText(/next request/i)).toBeInTheDocument();
  });

  it('pauses a client without deleting its fleet', async () => {
    mockedUpdate.mockResolvedValue({ ...alpha, disabled: true });
    renderPage();

    await openRow('Alpha Co');
    fireEvent.click(within(rowFor('Alpha Co')).getByRole('button', { name: /pause/i }));
    await waitFor(() => expect(mockedUpdate).toHaveBeenCalled());
    const call = mockedUpdate.mock.calls[0];
    expect(call[0]).toBe('c-alpha');
    expect(call[1]).toMatchObject({ disabled: true });
  });

  it('filters by client slug in the URL, so the filtered view is linkable', async () => {
    const seen: string[] = [];
    render(
      <AuthProvider>
        <MemoryRouter initialEntries={['/clients']}>
          <Routes>
            <Route
              path="/clients"
              element={
                <>
                  <Clients />
                  <LocationSpy seen={seen} />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    );
    await openRow('Alpha Co');

    // The chip, not the row: the filter is the one that rewrites the URL, and
    // the URL is the thing a link and a reload share (2.1.0's view-in-URL).
    fireEvent.click(screen.getByRole('button', { name: 'Beta Co' }));
    await waitFor(() => expect(seen.join('|')).toContain('client=beta-co'));
  });
});
