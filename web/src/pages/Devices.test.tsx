import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Devices } from './Devices';
import type { Device } from '../types/device';

vi.mock('../hooks/useWebSocket', () => ({ useWebSocket: () => ({ connected: true }) }));
vi.mock('../components/InstallerPanel', () => ({ InstallerPanel: () => null }));
vi.mock('../auth/context', () => ({
  useAuth: () => ({ accessToken: 't', hasAnyRole: () => true }),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn() }));
vi.mock('../api/tags', () => ({ bulkTags: vi.fn(), setDeviceTags: vi.fn(), listTags: vi.fn() }));
vi.mock('../api/patching', () => ({ scanDevices: vi.fn(), deployNow: vi.fn() }));

import { getDevices } from '../api/devices';
import { bulkTags } from '../api/tags';
import { scanDevices } from '../api/patching';

const mk = (name: string, o: Partial<Device> = {}): Device => ({
  id: name, name, hostname: `${name}.lan`, os: 'linux', arch: 'amd64', agent_version: '1', status: 'online',
  last_seen: new Date().toISOString(), public_ip: '', private_ip: '', created_at: '', updated_at: '', tags: [], ...o,
});

describe('Devices page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.setItem('devices.view', 'table');
    vi.mocked(getDevices).mockResolvedValue([mk('web-1', { tags: ['prod'] }), mk('web-2'), mk('db-1', { os: 'windows', status: 'offline' })]);
    vi.mocked(bulkTags).mockResolvedValue({ updated: 2 });
    vi.mocked(scanDevices).mockResolvedValue({ scans_sent: 2 });
  });

  it('filters by OS and tag', async () => {
    render(<MemoryRouter><Devices /></MemoryRouter>);
    await screen.findByText('db-1');
    fireEvent.change(screen.getByLabelText('Filter by OS'), { target: { value: 'windows' } });
    expect(screen.queryByText('web-1')).toBeNull();
    expect(screen.getByText('db-1')).toBeTruthy();
    fireEvent.click(screen.getByText('Clear filters'));
    fireEvent.change(screen.getByLabelText('Filter by tag'), { target: { value: 'prod' } });
    expect(screen.getByText('web-1')).toBeTruthy();
    expect(screen.queryByText('web-2')).toBeNull();
  });

  it('bulk-tags and scans only the selected devices', async () => {
    render(<MemoryRouter><Devices /></MemoryRouter>);
    await screen.findByText('web-2');
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
    await screen.findByText('web-2');
    fireEvent.click(screen.getByLabelText('Select web-1'));
    fireEvent.change(screen.getByLabelText('Filter by OS'), { target: { value: 'windows' } });
    expect(screen.queryByRole('toolbar', { name: 'Bulk actions' })).toBeNull();
  });
});
