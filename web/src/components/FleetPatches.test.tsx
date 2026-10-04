import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { FleetPatches } from './FleetPatches';

vi.mock('../api/patching', () => ({
  getPatchOverview: vi.fn(),
  listFleetUpdates: vi.fn(),
  bulkApproveUpdates: vi.fn(),
  bulkSkipUpdates: vi.fn(),
  deployNow: vi.fn(),
  scanDevices: vi.fn(),
}));
vi.mock('../auth/context', () => ({
  useAuth: () => ({ hasAnyRole: () => true }),
}));

import { getPatchOverview, listFleetUpdates, bulkApproveUpdates } from '../api/patching';

const overview = {
  totals: { devices: 4, compliant: 3, pending: 2, critical: 2, reboot_pending: 0 },
  devices: [
    { device_id: 'd1', name: 'alpha', os: 'windows', status: 'online', detected: 1, approved: 0, installing: 0, failed: 0, critical: 1, reboot_pending: false, compliant: false },
  ],
};
const updates = [
  { key: 'k1', title: '2026-10 Cumulative Update', kb: '5001', severity: 'critical', source: 'windows-update', category: 'Security', devices: 2, statuses: { detected: 2 }, update_ids: ['u1', 'u2'], detected_ids: ['u1', 'u2'] },
];

describe('FleetPatches', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getPatchOverview).mockResolvedValue(overview as never);
    vi.mocked(listFleetUpdates).mockResolvedValue(updates as never);
    vi.mocked(bulkApproveUpdates).mockResolvedValue({ changed: 2 });
  });

  it('shows compliance and grouped updates', async () => {
    render(<MemoryRouter><FleetPatches /></MemoryRouter>);
    expect(await screen.findByText('75%')).toBeTruthy();
    expect(screen.getByText('2026-10 Cumulative Update')).toBeTruthy();
    expect(screen.getByText('alpha')).toBeTruthy();
  });

  it('approves an update on all devices in one call', async () => {
    render(<MemoryRouter><FleetPatches /></MemoryRouter>);
    fireEvent.click(await screen.findByText('Approve on all'));
    await waitFor(() => expect(bulkApproveUpdates).toHaveBeenCalledWith(['u1', 'u2']));
    expect(await screen.findByText('Approved on 2 device(s)')).toBeTruthy();
  });

  it('filters by search', async () => {
    render(<MemoryRouter><FleetPatches /></MemoryRouter>);
    await screen.findByText('2026-10 Cumulative Update');
    fireEvent.change(screen.getByLabelText('Search updates'), { target: { value: 'nomatch' } });
    expect(screen.getByText('No updates match the filters.')).toBeTruthy();
  });
});
