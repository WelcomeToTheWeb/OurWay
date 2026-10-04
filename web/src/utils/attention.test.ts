import { describe, it, expect } from 'vitest';
import { buildAttentionList, osBreakdown } from './attention';
import type { Device } from '../types/device';
import type { PatchOverview } from '../types/patch';

const mk = (name: string, o: Partial<Device> = {}): Device => ({
  id: name, name, hostname: name, os: 'linux', arch: 'amd64', agent_version: '1', status: 'online',
  last_seen: '', public_ip: '', private_ip: '', created_at: '', updated_at: '', ...o,
});
const row = (id: string, o: object) => ({ device_id: id, name: id, os: 'linux', status: 'online', detected: 0, approved: 0, installing: 0, failed: 0, critical: 0, reboot_pending: false, compliant: true, ...o });

describe('buildAttentionList', () => {
  const devices = [mk('ok'), mk('alerting', { status: 'alert' }), mk('off', { status: 'offline' }), mk('crit'), mk('reboot', { reboot_pending: true })];
  const overview = { totals: { devices: 5, compliant: 3, pending: 3, critical: 2, reboot_pending: 1 }, devices: [row('crit', { critical: 2, compliant: false })] } as PatchOverview;

  it('omits healthy devices and orders by urgency', () => {
    const names = buildAttentionList(devices, overview).map((i) => i.device.name);
    expect(names).toEqual(['alerting', 'crit', 'off', 'reboot']);
  });

  it('explains why, pluralizing counts', () => {
    const crit = buildAttentionList(devices, overview).find((i) => i.device.name === 'crit')!;
    expect(crit.reasons).toEqual(['2 critical updates']);
  });

  it('works without patch data and honours the limit', () => {
    expect(buildAttentionList(devices, null, 2)).toHaveLength(2);
    expect(buildAttentionList([mk('ok')], null)).toEqual([]);
  });
});

describe('osBreakdown', () => {
  it('counts per OS, largest first', () => {
    const r = osBreakdown([mk('a'), mk('b', { os: 'windows' }), mk('c', { os: 'windows' })]);
    expect(r).toEqual([{ os: 'windows', count: 2 }, { os: 'linux', count: 1 }]);
  });
});
