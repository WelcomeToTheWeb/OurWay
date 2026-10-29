import client from './client';
import type { Role, UserWithRoles } from '../auth/types';

export const getUsers = () =>
  client.get<{ users: UserWithRoles[] }>('/users').then((r) => r.data.users);

export const createUser = (data: {
  username: string;
  email: string;
  password: string;
  roles: string[];
}) => client.post('/users', data).then((r) => r.data);

export const updateUserRoles = (id: string, roles: string[]) =>
  client.put(`/users/${id}/roles`, { roles }).then((r) => r.data);

export const deleteUser = (id: string) =>
  client.delete(`/users/${id}`).then((r) => r.data);

export const getRoles = () =>
  client.get<{ roles: Role[] }>('/roles').then((r) => r.data.roles);

export const createRole = (data: {
  name: string;
  description: string;
  permissions: string[];
}) => client.post('/roles', data).then((r) => r.data);

export const updateRole = (id: string, data: {
  name: string;
  description: string;
  permissions: string[];
}) => client.put(`/roles/${id}`, data).then((r) => r.data);

export const deleteRole = (id: string) =>
  client.delete(`/roles/${id}`).then((r) => r.data);

/**
 * A pending SSO identity (2.4-3): an identity provider asserted this email, and the
 * email belongs to an account that already exists. OurWay does not link those by
 * itself, so the assertion sits here — no roles, no login — until an admin approves
 * it as its own account, merges it into the account it points at, or refuses it.
 */
export interface PendingIdentity {
  id: string;
  provider: string;
  subject: string;
  email: string;
  name?: string;
  email_verified: boolean;
  created_at: string;
  link_target_id?: string;
  link_target_username?: string;
  link_target_roles?: string[];
}

export const getPendingIdentities = () =>
  client.get<{ identities: PendingIdentity[] }>('/users/pending').then((r) => r.data.identities);

export const approveIdentity = (id: string) =>
  client.post(`/users/${id}/approve`, {}).then((r) => r.data);

export const mergeIdentity = (id: string) =>
  client.post(`/users/${id}/merge`, {}).then((r) => r.data);
