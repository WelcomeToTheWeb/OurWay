import type { Device } from '../types/device';
import type { DeviceCompliance, PatchPolicy } from '../types/patch';
import { formatWindow } from './patchWindow';

/**
 * Which patch policy governs a unit — mirroring the server's scope rules
 * (PolicyEngine.resolveScope): all devices, named devices (ids, names or
 * hostnames, comma-separated), or tags.
 */
export function policyForDevice(policies: PatchPolicy[], device: Device): PatchPolicy | null {
  const scoped = policies.filter((p) => {
    if (p.scope === 'all') return true;
    const values = (p.scope_value || '')
      .split(',')
      .map((v) => v.trim().toLowerCase())
      .filter(Boolean);
    if (p.scope === 'devices') {
      return values.includes(device.id.toLowerCase()) || values.includes(device.name.toLowerCase()) || values.includes(device.hostname.toLowerCase());
    }
    if (p.scope === 'tags') {
      const tags = device.tags ?? [];
      return values.some((v) => tags.includes(v));
    }
    return false;
  });
  if (scoped.length === 0) return null;
  // The tightest scope wins: named devices, then tags, then all.
  const rank = { devices: 0, tags: 1, all: 2 } as const;
  return scoped.sort((a, b) => rank[a.scope] - rank[b.scope])[0] ?? null;
}

export interface ServiceRecordFields {
  lastLabel: string;
  last: string;
  nextLabel: string;
  next: string;
  intervalLabel: string;
  interval: string;
  nextTone: 'default' | 'due' | 'alert' | 'ok';
}
/** "14m ago" / "3h ago" / a date — how long since the unit last reported. */
export function agoLabel(iso: string, now: number = Date.now()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return 'no record';
  const diff = now - d.getTime();
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return d.toISOString().slice(0, 10);
}

/**
 * The record a decal prints: last seen, next due, service interval.
 * Compliance comes from the patch overview; the interval from the policy
 * that scopes this unit. "No policy" is said out loud, not hidden.
 */
export function deviceRecord(
  device: Device,
  compliance: DeviceCompliance | undefined,
  policy: PatchPolicy | null,
  labels: { lastSeen: string; nextDue: string; interval: string; noPolicy?: string },
  now: number = Date.now(),
): ServiceRecordFields {
  const interval = policy
    ? `${policy.schedule} · ${formatWindow(policy)}`
    : labels.noPolicy ?? 'no policy';

  if (device.status === 'offline') {
    return {
      lastLabel: labels.lastSeen,
      last: agoLabel(device.last_seen, now),
      nextLabel: labels.nextDue,
      next: 'waits for link',
      intervalLabel: labels.interval,
      interval,
      nextTone: 'default',
    };
  }
  if (compliance && compliance.critical > 0) {
    return {
      lastLabel: labels.lastSeen,
      last: agoLabel(device.last_seen, now),
      nextLabel: labels.nextDue,
      next: `now · ${compliance.critical} critical`,
      intervalLabel: labels.interval,
      interval,
      nextTone: 'alert',
    };
  }
  if (compliance && compliance.failed > 0) {
    return {
      lastLabel: labels.lastSeen,
      last: agoLabel(device.last_seen, now),
      nextLabel: labels.nextDue,
      next: `retry · ${compliance.failed} failed`,
      intervalLabel: labels.interval,
      interval,
      nextTone: 'alert',
    };
  }
  if (compliance && compliance.detected > 0) {
    return {
      lastLabel: labels.lastSeen,
      last: agoLabel(device.last_seen, now),
      nextLabel: labels.nextDue,
      next: `${compliance.detected} update${compliance.detected === 1 ? '' : 's'}`,
      intervalLabel: labels.interval,
      interval,
      nextTone: 'due',
    };
  }
  if (compliance && compliance.compliant) {
    return {
      lastLabel: labels.lastSeen,
      last: agoLabel(device.last_seen, now),
      nextLabel: labels.nextDue,
      next: 'clear',
      intervalLabel: labels.interval,
      interval,
      nextTone: 'ok',
    };
  }
  return {
    lastLabel: labels.lastSeen,
    last: agoLabel(device.last_seen, now),
    nextLabel: labels.nextDue,
    next: 'not scanned',
    intervalLabel: labels.interval,
    interval,
    nextTone: 'default',
  };
}
