import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { Devices } from './Devices';
import type { Device } from '../types/device';

vi.mock('../hooks/useWebSocket', () => ({ useWebSocket: () => ({ connected: true }) }));
vi.mock('../components/InstallerPanel', () => ({ InstallerPanel: () => null }));
// Admin by default: the tenant facet's Unassigned option is admin-only, so the
// role is part of the fixture, not an afterthought.
vi.mock('../auth/context', () => ({
  useAuth: () => ({ accessToken: 't', hasAnyRole: (roles: string[]) => roles.includes('admin') }),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn() }));
vi.mock('../api/tags', () => ({ bulkTags: vi.fn(), setDeviceTags: vi.fn(), listTags: vi.fn() }));
vi.mock('../api/patching', () => ({
  scanDevices: vi.fn(),
  deployNow: vi.fn(),
  getPatchOverview: vi.fn(() => Promise.resolve(null)),
  listPolicies: vi.fn(() => Promise.resolve([])),
}));
vi.mock('../api/clients', () => ({
  getClients: vi.fn(() => Promise.resolve([])),
  getClientRollups: vi.fn(() => Promise.resolve([])),
}));

import { getDevices } from '../api/devices';
import { bulkTags } from '../api/tags';
import { scanDevices } from '../api/patching';
import { getClients } from '../api/clients';
import { resetFleetPoller } from '../stores/fleet';
import type { Client } from '../api/clients';

const mk = (name: string, o: Partial<Device> = {}): Device => ({
  id: name, name, hostname: `${name}.lan`, os: 'linux', arch: 'amd64', agent_version: '1', status: 'online',
  last_seen: new Date().toISOString(), public_ip: '', private_ip: '', created_at: '', updated_at: '', tags: [], ...o,
});

/** The URL the router is actually showing. `window.location` is the jsdom
 * document, and a MemoryRouter never touches it. */
function UrlProbe() {
  const location = useLocation();
  return <span data-testid="url">{location.search}</span>;
}

const tenant = (id: string, name: string, slug: string): Client => ({
  id, name, slug, notes: '', disabled: false, created_at: '', updated_at: '',
});

describe('Devices page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.setItem('devices.view', 'table');
    resetFleetPoller();
    vi.mocked(getDevices).mockResolvedValue([mk('web-1', { tags: ['prod'] }), mk('web-2'), mk('db-1', { os: 'windows', status: 'offline' })]);
    vi.mocked(bulkTags).mockResolvedValue({ updated: 2 });
    vi.mocked(scanDevices).mockResolvedValue({ scans_sent: 2 });
  });

  it('filters by OS and tag', async () => {
    render(<MemoryRouter><Devices /></MemoryRouter>);
    // The ledger head prints the guide words (first/last unit in view), so a
    // visible unit name appears twice: head + row.
    await screen.findAllByText('db-1');
    fireEvent.change(screen.getByLabelText('Filter by OS'), { target: { value: 'windows' } });
    expect(screen.queryByText('web-1')).toBeNull();
    expect(screen.getAllByText('db-1').length).toBeGreaterThanOrEqual(1);
    fireEvent.click(screen.getByText('Clear filters'));
    fireEvent.change(screen.getByLabelText('Filter by tag'), { target: { value: 'prod' } });
    expect(screen.getAllByText('web-1').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText('web-2')).toBeNull();
  });

  it('bulk-tags and scans only the selected devices', async () => {
    render(<MemoryRouter><Devices /></MemoryRouter>);
    await screen.findAllByText('web-2');
    fireEvent.click(screen.getByLabelText('Select web-1'));
    fireEvent.click(screen.getByLabelText('Select web-2'));
    expect(screen.getByText('2 selected')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Tag to add or remove'), { target: { value: 'Staging' } });
    fireEvent.click(screen.getByText('Add tag'));
    await waitFor(() => expect(bulkTags).toHaveBeenCalledWith(['web-1', 'web-2'], ['staging'], []));

    fireEvent.click(screen.getByText('Scan for updates'));
    await waitFor(() => expect(scanDevices).toHaveBeenCalledWith(['web-1', 'web-2']));
  });

  it('does not act on selected devices hidden by a filter', async () => {
    render(<MemoryRouter><Devices /></MemoryRouter>);
    await screen.findAllByText('web-2');
    fireEvent.click(screen.getByLabelText('Select web-1'));
    fireEvent.change(screen.getByLabelText('Filter by OS'), { target: { value: 'windows' } });
    expect(screen.queryByRole('toolbar', { name: 'Bulk actions' })).toBeNull();
  });
});

