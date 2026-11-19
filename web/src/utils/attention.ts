import type { Device } from '../types/device';
import type { Alert } from '../types/alert';
import type { PatchOverview } from '../types/patch';
import { isAlertOpen } from './alerts';

export interface AttentionItem {
  device: Device;
  reasons: string[];
  /** Lower = more urgent. */
  rank: number;
}

// Worst first, so "in alert" means the signal a technician would act on, not
// whichever row the fetch happened to return first.
const severityRank: Record<Alert['severity'], number> = { critical: 0, warning: 1, info: 2 };

/**
 * Each unit's worst open alert, in one pass: severity decides, and two fires
 * at the same severity are read newest-first — the newest is the one still
 * happening. Indexed rather than scanned per device, because the collection is
 * the whole ledger, and a lookup per unit is a multiply of two fleet-sized
 * lists.
 */
function worstOpenAlerts(alerts: readonly Alert[]): Map<string, Alert> {
  const worst = new Map<string, Alert>();
  for (const a of alerts) {
    if (!isAlertOpen(a)) continue;
    const held = worst.get(a.device_id);
    if (!held) {
      worst.set(a.device_id, a);
      continue;
    }
    const rank = severityRank[a.severity];
    const heldRank = severityRank[held.severity];
    const newer = new Date(a.created_at).getTime() > new Date(held.created_at).getTime();
    if (rank < heldRank || (rank === heldRank && newer)) worst.set(a.device_id, a);
  }
  return worst;
}

/**
 * Devices a technician should look at first: in alert, offline, carrying
 * critical updates, with failed installs, or waiting on a reboot. Ordered
 * most urgent first, ties broken by name.
 *
 * "In alert" is read from the alerts collection, not from the device row:
 * `Device.status` is presence, and a second place that says a unit is in alert
 * is the split definition 2.2-4 closed (see utils/alerts). A unit contributes
 * one alert reason, from its worst open alert, however many it is firing.
 */
export function buildAttentionList(
  devices: Device[],
  overview: PatchOverview | null,
  alerts: readonly Alert[],
  limit = 8,
): AttentionItem[] {
  const patch = new Map((overview?.devices ?? []).map((d) => [d.device_id, d]));
  const openAlert = worstOpenAlerts(alerts);
  const items: AttentionItem[] = [];
  for (const device of devices) {
    const reasons: string[] = [];
    let rank = 99;
    const p = patch.get(device.id);
    const alert = openAlert.get(device.id);
    if (alert) {
      reasons.push(`${alert.metric} ${alert.severity}`);
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
