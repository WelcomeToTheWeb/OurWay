import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { Patches } from './Patches';
import type { Device } from '../types/device';
import type { SoftwareUpdate } from '../types/patch';

vi.mock('../api/patching', () => ({
  getDeviceUpdates: vi.fn(),
  scanDeviceForUpdates: vi.fn(),
  deployNow: vi.fn(),
  approveUpdate: vi.fn(),
}));
vi.mock('../api/devices', () => ({
  getDevices: vi.fn(),
}));

import { getDeviceUpdates, scanDeviceForUpdates, deployNow, approveUpdate } from '../api/patching';
import { getDevices } from '../api/devices';

const mockedGetUpdates = vi.mocked(getDeviceUpdates);
const mockedScan = vi.mocked(scanDeviceForUpdates);
const mockedDeploy = vi.mocked(deployNow);
const mockedApprove = vi.mocked(approveUpdate);
const mockedGetDevices = vi.mocked(getDevices);

const devices: Device[] = [
  {
    id: 'd1',
    name: 'web-01',
    hostname: 'web-01',
    os: 'linux',
    arch: 'amd64',
    agent_version: '1.0.0',
    status: 'online',
    last_seen: new Date().toISOString(),
    public_ip: '1.2.3.4',
    private_ip: '10.0.0.1',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
];

function makeUpdate(overrides: Partial<SoftwareUpdate>): SoftwareUpdate {
  return {
    id: 'u1',
    device_id: 'd1',
    source: 'apt',
    title: 'openssl security update',
    version: '3.0.12',
    size_bytes: 1048576,
    status: 'detected',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  };
}

describe('Patches page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockedGetDevices.mockResolvedValue(devices);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('loads devices and lists updates for the selected device', async () => {
    mockedGetUpdates.mockResolvedValue([
      makeUpdate({}),
      makeUpdate({ id: 'u2', title: 'kernel update', status: 'installed' }),
    ]);
    render(<Patches />);

    expect(await screen.findByText('openssl security update')).toBeInTheDocument();
    expect(screen.getByText('kernel update')).toBeInTheDocument();
    expect(mockedGetUpdates).toHaveBeenCalledWith('d1');
  });

  it('shows the up-to-date empty state when there are no updates', async () => {
    mockedGetUpdates.mockResolvedValue([]);
    render(<Patches />);

    await waitFor(() =>
      expect(screen.getByText(/all systems appear to be up to date/i)).toBeInTheDocument(),
    );
  });

  it('approves a detected update and refreshes the list', async () => {
    mockedGetUpdates.mockResolvedValue([makeUpdate({})]);
    mockedApprove.mockResolvedValue(makeUpdate({ status: 'approved' }));
    render(<Patches />);

    const approveBtn = await screen.findByRole('button', { name: /approve/i });
    fireEvent.click(approveBtn);
    await waitFor(() => expect(mockedApprove).toHaveBeenCalledWith('u1'));
    await waitFor(() => expect(mockedGetUpdates).toHaveBeenCalledTimes(2));
  });

  it('scans the selected device', async () => {
    mockedGetUpdates.mockResolvedValue([]);
    mockedScan.mockResolvedValue({} as never);
    render(<Patches />);

    await waitFor(() => expect(mockedGetUpdates).toHaveBeenCalled());
    const scanBtn = screen.getByRole('button', { name: /scan/i });
    fireEvent.click(scanBtn);
    await waitFor(() => expect(mockedScan).toHaveBeenCalledWith('d1'));
  });

  it('deploys approved updates for the selected device', async () => {
    mockedGetUpdates.mockResolvedValue([makeUpdate({ status: 'approved' })]);
    mockedDeploy.mockResolvedValue({ deployment_id: 'dep1', status: 'pending' });
    render(<Patches />);

    await screen.findByText('openssl security update');
    const deployBtn = screen.getByRole('button', { name: /deploy now/i });
    fireEvent.click(deployBtn);
    await waitFor(() => expect(mockedDeploy).toHaveBeenCalledWith(['d1']));
  });

  it('filters updates by status', async () => {
    mockedGetUpdates.mockResolvedValue([
      makeUpdate({}),
      makeUpdate({ id: 'u2', title: 'installed pkg', status: 'installed' }),
    ]);
    render(<Patches />);

    await screen.findByText('openssl security update');
    fireEvent.change(screen.getByLabelText(/filter by status/i), { target: { value: 'installed' } });
    expect(screen.queryByText('openssl security update')).not.toBeInTheDocument();
    expect(screen.getByText('installed pkg')).toBeInTheDocument();
  });

  it('filters updates by search text', async () => {
    mockedGetUpdates.mockResolvedValue([
      makeUpdate({}),
      makeUpdate({ id: 'u2', title: 'vim editor', source: 'apt' }),
    ]);
    render(<Patches />);

    await screen.findByText('openssl security update');
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'vim' } });
    expect(screen.queryByText('openssl security update')).not.toBeInTheDocument();
    expect(screen.getByText('vim editor')).toBeInTheDocument();
  });
});
