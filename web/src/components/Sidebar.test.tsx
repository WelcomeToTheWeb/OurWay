import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';

let roles: string[] = [];
vi.mock('../auth/context', () => ({
  useAuth: () => ({
    hasAnyRole: (r: string[]) => r.some((x) => roles.includes(x)),
    hasRole: (r: string) => roles.includes(r),
  }),
}));

const renderSidebar = () => render(<MemoryRouter><Sidebar /></MemoryRouter>);

describe('Sidebar', () => {
  it('shows Monitor, Manage and Admin groups to an admin', () => {
    roles = ['admin'];
    renderSidebar();
    for (const g of ['Monitor', 'Manage', 'Admin']) expect(screen.getByRole('group', { name: g })).toBeTruthy();
    expect(screen.getByText('Settings')).toBeTruthy();
  });

  it('drops the Admin group and manager-only links for a technician', () => {
    roles = ['technician'];
    renderSidebar();
    expect(screen.queryByRole('group', { name: 'Admin' })).toBeNull();
    expect(screen.queryByText('Policies')).toBeNull();
    expect(screen.getByText('Patches')).toBeTruthy();
  });

  it('shows only Monitor to a viewer', () => {
    roles = ['viewer'];
    renderSidebar();
    expect(screen.getByRole('group', { name: 'Monitor' })).toBeTruthy();
    expect(screen.queryByRole('group', { name: 'Manage' })).toBeNull();
  });
});
