import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Login } from './Login';
import { getAuthStatus } from '../api/auth';
import { listSSOProviders } from '../api/sso';

vi.mock('../api/sso', () => ({
  listSSOProviders: vi.fn(),
  authorizeProvider: vi.fn(() => ''),
  exchangeSSOCode: vi.fn(),
}));
vi.mock('../api/auth', () => ({
  getAuthStatus: vi.fn(),
}));

// The auth context's real login hits the network; substitute it with a
// spy so the test asserts the wiring (login called, error surfaced on
// failure) without a server.
const loginMock = vi.fn();
const registerMock = vi.fn();

vi.mock('../auth/context', () => ({
  useAuth: () => ({
    login: (...args: unknown[]) => loginMock(...args),
    register: (...args: unknown[]) => registerMock(...args),
    isAuthenticated: false,
    setToken: vi.fn(),
  }),
}));

async function fillLogin(username: string, password: string) {
  const inputs = await screen.findAllByRole('textbox');
  fireEvent.change(inputs[0], { target: { value: username } });
  const pwd = screen.getByPlaceholderText('Enter your password') as HTMLInputElement;
  fireEvent.change(pwd, { target: { value: password } });
  const submit = screen
    .getAllByRole('button')
    .find((b) => /log in|login|sign in/i.test(b.textContent || ''));
  fireEvent.click(submit!);
}

describe('Login page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getAuthStatus).mockResolvedValue({ has_users: true });
    vi.mocked(listSSOProviders).mockResolvedValue([]);
    loginMock.mockResolvedValue(undefined);
    registerMock.mockResolvedValue(undefined);
  });

  it('logs in with username and password', async () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Login />
      </MemoryRouter>
    );
    await fillLogin('admin', 'password123');
    await waitFor(() => expect(loginMock).toHaveBeenCalledWith('admin', 'password123'));
  });

  it('shows the error message when login fails', async () => {
    loginMock.mockRejectedValueOnce(new Error('invalid credentials'));
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Login />
      </MemoryRouter>
    );
    await fillLogin('admin', 'wrong');
    expect(await screen.findByText('invalid credentials')).toBeInTheDocument();
  });
});
