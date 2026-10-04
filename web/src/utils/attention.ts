import type { Device } from '../types/device';
import type { PatchOverview } from '../types/patch';

export interface AttentionItem {
  device: Device;
  reasons: string[];
  /** Lower = more urgent. */
  rank: number;
}

/**
 * Devices a technician should look at first: in alert, offline, carrying
 * critical updates, with failed installs, or waiting on a reboot. Ordered
 * most urgent first, ties broken by name.
 */
export function buildAttentionList(devices: Device[], overview: PatchOverview | null, limit = 8): AttentionItem[] {
  const patch = new Map((overview?.devices ?? []).map((d) => [d.device_id, d]));
  const items: AttentionItem[] = [];
  for (const device of devices) {
    const reasons: string[] = [];
    let rank = 99;
    const p = patch.get(device.id);
    if (device.status === 'alert') {
      reasons.push('Active alert');
      rank = Math.min(rank, 0);
    }
    if (p && p.critical > 0) {
      reasons.push(`${p.critical} critical update${p.critical === 1 ? '' : 's'}`);
      rank = Math.min(rank, 1);
    }
    if (p && p.failed > 0) {
      reasons.push(`${p.failed} failed install${p.failed === 1 ? '' : 's'}`);
      rank = Math.min(rank, 2);
    }
    if (device.status === 'offline') {
      reasons.push('Offline');
      rank = Math.min(rank, 3);
    }
    if (device.reboot_pending || p?.reboot_pending) {
      reasons.push('Reboot pending');
      rank = Math.min(rank, 4);
    }
    if (reasons.length > 0) items.push({ device, reasons, rank });
  }
  items.sort((a, b) => a.rank - b.rank || a.device.name.localeCompare(b.device.name, undefined, { numeric: true }));
  return items.slice(0, limit);
}

/** Count of devices per OS, largest first. */
export function osBreakdown(devices: Device[]): { os: Device['os']; count: number }[] {
  const counts = new Map<Device['os'], number>();
  for (const d of devices) counts.set(d.os, (counts.get(d.os) ?? 0) + 1);
  return [...counts.entries()].map(([os, count]) => ({ os, count })).sort((a, b) => b.count - a.count);
}
