import client from './client';
import type { AuthStatus } from '../auth/types';

export const getAuthStatus = () =>
  client.get<AuthStatus>('/auth/status').then((r) => r.data);

export const updateProfile = (profile: { username?: string; email?: string }) =>
  client.put<{ user: { username: string; email: string } }>('/auth/profile', profile).then((r) => r.data);

export const updatePassword = (password: { current_password: string; new_password: string }) =>
  client.put<{ status: string }>('/auth/password', password).then((r) => r.data);

export const deleteAccount = () =>
  client.delete<{ status: string }>('/auth/me').then((r) => r.data);
