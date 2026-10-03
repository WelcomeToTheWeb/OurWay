import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import axios from 'axios';
import client from './client';
import { AuthProvider, useAuth } from '../auth/context';
import { render, screen, waitFor } from '@testing-library/react';

function LoggedIn() {
  const { isAuthenticated } = useAuth();
  return <div>{isAuthenticated ? 'authed' : 'not-authed'}</div>;
}

describe('api client 401 refresh interceptor', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('retries the original request with the new token after a 401', async () => {
    localStorage.setItem('ourway_access_token', 'expired');
    localStorage.setItem('ourway_refresh_token', 'valid-refresh');

    const refreshSpy = vi
      .spyOn(axios, 'post')
      .mockResolvedValueOnce({ data: { access_token: 'fresh-token' } });
    // Raw axios (no interceptors) is used for the refresh call, so stub
    // its adapter to record the retried request's Authorization header.
    const seenAuth: (string | undefined)[] = [];
    client.defaults.adapter = async (config: any) => {
      seenAuth.push(config?.headers?.Authorization as string | undefined);
      if (seenAuth.length === 1) {
        throw new axios.AxiosError('401', '401', config, {}, {
          status: 401,
          data: {},
          headers: {},
          config,
          statusText: 'Unauthorized',
        } as never);
      }
      return { data: { ok: true }, status: 200, statusText: 'OK', config, headers: {} };
    };

    const res = await client.get('/devices');
    expect((res.data as any).ok).toBe(true);
    expect(refreshSpy).toHaveBeenCalledWith('/api/auth/refresh', {
      refresh_token: 'valid-refresh',
    });
    expect(localStorage.getItem('ourway_access_token')).toBe('fresh-token');
    expect(seenAuth[0]).toBe('Bearer expired');
    expect(seenAuth[1]).toBe('Bearer fresh-token');
  });

  it('rejects without refreshing when there is no refresh token stored', async () => {
    localStorage.setItem('ourway_access_token', 'expired');

    const refreshSpy = vi.spyOn(axios, 'post').mockResolvedValue({ data: {} });
    client.defaults.adapter = async (config: any) => {
      throw new axios.AxiosError('401', '401', config, {}, {
        status: 401,
        data: {},
        headers: {},
        config,
        statusText: 'Unauthorized',
      } as never);
    };

    await expect(client.get('/devices')).rejects.toMatchObject({
      response: { status: 401 },
    });
    expect(refreshSpy).not.toHaveBeenCalled();
  });
});

describe('auth context logout', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('clears the persisted session on logout', async () => {
    localStorage.setItem('ourway_access_token', 'tok');
    localStorage.setItem('ourway_refresh_token', 'ref');
    localStorage.setItem(
      'ourway_user',
      JSON.stringify({ id: 'u1', username: 'admin', email: 'a@b.c', created_at: '' })
    );
    localStorage.setItem('ourway_roles', JSON.stringify(['admin']));

    function LogoutButton() {
      const { logout } = useAuth();
      return <button onClick={logout}>logout</button>;
    }

    render(
      <AuthProvider>
        <LoggedIn />
        <LogoutButton />
      </AuthProvider>
    );

    expect(await screen.findByText('authed')).toBeInTheDocument();
    screen.getByText('logout').click();
    await waitFor(() => expect(screen.getByText('not-authed')).toBeInTheDocument());
    expect(localStorage.getItem('ourway_access_token')).toBeNull();
    expect(localStorage.getItem('ourway_refresh_token')).toBeNull();
    expect(localStorage.getItem('ourway_user')).toBeNull();
    expect(localStorage.getItem('ourway_roles')).toBeNull();
  });
});
