import client from './client';
import type { AuthStatus } from '../auth/types';

export const getAuthStatus = () =>
  client.get<AuthStatus>('/auth/status').then((r) => r.data);
