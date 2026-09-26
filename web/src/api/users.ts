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
