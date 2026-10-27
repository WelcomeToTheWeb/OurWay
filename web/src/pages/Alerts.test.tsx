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
import { resetFleetPoller } from '../stores/fleet';

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
    // The ledger is a view over the shared collection; the collection is
    // module state, so a test starts with an empty one.
    resetFleetPoller();
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

  it('counts "open" the way the ledger counts it: acknowledged included', async () => {
    // 2.2-4: the dashboard tile, the ledger header and the chips must be the
    // same population. Open is unresolved; a sign-off does not close a signal.
    mockedGetAlerts.mockResolvedValue([
      makeAlert({ id: 'a1', message: 'unacknowledged signal' }),
      makeAlert({ id: 'a2', message: 'acknowledged signal', acknowledged: true }),
      makeAlert({ id: 'a3', message: 'closed signal', acknowledged: true, resolved: true }),
    ]);
    render(<Alerts />);
    await screen.findByText('unacknowledged signal');

    expect(await screen.findByText(/2 open · 3 on the sheet/)).toBeInTheDocument();
    // The chip population matches the rows exactly.
    const chips = screen.getAllByRole('button', { name: /^\d+ (All|Open|Signed off|Resolved)$/ });
    const byLabel = Object.fromEntries(
      chips.map((chip) => {
        const text = (chip.textContent ?? '').trim();
        const [count, ...rest] = text.split(' ');
        return [rest.join(' '), Number(count)];
      })
    );
    expect(byLabel).toEqual({ All: 3, Open: 2, 'Signed off': 1, Resolved: 1 });
  });

  it('the chips count what the filters leave on screen', async () => {
    mockedGetAlerts.mockResolvedValue([
      makeAlert({ id: 'a1', device_name: 'web-01', message: 'CPU above threshold' }),
      makeAlert({ id: 'a2', device_name: 'db-01', message: 'Disk almost full' }),
    ]);
    render(<Alerts />);
    await screen.findByText('CPU above threshold');

    fireEvent.change(screen.getByPlaceholderText(/search alerts/i), { target: { value: 'db-01' } });

    // The chips measure the searched population, so they move with the search
    // and describe the rows the operator is looking at.
    const chip = await screen.findByRole('button', { name: /^1 All$/ });
    expect(chip).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^1 Open$/ })).toBeInTheDocument();
    expect(screen.getByText(/of 1 matching · 2 in the ledger/)).toBeInTheDocument();
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

  it('resolves an alert: the mutation re-reads the collection the counts come from', async () => {
    mockedGetAlerts
      .mockResolvedValueOnce([makeAlert({})])
      .mockResolvedValue([makeAlert({ resolved: true })]);
    mockedResolve.mockResolvedValue({} as never);
    render(<Alerts />);

    // The row action is "Resolve"; the count chip is "0 Resolved".
    const resolveBtn = await screen.findByRole('button', { name: /^resolve$/i });
    fireEvent.click(resolveBtn);
    await waitFor(() => expect(mockedResolve).toHaveBeenCalledWith('a1'));
    // The row's new state comes from the re-read, not from a local patch: the
    // counts on the dashboard tile are computed from the same collection, and
    // a local patch would leave them behind.
    await waitFor(() => expect(mockedGetAlerts).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /^resolve$/i })).not.toBeInTheDocument(),
    );
    expect(screen.getAllByText('Resolved').length).toBeGreaterThanOrEqual(1);
  });

  it('acknowledges an alert and shows the acknowledged badge', async () => {
    mockedGetAlerts
      .mockResolvedValueOnce([makeAlert({})])
      .mockResolvedValue([makeAlert({ acknowledged: true })]);
    mockedAck.mockResolvedValue({} as never);
    render(<Alerts />);

    // The ack button's label is the short "Ack" (title is the long form).
    const ackBtn = await screen.findByRole('button', { name: /^ack$/i });
    fireEvent.click(ackBtn);
    await waitFor(() => expect(mockedAck).toHaveBeenCalledWith('a1'));
    await waitFor(() => expect(mockedGetAlerts).toHaveBeenCalledTimes(2));
    expect(await screen.findByText('Acknowledged')).toBeInTheDocument();
    // Acknowledging is not closing: the alert is still open.
    expect(await screen.findByText(/1 open · 1 on the sheet/)).toBeInTheDocument();
  });

  it('assigns an alert to a user', async () => {
    mockedGetAlerts
      .mockResolvedValueOnce([makeAlert({})])
      .mockResolvedValue([makeAlert({ assigned_to: 'alice' })]);
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

  it('filters by severity in the browser, over the whole collection', async () => {
    mockedGetAlerts.mockResolvedValue([
      makeAlert({ id: 'a1', severity: 'critical', message: 'CPU above threshold' }),
      makeAlert({ id: 'a2', severity: 'warning', message: 'RAM high' }),
    ]);
    render(<Alerts />);
    await screen.findByText('CPU above threshold');

    fireEvent.change(screen.getByLabelText(/filter by severity/i), { target: { value: 'warning' } });
    await waitFor(() => expect(screen.queryByText('CPU above threshold')).not.toBeInTheDocument());
    expect(screen.getByText('RAM high')).toBeInTheDocument();
  });

  it('filters resolved vs unresolved in the browser', async () => {
    mockedGetAlerts.mockResolvedValue([
      makeAlert({ id: 'a1', message: 'still open' }),
      makeAlert({ id: 'a2', message: 'closed', resolved: true }),
    ]);
    render(<Alerts />);
    await screen.findByText('still open');

    fireEvent.change(screen.getByLabelText(/filter by status/i), { target: { value: 'resolved' } });
    await waitFor(() => expect(screen.queryByText('still open')).not.toBeInTheDocument());
    expect(screen.getByText('closed')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/filter by status/i), { target: { value: 'unresolved' } });
    await waitFor(() => expect(screen.queryByText('closed')).not.toBeInTheDocument());
    expect(screen.getByText('still open')).toBeInTheDocument();
  });
});
