import type { Device } from '../types/device';
import type { Client } from '../api/clients';
import { clientSlugOfDevice } from './clientFilter';

export type SortKey = 'name' | 'status' | 'last_seen' | 'os';

export interface DeviceFilters {
  search: string;
  status: 'all' | Device['status'];
  os: 'all' | Device['os'];
  tag: string; // '' = any
  /** Tenant slug (`?client=`); '' is every tenant the caller may see. */
  client: string;
}

export const defaultFilters: DeviceFilters = { search: '', status: 'all', os: 'all', tag: '', client: '' };

// Alerts first, then offline, then online: the order a technician wants to
// triage in.
const statusRank: Record<Device['status'], number> = { alert: 0, offline: 1, online: 2 };

/** Filter then sort devices. Sorting never mutates the input. */
export function applyDeviceFilters(
  devices: Device[],
  f: DeviceFilters,
  sort: SortKey,
  desc = false,
  clients: Client[] = [],
): Device[] {
  const q = f.search.trim().toLowerCase();
  const out = devices.filter((d) => {
    if (f.status !== 'all' && d.status !== f.status) return false;
    if (f.os !== 'all' && d.os !== f.os) return false;
    if (f.tag && !(d.tags ?? []).includes(f.tag)) return false;
    // The tenant facet (2.3-5): the filter's value is a slug, and a unit's
    // tenant is resolved through the tenant list, so the sheet, the column, the
    // card label and the URL all speak slug.
    if (f.client && clientSlugOfDevice(d, clients) !== f.client) return false;
    if (!q) return true;
    return (
      d.name.toLowerCase().includes(q) ||
      d.hostname.toLowerCase().includes(q) ||
      (d.private_ip ?? '').includes(q) ||
      (d.public_ip ?? '').includes(q) ||
      (d.tags ?? []).some((t) => t.includes(q))
    );
  });
  const dir = desc ? -1 : 1;
  out.sort((a, b) => {
    let c = 0;
    switch (sort) {
      case 'status':
        c = statusRank[a.status] - statusRank[b.status];
        break;
      case 'last_seen':
        c = new Date(a.last_seen).getTime() - new Date(b.last_seen).getTime();
        break;
      case 'os':
        c = a.os.localeCompare(b.os);
        break;
      default:
        c = 0;
    }
    if (c === 0) c = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
    return c * dir;
  });
  return out;
}

/** Distinct tags across devices, sorted. */
export function collectTags(devices: Device[]): string[] {
  return [...new Set(devices.flatMap((d) => d.tags ?? []))].sort();
}

/**
 * The sheet's view is its URL (2.1.0's view-in-URL rule, finished in 2.3-5).
 *
 * Every control on the filter row is a parameter, and every parameter is a
 * control: `?client=acme&status=alert&sort=last-seen`. The reason is not
 * tidiness. A technician under a `?client=` link from a ticket, a bookmark, or
 * a browser back must land on the fleet they were pointed at, with the sort
 * they were told to read — and a page that kept those in component state
 * answered a reload with a different sheet than the one it displayed.
 *
 * The URL is therefore the source, and the setters write it. Unknown values
 * fall back to the default rather than rendering a sheet that claims a filter
 * it is not applying.
 */

/** Sort keys are spelled the way a person types them in a link. */
const SORT_FROM_URL: Record<string, SortKey> = {
  name: 'name',
  status: 'status',
  'last-seen': 'last_seen',
  last_seen: 'last_seen',
  os: 'os',
};

const SORT_TO_URL: Record<SortKey, string> = {
  name: 'name',
  status: 'status',
  last_seen: 'last-seen',
  os: 'os',
};

/** The parameters this view owns; anything else in the URL is left alone. */
const VIEW_PARAMS = ['q', 'client', 'status', 'os', 'tag', 'sort', 'dir', 'view'];

export function devicesViewFromSearch(params: URLSearchParams): DeviceFilters & { sort: SortKey; desc: boolean } {
  const status = params.get('status');
  const os = params.get('os');
  const dir = params.get('dir');
  return {
    search: params.get('q') ?? '',
    client: (params.get('client') ?? '').trim().toLowerCase(),
    status: status === 'online' || status === 'offline' || status === 'alert' ? status : 'all',
    os: os === 'windows' || os === 'linux' || os === 'darwin' ? os : 'all',
    tag: params.get('tag') ?? '',
    sort: SORT_FROM_URL[params.get('sort') ?? ''] ?? 'name',
    desc: dir === 'desc',
  };
}

export function devicesViewToParams(
  v: DeviceFilters & { sort: SortKey; desc: boolean },
  params: URLSearchParams,
  view?: 'cards' | 'table',
): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of VIEW_PARAMS) if (key !== 'view' || view !== undefined) next.delete(key);
  if (v.search) next.set('q', v.search);
  if (v.client) next.set('client', v.client);
  if (v.status !== 'all') next.set('status', v.status);
  if (v.os !== 'all') next.set('os', v.os);
  if (v.tag) next.set('tag', v.tag);
  if (v.sort !== 'name') next.set('sort', SORT_TO_URL[v.sort]);
  if (v.desc) next.set('dir', 'desc');
  if (view === 'table') next.set('view', 'table');
  return next;
}

/** Any filter at all? Drives the "N of M units in view" line and Clear. */
/**
 * Are any filters on? Explicitly field by field, because the caller is the
 * whole view object — filters plus sort and direction — and a JSON comparison
 * against `defaultFilters` reads "sorted" as "filtered". That misreport is what
 * made an ungranted tenant's empty fleet say "no devices match your search"
 * with a Clear-filters button, when the honest answer is "you have no devices".
 */
export function isFiltered(v: DeviceFilters): boolean {
  return (
    v.search.trim() !== '' ||
    v.status !== 'all' ||
    v.os !== 'all' ||
    v.tag.trim() !== '' ||
    v.client.trim() !== ''
  );
}
