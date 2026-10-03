import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { Alerts } from './Alerts';
import type { Alert } from '../types/alert';
import type { UserWithRoles } from '../auth/types';

vi.mock('../api/devices', () => ({
  getAlerts: vi.fn(),
  resolveAlert: vi.fn(),
  acknowledgeAlert: vi.fn(),
  assignAlert: vi.fn(),
}));
vi.mock('../api/users', () => ({
  getUsers: vi.fn(),
}));

import { getAlerts, resolveAlert, acknowledgeAlert, assignAlert } from '../api/devices';
import { getUsers } from '../api/users';

const mockedGetAlerts = vi.mocked(getAlerts);
const mockedResolve = vi.mocked(resolveAlert);
const mockedAck = vi.mocked(acknowledgeAlert);
const mockedAssign = vi.mocked(assignAlert);
const mockedGetUsers = vi.mocked(getUsers);

function makeAlert(overrides: Partial<Alert>): Alert {
  return {
    id: 'a1',
    device_id: 'd1',
    device_name: 'web-01',
    severity: 'critical',
    message: 'CPU above threshold',
    metric: 'cpu',
    value: 95,
    threshold: 90,
    resolved: false,
    acknowledged: false,
    created_at: new Date(Date.now() - 60000).toISOString(),
    ...overrides,
  };
}

const users: UserWithRoles[] = [
  { id: 'u1', username: 'alice', email: 'alice@example.com', created_at: '', roles: ['admin'] } as UserWithRoles,
];

describe('Alerts page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockedGetUsers.mockResolvedValue(users);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders alerts with severity badges and device names', async () => {
    mockedGetAlerts.mockResolvedValue([
      makeAlert({}),
      makeAlert({ id: 'a2', severity: 'warning', message: 'RAM high', metric: 'ram' }),
      makeAlert({ id: 'a3', resolved: true, message: 'disk ok now', metric: 'disk' }),
    ]);
    render(<Alerts />);

    expect(await screen.findByText('CPU above threshold')).toBeInTheDocument();
    expect(screen.getByText('RAM high')).toBeInTheDocument();
    // Severity badges render on the alert rows (the filter select also
    // contains these labels, so scope to the badge styling).
    expect(screen.getAllByText('Critical').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('Warning').length).toBeGreaterThanOrEqual(2);
    // Device name renders alongside the metric on every alert row.
    expect(screen.getAllByText(/web-01/).length).toBeGreaterThanOrEqual(3);
    // Resolved alerts show a resolved badge.
    expect(screen.getAllByText('Resolved').length).toBeGreaterThanOrEqual(2);
  });

  it('shows the empty state when there are no alerts', async () => {
    mockedGetAlerts.mockResolvedValue([]);
    render(<Alerts />);

    await waitFor(() =>
      expect(screen.getByText(/no alerts match the current filters/i)).toBeInTheDocument(),
    );
  });

  it('filters by search text across device name and message', async () => {
    mockedGetAlerts.mockResolvedValue([
      makeAlert({ device_name: 'web-01', message: 'CPU above threshold' }),
      makeAlert({ id: 'a2', device_name: 'db-01', message: 'Disk almost full' }),
    ]);
    render(<Alerts />);

    await screen.findByText('CPU above threshold');
    const search = screen.getByPlaceholderText(/search alerts/i);
    fireEvent.change(search, { target: { value: 'db-01' } });
    expect(screen.queryByText('CPU above threshold')).not.toBeInTheDocument();
    expect(screen.getByText('Disk almost full')).toBeInTheDocument();
  });

  it('resolves an alert and updates the list optimistically', async () => {
    mockedGetAlerts.mockResolvedValue([makeAlert({})]);
    mockedResolve.mockResolvedValue({} as never);
    render(<Alerts />);

    const resolveBtn = await screen.findByRole('button', { name: /resolve/i });
    fireEvent.click(resolveBtn);
    await waitFor(() => expect(mockedResolve).toHaveBeenCalledWith('a1'));
    // After resolving, the row shows the resolved badge and the action
    // buttons disappear.
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /resolve/i })).not.toBeInTheDocument(),
    );
  });

  it('acknowledges an alert and shows the acknowledged badge', async () => {
    mockedGetAlerts.mockResolvedValue([makeAlert({})]);
    mockedAck.mockResolvedValue({} as never);
    render(<Alerts />);

    // The ack button's label is the short "Ack" (title is the long form).
    const ackBtn = await screen.findByRole('button', { name: /^ack$/i });
    fireEvent.click(ackBtn);
    await waitFor(() => expect(mockedAck).toHaveBeenCalledWith('a1'));
    expect(await screen.findByText('Acknowledged')).toBeInTheDocument();
  });

  it('assigns an alert to a user', async () => {
    mockedGetAlerts.mockResolvedValue([makeAlert({})]);
    mockedAssign.mockResolvedValue({} as never);
    render(<Alerts />);

    const assignBtn = await screen.findByRole('button', { name: /^assign$/i });
    fireEvent.click(assignBtn);
    // The assign picker offers the loaded users; pick alice and confirm.
    const select = await screen.findByLabelText(/assign to user/i);
    fireEvent.change(select, { target: { value: 'alice' } });
    const confirmBtn = await screen.findByRole('button', { name: /^assign$/i });
    fireEvent.click(confirmBtn);
    await waitFor(() => expect(mockedAssign).toHaveBeenCalledWith('a1', 'alice'));
    expect(await screen.findByText('alice')).toBeInTheDocument();
  });

  it('filters by severity via the severity select', async () => {
    mockedGetAlerts.mockResolvedValue([makeAlert({})]);
    render(<Alerts />);

    await screen.findByText('CPU above threshold');
    const severitySelect = screen.getByLabelText(/filter by severity/i);
    fireEvent.change(severitySelect, { target: { value: 'critical' } });
    await waitFor(() => expect(mockedGetAlerts).toHaveBeenCalledWith({ severity: 'critical' }));
  });

  it('filters resolved vs unresolved via the status select', async () => {
    mockedGetAlerts.mockResolvedValue([]);
    render(<Alerts />);

    await waitFor(() => expect(mockedGetAlerts).toHaveBeenCalled());
    const statusSelect = screen.getByLabelText(/filter by status/i);
    fireEvent.change(statusSelect, { target: { value: 'resolved' } });
    await waitFor(() => expect(mockedGetAlerts).toHaveBeenCalledWith({ resolved: 'true' }));
  });
});
