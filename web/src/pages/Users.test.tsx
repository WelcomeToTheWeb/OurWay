import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { Users } from './Users';
import { AuthProvider } from '../auth/context';
import type { PendingIdentity } from '../api/users';
import type { Role, UserWithRoles } from '../auth/types';

vi.mock('../api/users', () => ({
  getUsers: vi.fn(),
  getRoles: vi.fn(),
  createUser: vi.fn(),
  updateUserRoles: vi.fn(),
  deleteUser: vi.fn(),
  getPendingIdentities: vi.fn(),
  approveIdentity: vi.fn(),
  mergeIdentity: vi.fn(),
}));

import {
  getUsers,
  getRoles,
  deleteUser,
  getPendingIdentities,
  approveIdentity,
  mergeIdentity,
} from '../api/users';

const mockedUsers = vi.mocked(getUsers);
const mockedRoles = vi.mocked(getRoles);
const mockedDelete = vi.mocked(deleteUser);
const mockedPending = vi.mocked(getPendingIdentities);
const mockedApprove = vi.mocked(approveIdentity);
const mockedMerge = vi.mocked(mergeIdentity);

function user(id: string, username: string, roles: string[]): UserWithRoles {
  return {
    id,
    username,
    email: `${username}@corp.example`,
    roles,
    created_at: '2026-11-20T10:00:00Z',
  };
}

function role(name: string): Role {
  return {
    id: `role-${name}`,
    name,
    description: `${name} description`,
    permissions: [],
    is_builtin: true,
    created_at: '2026-11-20T10:00:00Z',
  };
}

// The row the contract produces: an IdP asserted ops-admin@corp.example, that email
// belongs to a local admin, and OurWay parked the assertion with no roles.
function pendingIdentity(over: Partial<PendingIdentity> = {}): PendingIdentity {
  return {
    id: 'identity-1',
    provider: 'permissive-corp',
    subject: 'idp-subject-1',
    email: 'ops-admin@corp.example',
    name: 'Ops Admin',
    email_verified: true,
    created_at: '2026-11-20T10:00:00Z',
    link_target_id: 'ops-admin-local-account',
    link_target_username: 'ops-admin',
    link_target_roles: ['admin'],
    ...over,
  };
}

function setRoles(roles: string[]) {
  localStorage.setItem('ourway_roles', JSON.stringify(roles));
}

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={['/users']}>
        <Routes>
          <Route path="/users" element={<Users />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}

describe('Users — pending SSO identities (2.4-3)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(['admin']);
    mockedUsers.mockResolvedValue([user('ops-admin-local-account', 'ops-admin', ['admin'])]);
    mockedRoles.mockResolvedValue([role('admin'), role('viewer')]);
    mockedPending.mockResolvedValue([pendingIdentity()]);
    mockedApprove.mockResolvedValue({ ok: true });
    mockedMerge.mockResolvedValue({ ok: true });
  });

  it('names the asserted email, the provider, and the account it points at', async () => {
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    // The row is one line, not two columns the operator has to line up by eye: the
    // asserted identity and the account it would take over, with that account's
    // roles stated beside it.
    const row = within(band).getByText('ops-admin@corp.example').closest('tr');
    expect(row?.textContent).toContain('permissive-corp');
    expect(row?.textContent).toContain('idp-subject-1');
    expect(row?.textContent).toContain('ops-admin');
    expect(row?.textContent).toContain('admin');
    expect(within(band).getByText('Email verified by the provider')).toBeTruthy();
  });

  it('states the verified-email fact the decision turns on', async () => {
    mockedPending.mockResolvedValue([pendingIdentity({ email_verified: false })]);
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    expect(within(band).getByText('Email not verified by the provider')).toBeTruthy();
  });

  it("is admin-only: a manager never sees someone else's account being claimed", async () => {
    setRoles(['manager']);
    renderPage();
    await screen.findByRole('button', { name: /Add User/ });
    expect(mockedPending).not.toHaveBeenCalled();
    expect(screen.queryByRole('region', { name: /Pending SSO identities/ })).toBeNull();
  });

  it('says so when there is nothing pending, and reads the endpoint only as admin', async () => {
    mockedPending.mockResolvedValue([]);
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    expect(within(band).getByText('No pending identities.')).toBeTruthy();
    expect(mockedPending).toHaveBeenCalledTimes(1);
  });

  it('approves through the sheet, naming the floor role it grants', async () => {
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    fireEvent.click(within(band).getByRole('button', { name: /Approve as own account/ }));
    // The consequence is read before the verb is pressed (DESIGN.md §6: no browser
    // dialogs; the sheet carries the copy).
    const sheet = await screen.findByRole('dialog');
    expect(sheet.textContent).toContain('ops-admin@corp.example');
    expect(sheet.textContent).toContain('viewer floor');
    fireEvent.click(within(sheet).getByRole('button', { name: /Approve identity/ }));
    await waitFor(() => expect(mockedApprove).toHaveBeenCalledWith('identity-1'));
  });

  it('merges through the sheet, naming the account the identity lands in', async () => {
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    fireEvent.click(within(band).getByRole('button', { name: /Merge into account/ }));
    const sheet = await screen.findByRole('dialog');
    expect(sheet.textContent).toContain('idp-subject-1');
    expect(sheet.textContent).toContain('ops-admin');
    fireEvent.click(within(sheet).getByRole('button', { name: /Merge identity/ }));
    await waitFor(() => expect(mockedMerge).toHaveBeenCalledWith('identity-1'));
  });

  it("reports the server's reason when a decision is refused", async () => {
    mockedApprove.mockRejectedValue(new Error('that identity is already active'));
    renderPage();
    const band = await screen.findByRole('region', { name: /Pending SSO identities/ });
    fireEvent.click(within(band).getByRole('button', { name: /Approve as own account/ }));
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByRole('button', { name: /Approve identity/ }));
    // A refusal is stated at page level: the page must not look like it did nothing.
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('that identity is already active');
  });

  it('keeps deleting accounts the destructive act it was, confirmed in the sheet', async () => {
    renderPage();
    const deleteButton = await screen.findByRole('button', { name: /^Delete$/ });
    fireEvent.click(deleteButton);
    const sheet = await screen.findByRole('dialog');
    expect(sheet.textContent).toContain('ops-admin');
    fireEvent.click(within(sheet).getByRole('button', { name: /Delete user/ }));
    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith('ops-admin-local-account'));
  });
});
