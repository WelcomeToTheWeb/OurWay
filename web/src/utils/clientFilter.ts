import type { Client } from '../api/clients';
import type { Device } from '../types/device';

/**
 * The tenant axis of every ledger (2.3-5).
 *
 * The server scopes every read by rows (`user_clients`), and it states the
 * scope it applied on `X-OurWay-Client-Scope`. What lives here is the console's
 * half of the same rule: one name for the filter, one definition of
 * "unassigned", one URL parameter, and one arithmetic that turns a tenant slug
 * into the set of units it owns — shared by the devices sheet, the alert
 * ledger, the patch view, sessions, transfers, the unit view's header and the
 * dashboard's per-client rows.
 *
 * Two rules the helpers enforce:
 *
 * **The filter is URL state.** `?client=<slug>` is the whole of it, so a
 * filtered view is linkable, survives a reload, and can be pasted into a
 * support ticket. A facet that kept its state in component state would show a
 * technician one fleet and send a colleague to another.
 *
 * **The slug is the public identity, not the id.** Slugs are short, printed in
 * copy, and stable across environments; ids are uuids a technician cannot read
 * off a screen. The `unassigned` sentinel is a slug like any other, so nothing
 * in the console branches on "is this the special one".
 */

/** The slug of the bucket no tenant owns — the server's sentinel, spelled here. */
export const UNASSIGNED_SLUG = 'unassigned';

export interface ClientOption {
  slug: string;
  name: string;
}

/**
 * The facet's options: every tenant the caller may see, by name, with
 * `Unassigned` last — and only for the caller who can see those units, which is
 * `admin`. A scoped caller has no unassigned units at all, so offering the
 * option would promise a filter that always answers empty.
 */
export function clientOptions(clients: Client[], includeUnassigned: boolean): ClientOption[] {
  const options = [...clients]
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }))
    .map((c) => ({ slug: c.slug, name: c.name }));
  if (includeUnassigned) options.push({ slug: UNASSIGNED_SLUG, name: 'Unassigned' });
  return options;
}

/** The tenant the URL asks for: `''` is the unfiltered view. */
export function clientSlugFromSearch(params: URLSearchParams): string {
  return (params.get('client') ?? '').trim().toLowerCase();
}

/**
 * Write (or clear) the tenant filter without disturbing the other filters in
 * the URL — a facet that reset `?status=` while you were composing a link is a
 * facet people stop using.
 */
export function withClientSlug(params: URLSearchParams, slug: string): URLSearchParams {
  const next = new URLSearchParams(params);
  if (slug === '') next.delete('client');
  else next.set('client', slug);
  return next;
}

/**
 * The tenant key of a unit: its slug, or the sentinel.
 *
 * The wire names a tenant by id (`device.client_id`); the console names it by
 * slug, because a slug is what a link, a filter chip and a support ticket can
 * carry. This function is the one translation, at the edge where a unit's row
 * meets the tenant list — which is also why it takes the list: a unit's tenant
 * is not knowable from the unit alone once names matter. Both collections
 * arrive on the same mount (2.2-2's shared model), so the mapping is available
 * whenever a filtered view is on screen.
 *
 * An id the caller's tenant list does not name resolves to the sentinel, which
 * is the answer a filter can act on: not in your view.
 */
export function clientSlugOfDevice(
  device: { client_id?: string | null },
  clients: Client[],
): string {
  if (!device.client_id) return UNASSIGNED_SLUG;
  return clients.find((c) => c.id === device.client_id)?.slug ?? UNASSIGNED_SLUG;
}

/** Resolve a slug to its row. The sentinel has no row, and says so. */
export function clientBySlug(clients: Client[], slug: string): Client | null {
  if (slug === '' || slug === UNASSIGNED_SLUG) return null;
  return clients.find((c) => c.slug === slug) ?? null;
}

/** Display name for a tenant slug: the row's name, or the sentinel's word. */
export function clientNameForSlug(clients: Client[], slug: string): string {
  if (slug === '') return '';
  if (slug === UNASSIGNED_SLUG) return 'Unassigned';
  return clientBySlug(clients, slug)?.name ?? slug;
}

/** The units of one tenant. `''` is the whole fleet the caller can see. */
export function filterDevicesByClient(devices: Device[], slug: string, clients: Client[] = []): Device[] {
  if (slug === '') return devices;
  return devices.filter((d) => clientSlugOfDevice(d, clients) === slug);
}

/**
 * The unit ids of one tenant, as a set — the shape the entity ledgers need,
 * because an alert, a session, an update and a transfer name a *device*, not a
 * tenant. Scoping them is a lookup on this set, never a request per row.
 */
export function deviceIdsForClient(devices: Device[], slug: string, clients: Client[] = []): Set<string> {
  const ids = new Set<string>();
  for (const d of filterDevicesByClient(devices, slug, clients)) ids.add(d.id);
  return ids;
}

/** Is this unit in the tenant the URL asks for? `''` always is. */
export function deviceInClient(deviceId: string | null | undefined, slug: string, scopedIds: Set<string>): boolean {
  if (slug === '') return true;
  if (!deviceId) return false;
  return scopedIds.has(deviceId);
}

/**
 * The shift bar's reading of `X-OurWay-Client-Scope`.
 *
 * The header is ids, because ids are what the server is certain of. Printed to
 * an operator it becomes names, because an operator reading "cannot find
 * web-3" needs to know which fleet they were allowed to look at, and a uuid
 * does not answer that.
 */
export function scopeLabel(scope: string, clients: Client[]): string {
  if (scope === '') return '';
  if (scope === 'all') return 'all tenants';
  if (scope === 'none') return 'no tenants';
  const names = scope
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
    .map((id) => clients.find((c) => c.id === id)?.name ?? id);
  if (names.length === 0) return 'no tenants';
  if (names.length <= 3) return names.join(', ');
  return `${names.slice(0, 3).join(', ')} +${names.length - 3}`;
}

/** How many tenants the scope names — for the shift bar's compact stamp. */
export function scopeCount(scope: string): number {
  if (scope === 'all') return -1;
  if (scope === '' || scope === 'none') return 0;
  return scope.split(',').filter((s) => s.trim() !== '').length;
}
