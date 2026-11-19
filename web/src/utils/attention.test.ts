import { describe, it, expect } from 'vitest';
import { buildAttentionList, osBreakdown } from './attention';
import type { Device } from '../types/device';
import type { Alert } from '../types/alert';
import type { PatchOverview } from '../types/patch';

const mk = (name: string, o: Partial<Device> = {}): Device => ({
  id: name, name, hostname: name, os: 'linux', arch: 'amd64', agent_version: '1', status: 'online',
  last_seen: '', public_ip: '', private_ip: '', created_at: '', updated_at: '', ...o,
});
const row = (id: string, o: object) => ({ device_id: id, name: id, os: 'linux', status: 'online', detected: 0, approved: 0, installing: 0, failed: 0, critical: 0, reboot_pending: false, compliant: true, ...o });
const alertRow = (device_id: string, o: Partial<Alert> = {}): Alert => ({
  id: `${device_id}-a`, device_id, device_name: device_id, severity: 'critical', message: '', metric: 'cpu',
  value: 0, threshold: 0, resolved: false, acknowledged: false, created_at: '2026-10-04T10:00:00Z', ...o,
});

describe('buildAttentionList', () => {
  // "In alert" comes from the alerts collection, so the fixture carries one
  // open alert for `alerting` and a resolved one for `ok`: the resolved row is
  // there to prove that what counts is *open*, not "has ever fired".
  const devices = [mk('ok'), mk('alerting'), mk('off', { status: 'offline' }), mk('crit'), mk('reboot', { reboot_pending: true })];
  const alerts = [alertRow('alerting'), alertRow('ok', { resolved: true, severity: 'warning', metric: 'ram' })];
  const overview = { totals: { devices: 5, compliant: 3, pending: 3, critical: 2, reboot_pending: 1 }, devices: [row('crit', { critical: 2, compliant: false })] } as PatchOverview;

  it('omits healthy devices and orders by urgency', () => {
    const names = buildAttentionList(devices, overview, alerts).map((i) => i.device.name);
    expect(names).toEqual(['alerting', 'crit', 'off', 'reboot']);
  });

  it('explains why, pluralizing counts', () => {
    const crit = buildAttentionList(devices, overview, alerts).find((i) => i.device.name === 'crit')!;
    expect(crit.reasons).toEqual(['2 critical updates']);
  });

  it('ranks a unit by its worst open alert and gives it one reason', () => {
    // Severity decides; a tie reads newest-first. Three open signals must still
    // yield a single reason, or the "Why" column repeats itself.
    const fleet = [
      alertRow('db-1', { id: 'a1', severity: 'info', metric: 'ram', created_at: '2026-10-09T12:00:00Z' }),
      alertRow('db-1', { id: 'a2', severity: 'critical', metric: 'disk', created_at: '2026-10-01T12:00:00Z' }),
      alertRow('db-1', { id: 'a3', severity: 'critical', metric: 'cpu', created_at: '2026-10-09T12:00:00Z' }),
    ];
    const [item] = buildAttentionList([mk('db-1')], null, fleet);
    expect(item.rank).toBe(0);
    expect(item.reasons).toEqual(['cpu critical']);
  });

  it('works without patch data and honours the limit', () => {
    expect(buildAttentionList(devices, null, alerts, 2)).toHaveLength(2);
    expect(buildAttentionList([mk('ok')], null, [])).toEqual([]);
  });
});

describe('osBreakdown', () => {
  it('counts per OS, largest first', () => {
    const r = osBreakdown([mk('a'), mk('b', { os: 'windows' }), mk('c', { os: 'windows' })]);
    expect(r).toEqual([{ os: 'windows', count: 2 }, { os: 'linux', count: 1 }]);
  });
});
