import { describe, it, expect } from 'vitest';
import {
  clientNameForSlug,
  clientOptions,
  clientSlugFromSearch,
  clientSlugOfDevice,
  deviceIdsForClient,
  filterDevicesByClient,
  scopeCount,
  scopeLabel,
  UNASSIGNED_SLUG,
  withClientSlug,
} from './clientFilter';
import type { Client } from '../api/clients';
import type { Device } from '../types/device';

const mk = (name: string, client_id?: string | null): Device =>
  ({
    id: name,
    name,
    hostname: `${name}.lan`,
    os: 'linux',
    arch: 'amd64',
    agent_version: '1',
    status: 'online',
    last_seen: new Date().toISOString(),
    public_ip: '',
    private_ip: '',
    created_at: '',
    updated_at: '',
    tags: [],
    client_id,
  }) as Device;

const clients: Client[] = [
  { id: 'c-alpha', name: 'Alpha Co', slug: 'alpha-co', notes: '', disabled: false, created_at: '', updated_at: '' },
  { id: 'c-beta', name: 'Beta Co', slug: 'beta-co', notes: '', disabled: false, created_at: '', updated_at: '' },
];

describe('client filter helpers (2.3-5)', () => {
  it('names a unit with no tenant by the sentinel, the same word the server uses', () => {
    expect(clientSlugOfDevice(mk('free'), clients)).toBe(UNASSIGNED_SLUG);
    expect(clientSlugOfDevice(mk('x', null), clients)).toBe(UNASSIGNED_SLUG);
    // A unit's tenant is a slug in the console, never the wire's id.
    expect(clientSlugOfDevice(mk('x', 'c-alpha'), clients)).toBe('alpha-co');
    // A tenant this caller cannot name resolves to the sentinel, which is the
    // answer a filter can act on: that unit is not in your view.
    expect(clientSlugOfDevice(mk('x', 'c-who'), clients)).toBe(UNASSIGNED_SLUG);
    expect(clientNameForSlug(clients, UNASSIGNED_SLUG)).toBe('Unassigned');
    expect(clientNameForSlug(clients, 'beta-co')).toBe('Beta Co');
  });

  it('offers Unassigned only to the caller who can see those units', () => {
    const scoped = clientOptions(clients, false).map((o) => o.slug);
    expect(scoped).toEqual(['alpha-co', 'beta-co']);
    const admin = clientOptions(clients, true).map((o) => o.slug);
    expect(admin[admin.length - 1]).toBe(UNASSIGNED_SLUG);
  });

  it('writes the tenant into the URL without disturbing the other filters', () => {
    const params = new URLSearchParams('status=alert&sort=last-seen');
    const next = withClientSlug(params, 'alpha-co');
    expect(next.get('client')).toBe('alpha-co');
    expect(next.get('status')).toBe('alert');
    expect(next.get('sort')).toBe('last-seen');
    // Clearing the filter leaves the rest of the view intact.
    expect(withClientSlug(next, '').get('client')).toBeNull();
    expect(clientSlugFromSearch(next)).toBe('alpha-co');
  });

  it('filters a fleet to one tenant through the same mapping every ledger uses', () => {
    const fleet = [mk('a1', 'c-alpha'), mk('a2', 'c-alpha'), mk('b1', 'c-beta'), mk('free')];
    expect(filterDevicesByClient(fleet, '').length).toBe(4);
    expect(filterDevicesByClient(fleet, 'alpha-co', clients).map((d) => d.name)).toEqual(['a1', 'a2']);
    expect(filterDevicesByClient(fleet, UNASSIGNED_SLUG, clients).map((d) => d.name)).toEqual(['free']);
    expect([...deviceIdsForClient(fleet, 'beta-co', clients)]).toEqual(['b1']);
  });

  it('reads the scope header as an operator needs to read it', () => {
    expect(scopeLabel('all', clients)).toBe('all tenants');
    expect(scopeLabel('none', clients)).toBe('no tenants');
    expect(scopeLabel('c-beta,c-alpha', clients)).toBe('Beta Co, Alpha Co');
    // Ids the caller cannot name stay ids: the header is the server's statement,
    // and the console does not invent a tenant to explain it.
    expect(scopeLabel('unknown-id', clients)).toBe('unknown-id');
    expect(scopeCount('all')).toBe(-1);
    expect(scopeCount('c-alpha,c-beta')).toBe(2);
    expect(scopeCount('none')).toBe(0);
  });
});
