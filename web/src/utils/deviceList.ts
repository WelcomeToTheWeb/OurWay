import type { Device } from '../types/device';

export type SortKey = 'name' | 'status' | 'last_seen' | 'os';

export interface DeviceFilters {
  search: string;
  status: 'all' | Device['status'];
  os: 'all' | Device['os'];
  tag: string; // '' = any
}

export const defaultFilters: DeviceFilters = { search: '', status: 'all', os: 'all', tag: '' };

// Alerts first, then offline, then online: the order a technician wants to
// triage in.
const statusRank: Record<Device['status'], number> = { alert: 0, offline: 1, online: 2 };

/** Filter then sort devices. Sorting never mutates the input. */
export function applyDeviceFilters(
  devices: Device[],
  f: DeviceFilters,
  sort: SortKey,
  desc = false,
): Device[] {
  const q = f.search.trim().toLowerCase();
  const out = devices.filter((d) => {
    if (f.status !== 'all' && d.status !== f.status) return false;
    if (f.os !== 'all' && d.os !== f.os) return false;
    if (f.tag && !(d.tags ?? []).includes(f.tag)) return false;
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
