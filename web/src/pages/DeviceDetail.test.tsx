import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

// The unit view is where an operator learns what a remote session can do on a
// unit (2.2-1). Two failure modes are cheap to ship and expensive to notice:
// rendering "not reported" as if it were granted, and rendering a locale key
// path because the key was assembled from a wire value.
vi.mock('../api/devices', () => ({
  getDevice: vi.fn(),
  getMetricsHistory: vi.fn(() => Promise.resolve([])),
  deleteDevice: vi.fn(),
  rebootDevice: vi.fn(),
  startDeviceStream: vi.fn(),
  stopDeviceStream: vi.fn(),
}));
vi.mock('../api/sessions', () => ({ startSession: vi.fn() }));
vi.mock('../api/installers', () => ({
  fetchInstallers: vi.fn(() => Promise.resolve([])),
  downloadInstaller: vi.fn(),
}));
vi.mock('../api/patching', () => ({
  listPolicies: vi.fn(() => Promise.resolve([])),
  getPatchOverview: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('../api/clients', () => ({
  getClients: vi.fn(() => Promise.resolve([])),
  getClientRollups: vi.fn(() => Promise.resolve([])),
}));
vi.mock('../auth/context', () => ({
  useAuth: () => ({ accessToken: 'token', hasAnyRole: () => true }),
}));

import { getDevice, getMetricsHistory, startDeviceStream, stopDeviceStream } from '../api/devices';
import { fetchInstallers } from '../api/installers';
import { listPolicies, getPatchOverview } from '../api/patching';
import { DeviceDetail } from './DeviceDetail';
import type { Device } from '../types/device';
import type { Client } from '../api/clients';
import { getClients } from '../api/clients';
import { resetFleetPoller, useFleetStore } from '../stores/fleet';

const mk = (o: Partial<Device> = {}): Device => ({
  id: 'dev-1',
  name: 'mac-01',
  hostname: 'mac-01',
  os: 'darwin',
  arch: 'arm64',
  agent_version: '2.2.0',
  status: 'online',
  last_seen: new Date().toISOString(),
  public_ip: '203.0.113.5',
  private_ip: '10.0.0.5',
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  ...o,
});

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/devices/dev-1']}>
      <Routes>
        <Route path="/devices/:id" element={<DeviceDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Device detail · remote-control capability (2.2-1)', () => {
  beforeEach(() => {
    // vi.clearAllMocks() also drops the implementations the factories above
    // were built with, so every promise-returning call is re-armed here.
    vi.clearAllMocks();
    vi.mocked(getMetricsHistory).mockResolvedValue([]);
    vi.mocked(startDeviceStream).mockResolvedValue(undefined as never);
    vi.mocked(stopDeviceStream).mockResolvedValue(undefined as never);
    vi.mocked(fetchInstallers).mockResolvedValue([] as never);
    vi.mocked(listPolicies).mockResolvedValue([] as never);
    vi.mocked(getPatchOverview).mockResolvedValue(null as never);
  });

  it('states the capability the unit reported, with the reason', async () => {
    vi.mocked(getDevice).mockResolvedValue(mk({ input_capability: 'denied', capture_state: 'error' }));
    renderDetail();

    expect(await screen.findByText('Needs Accessibility')).toBeInTheDocument();
    expect(screen.getByText('Capture failing')).toBeInTheDocument();
    // The remedy is named in words: a colour is not the information.
    expect(screen.getByText(/grant-input-permission/)).toBeInTheDocument();
  });

  it('renders a granted unit as ready, without a warning', async () => {
    vi.mocked(getDevice).mockResolvedValue(mk({ input_capability: 'granted', capture_state: 'ok' }));
    renderDetail();

    expect(await screen.findByText('Control ready')).toBeInTheDocument();
    expect(screen.queryByText(/grant-input-permission/)).toBeNull();
    expect(screen.queryByText('Capture failing')).toBeNull();
  });

  it('never renders an unreported capability as ready', async () => {
    // An agent predating 2.2.0 reports neither field.
    vi.mocked(getDevice).mockResolvedValue(mk({}));
    renderDetail();

    expect(await screen.findByText('Not reported')).toBeInTheDocument();
    expect(screen.queryByText('Control ready')).toBeNull();
  });

  it('prints words, never a locale key path', async () => {
    // The keys are assembled from the wire value (`devices.inputDenied`):
    // a value outside the table, or a key assembled in the wrong case, reaches
    // the operator as "devices.inputdenied" if nothing catches it.
    // 'weird' stands for a value outside the contract (a newer agent, a
    // hand-edited row): the console must still print words.
    const cases: Device['input_capability'][] = [
      'denied', 'granted', 'unavailable', 'unknown',
      'weird' as Device['input_capability'],
      undefined,
    ];
    for (const cap of cases) {
      vi.mocked(getDevice).mockResolvedValue(mk({ input_capability: cap }));
      const { unmount } = renderDetail();
      await screen.findByText(/Remote input/);
      expect(document.body.textContent).not.toMatch(/devices\.(input|hint)[A-Za-z]*Denied|devices\.input|devices\.hint/);
      unmount();
    }
  });
});

describe('Device detail · whose machine this is (2.3-5)', () => {
  const tenants: Client[] = [
    { id: 'c-a', name: 'Alpha Co', slug: 'alpha-co', notes: '', disabled: false, created_at: '', updated_at: '' },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    resetFleetPoller();
    vi.mocked(getDevice).mockResolvedValue(mk({ client_id: 'c-a' }) as never);
    vi.mocked(getMetricsHistory).mockResolvedValue([] as never);
    vi.mocked(getClients).mockResolvedValue(tenants as never);
    useFleetStore.getState().setCollection('clients', tenants);
    useFleetStore.getState().setCollection('patchPolicies', []);
  });

  it('names the tenant on the unit record, and links to that tenant\'s filtered fleet', async () => {
    renderDetail();
    // The record paints after its read, so the header is awaited like every
    // other assertion in this file.
    const link = (await screen.findByLabelText('Show the Alpha Co fleet')) as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/devices?client=alpha-co');
  });

  it('says "unassigned" for a unit no tenant owns, and links to the admin\'s unplaced filter', async () => {
    // The 2.2 upgrade leaves every device unplaced, so this is the state every
    // installation is in on the day tenancy arrives. An admin must be able to
    // find exactly these units, which is why the link carries `unassigned`.
    vi.mocked(getDevice).mockResolvedValue(mk({ client_id: null }) as never);
    renderDetail();
    const link = (await screen.findByLabelText('Show the Unassigned fleet')) as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/devices?client=unassigned');
  });
});
