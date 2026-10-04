import { describe, it, expect } from 'vitest';
import { applyDeviceFilters, collectTags, defaultFilters } from './deviceList';
import type { Device } from '../types/device';

const mk = (o: Partial<Device>): Device => ({
  id: o.name ?? 'x', name: 'x', hostname: 'h', os: 'linux', arch: 'amd64', agent_version: '1',
  status: 'online', last_seen: '2026-10-04T10:00:00Z', public_ip: '', private_ip: '10.0.0.1',
  created_at: '', updated_at: '', ...o,
});

const devices = [
  mk({ name: 'web-10', status: 'online', os: 'linux', tags: ['prod', 'web'] }),
  mk({ name: 'web-2', status: 'offline', os: 'windows', tags: ['prod'], last_seen: '2026-10-01T10:00:00Z' }),
  mk({ name: 'db-1', status: 'alert', os: 'linux', private_ip: '10.0.9.9', tags: [] }),
];

describe('applyDeviceFilters', () => {
  it('sorts names naturally (web-2 before web-10)', () => {
    const names = applyDeviceFilters(devices, defaultFilters, 'name').map((d) => d.name);
    expect(names).toEqual(['db-1', 'web-2', 'web-10']);
  });

  it('puts alerts, then offline, then online when sorting by status', () => {
    const names = applyDeviceFilters(devices, defaultFilters, 'status').map((d) => d.name);
    expect(names).toEqual(['db-1', 'web-2', 'web-10']);
  });

  it('filters by status, os and tag together', () => {
    expect(applyDeviceFilters(devices, { ...defaultFilters, os: 'linux', tag: 'prod' }, 'name').map((d) => d.name)).toEqual(['web-10']);
    expect(applyDeviceFilters(devices, { ...defaultFilters, status: 'offline' }, 'name')).toHaveLength(1);
  });

  it('searches name, ip and tags, case-insensitively', () => {
    expect(applyDeviceFilters(devices, { ...defaultFilters, search: 'WEB' }, 'name')).toHaveLength(2);
    expect(applyDeviceFilters(devices, { ...defaultFilters, search: '10.0.9' }, 'name').map((d) => d.name)).toEqual(['db-1']);
  });

  it('reverses and does not mutate the input', () => {
    const copy = [...devices];
    const names = applyDeviceFilters(devices, defaultFilters, 'last_seen', true).map((d) => d.name);
    expect(names[0]).not.toBe('web-2');
    expect(devices).toEqual(copy);
  });
});

describe('collectTags', () => {
  it('returns distinct sorted tags and tolerates missing tags', () => {
    expect(collectTags(devices)).toEqual(['prod', 'web']);
  });
});