describe('Devices page - tenant filter (2.3-5)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    localStorage.setItem('devices.view', 'table');
    resetFleetPoller();
    vi.mocked(getClients).mockResolvedValue([tenant('c-a', 'Alpha Co', 'alpha-co')]);
    vi.mocked(getDevices).mockResolvedValue([
      mk('alpha-1', { client_id: 'c-a', last_seen: '2026-10-01T00:00:00Z' }),
      mk('alpha-2', { client_id: 'c-a', status: 'alert', last_seen: '2026-10-09T00:00:00Z' }),
      mk('loose-1', { client_id: null, last_seen: '2026-10-05T00:00:00Z' }),
    ]);
  });

  it('filters the fleet by tenant from the URL, exactly as the link reads', async () => {
    // The acceptance criterion for 2.3-5 in one line: the tenant filter is URL
    // state, so a link the operator saved is a view they land on.
    render(
      <MemoryRouter initialEntries={['/devices?client=alpha-co&status=alert&sort=last-seen']}>
        <Devices />
      </MemoryRouter>,
    );
    await screen.findAllByText('alpha-2');
    expect(screen.queryByText('alpha-1')).toBeNull();
    expect(screen.queryByText('loose-1')).toBeNull();
  });

  it('names the tenant in the sheet, the card and the profile', async () => {
    render(<MemoryRouter initialEntries={['/devices']}><Devices /></MemoryRouter>);
    // Table column and the ledger head's first/last line both carry the name.
    expect((await screen.findAllByText('Alpha Co')).length).toBeGreaterThanOrEqual(1);
  });

  it('shows Unassigned for units no tenant owns, and filters to them', async () => {
    render(<MemoryRouter initialEntries={['/devices?client=unassigned']}><Devices /></MemoryRouter>);
    await screen.findAllByText('loose-1');
    expect(screen.queryByText('alpha-1')).toBeNull();
  });

  it('keeps the tenant facet out of the way when there is nothing to switch between', async () => {
    // One option is not a choice. A viewer with no grants sees the empty state,
    // not a control with a single entry.
    vi.mocked(getClients).mockResolvedValue([]);
    render(<MemoryRouter initialEntries={['/devices']}><Devices /></MemoryRouter>);
    await screen.findAllByText('alpha-1');
    expect(screen.queryByLabelText('Filter by tenant')).toBeNull();
  });

  it('offers the tenant facet when there are tenants to switch between', async () => {
    vi.mocked(getClients).mockResolvedValue([
      tenant('c-a', 'Alpha Co', 'alpha-co'),
      tenant('c-b', 'Beta Co', 'beta-co'),
    ]);
    render(<MemoryRouter initialEntries={['/devices']}><Devices /></MemoryRouter>);
    const facet = await screen.findByLabelText('Filter by tenant');
    fireEvent.change(facet, { target: { value: 'beta-co' } });
    await waitFor(() => expect(screen.queryByText('alpha-1')).toBeNull());
    // Unassigned is the admin's answer, and the facet offers it to no one else.
    expect(facet).toHaveValue('beta-co');
  });

  it('writes the chosen tenant into the URL', async () => {
    vi.mocked(getClients).mockResolvedValue([
      tenant('c-a', 'Alpha Co', 'alpha-co'),
      tenant('c-b', 'Beta Co', 'beta-co'),
    ]);
    const view = render(
      <MemoryRouter initialEntries={['/devices']}>
        <Devices />
        <UrlProbe />
      </MemoryRouter>,
    );
    fireEvent.change(await screen.findByLabelText('Filter by tenant'), { target: { value: 'alpha-co' } });
    await screen.findAllByText('alpha-1');
    // The view is the URL: the facet writes it, and the URL is what a re-render
    // lands on. That is the whole mechanism a deep link depends on.
    const search = screen.getByTestId('url').textContent ?? '';
    expect(search).toContain('client=alpha-co');
    view.unmount();
    render(<MemoryRouter initialEntries={[`/devices${search}`]}><Devices /></MemoryRouter>);
    await screen.findAllByText('alpha-1');
  });
});

describe('Devices page - virtualized cards (2.3-5)', () => {
  const many = Array.from({ length: 40 }, (_, i) =>
    mk(`unit-${i}`, { client_id: i % 2 ? 'c-a' : null }),
  );

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    localStorage.setItem('devices.view', 'cards');
    // The cards grid is a fallback: above this many units the ledger virtualizes.
    localStorage.setItem('devices.cards-threshold', '10');
    resetFleetPoller();
    vi.mocked(getClients).mockResolvedValue([tenant('c-a', 'Alpha Co', 'alpha-co')]);
    vi.mocked(getDevices).mockResolvedValue(many);
  });

  it('renders a 40-unit fleet as a grid window, not 40 mounted cards', async () => {
    // Where 2.3-8's budget is proven. jsdom reports a 0x0 viewport, so a
    // windowed grid mounts only its overscan: the fleet is in the window's
    // model, and never all of it in the DOM. A 40-card mount here means the
    // grid stopped windowing, which is what a 10k fleet punishes.
    render(<MemoryRouter initialEntries={['/devices']}><Devices /></MemoryRouter>);
    // jsdom gives every element a 0x0 box. `.panel` is a Tailwind
    // @apply-ed class with no CSS behind it in a unit test, so the grid's
    // height is the only box react-window can read - which is why the page
    // gives it an explicit one. The window mounts its overscan, not the fleet.
    await waitFor(() => expect(document.querySelector('[role="grid"]')).not.toBeNull());
    const mounted = document.querySelectorAll('[data-device-card]').length;
    expect(mounted).toBeLessThan(many.length);
  });
});
