import client from './client';

/**
 * 2.3-4 — the client (tenant) surface.
 *
 * The server answers every scoped read with `X-OurWay-Client-Scope`, and the
 * console reads it: "the technician cannot find the unit" is the support ticket
 * this release is measured by, and the header is the answer to it. `lastScope`
 * is the most recent scope the server said it applied, which the shift bar
 * prints (2.3-5) so the reason is visible without opening the network tab.
 */

export interface Client {
  id: string;
  name: string;
  slug: string;
  notes: string;
  disabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ClientRollup {
  client_id: string;
  name: string;
  slug: string;
  disabled: boolean;
  units: number;
  units_online: number;
  last_seen?: string | null;
  alerts_open: number;
  alerts_critical: number;
  alerts_warning: number;
  alerts_info: number;
  patch_units: number;
  patch_compliant: number;
  patch_pending: number;
  patch_critical: number;
  patch_reboot_pending: number;
  technicians: number;
}

export interface ClientUser {
  id: string;
  username: string;
  email: string;
}

/**
 * The scope the server last said it applied is captured in `api/client.ts`,
 * on the response interceptor: every scoped read carries the header, so the
 * shift bar's answer does not depend on which page happened to read last.
 */
export { scopeHeader } from './client';

export const getClients = () => client.get<{ clients: Client[] }>('/clients').then((r) => r.data.clients);

/**
 * Every per-client figure in one read (2.3-5).
 *
 * Asked per client, a 40-tenant installation costs the dashboard 40 reads on
 * mount. Asked once, it costs the same handful of reads whatever the tenant
 * count - and the dashboard's rows are then the Clients ledger's rows by
 * construction, which is 2.2-4's single-source rule applied to the tenant axis.
 */
export const getClientRollups = () =>
  client.get<{ rollups: ClientRollup[] }>('/clients/rollups').then((r) => r.data.rollups);

export const getClientRollup = (id: string) =>
  client.get<{ rollup: ClientRollup }>(`/clients/${id}/rollup`).then((r) => r.data.rollup);

export const createClient = (data: { name: string; slug?: string; notes?: string }) =>
  client.post<{ client: Client }>('/clients', data).then((r) => r.data.client);

export const updateClient = (
  id: string,
  data: { name: string; slug?: string; notes?: string; disabled?: boolean }
) => client.put<{ client: Client }>(`/clients/${id}`, data).then((r) => r.data.client);

/** Deleting a client moves its devices to unassigned; it never deletes devices. */
export const deleteClient = (id: string) =>
  client.delete<{ status: string; devices_unassigned: number }>(`/clients/${id}`).then((r) => r.data);

export const assignClientDevices = (id: string, deviceIds: string[]) =>
  client
    .put<{ status: string; assigned?: number; unassigned?: number }>(`/clients/${id}/devices`, {
      device_ids: deviceIds,
    })
    .then((r) => r.data);

export const getClientUsers = (id: string) =>
  client.get<{ users: ClientUser[] }>(`/clients/${id}/users`).then((r) => r.data.users);

export const setClientUsers = (id: string, userIds: string[]) =>
  client
    .put<{ status: string; users: number }>(`/clients/${id}/users`, { user_ids: userIds })
    .then((r) => r.data);

/** The client filter is URL state (2.3-5): `?client=<slug>`, linkable, survives reload. */
export function clientFromSearch(search: string): string {
  return new URLSearchParams(search).get('client') ?? '';
}

export function withClientParam(search: string, slug: string): string {
  const params = new URLSearchParams(search);
  if (slug === '') params.delete('client');
  else params.set('client', slug);
  const out = params.toString();
  return out ? `?${out}` : '';
}
