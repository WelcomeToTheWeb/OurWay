import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider, useAuth } from '../auth/context';
import { RoleRoute } from './RoleRoute';

function Dashboard() {
  return <div>Dashboard</div>;
}

function UsersPage() {
  return <div>Users admin page</div>;
}

function Probe() {
  const { isAuthenticated } = useAuth();
  return <div>{isAuthenticated ? 'authenticated' : 'anonymous'}</div>;
}

describe('RoleRoute', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('renders children for a user with a required role', async () => {
    localStorage.setItem(
      'ourway_user',
      JSON.stringify({ id: 'u1', username: 'admin', email: 'a@b.c', created_at: '' })
    );
    localStorage.setItem('ourway_roles', JSON.stringify(['admin']));
    localStorage.setItem('ourway_access_token', 'tok');
    localStorage.setItem('ourway_refresh_token', 'ref');

    render(
      <AuthProvider>
        <MemoryRouter initialEntries={['/users']}>
          <Routes>
            <Route
              path="/users"
              element={
                <RoleRoute roles={['admin']}>
                  <UsersPage />
                </RoleRoute>
              }
            />
            <Route path="/" element={<Dashboard />} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    );

    expect(await screen.findByText('Users admin page')).toBeInTheDocument();
  });

  it('redirects to / when the user lacks the required role', async () => {
    localStorage.setItem(
      'ourway_user',
      JSON.stringify({ id: 'u2', username: 'viewer', email: 'v@b.c', created_at: '' })
    );
    localStorage.setItem('ourway_roles', JSON.stringify(['viewer']));
    localStorage.setItem('ourway_access_token', 'tok');
    localStorage.setItem('ourway_refresh_token', 'ref');

    render(
      <AuthProvider>
        <MemoryRouter initialEntries={['/users']}>
          <Routes>
            <Route
              path="/users"
              element={
                <RoleRoute roles={['admin']}>
                  <UsersPage />
                </RoleRoute>
              }
            />
            <Route path="/" element={<Dashboard />} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    );

    expect(await screen.findByText('Dashboard')).toBeInTheDocument();
    expect(screen.queryByText('Users admin page')).not.toBeInTheDocument();
  });

  it('redirects when there are no roles at all', async () => {
    localStorage.setItem('ourway_access_token', 'tok');
    localStorage.setItem('ourway_refresh_token', 'ref');

    render(
      <AuthProvider>
        <MemoryRouter initialEntries={['/users']}>
          <Routes>
            <Route
              path="/users"
              element={
                <RoleRoute roles={['admin']}>
                  <UsersPage />
                </RoleRoute>
              }
            />
            <Route path="/" element={<Dashboard />} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    );

    expect(await screen.findByText('Dashboard')).toBeInTheDocument();
  });
});

describe('useAuth', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('is authenticated when an access token is present', async () => {
    localStorage.setItem('ourway_access_token', 'tok');
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    expect(await screen.findByText('authenticated')).toBeInTheDocument();
  });

  it('is anonymous without a token', async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    expect(await screen.findByText('anonymous')).toBeInTheDocument();
  });
});
