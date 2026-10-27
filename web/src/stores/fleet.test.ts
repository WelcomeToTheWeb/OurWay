import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// 2.2-2: the point of the shared fleet model is measurable, and these are the
// measurements. A second subscriber must not add a request, and a collection
// nobody is looking at must not poll.
vi.mock('../api/devices', () => ({ getDevices: vi.fn(), getAlerts: vi.fn() }));
vi.mock('../api/patching', () => ({
  getPatchOverview: vi.fn(),
  listPolicies: vi.fn(),
}));
vi.mock('../api/sessions', () => ({ listSessions: vi.fn() }));
vi.mock('../api/client', () => ({ isRateLimited: vi.fn(() => false) }));
vi.mock('../api/users', () => ({ getUsers: vi.fn() }));

import { getDevices } from '../api/devices';

/**
 * Fake timers and promise microtasks do not mix: `waitFor` polls on a real
 * timer that never runs once `useFakeTimers()` is in play. These reads resolve
 * in one microtask flush, so draining the queue is the honest wait.
 */
async function settled(times = 6) {
  for (let i = 0; i < times; i += 1) await Promise.resolve();
}
import { getPatchOverview, listPolicies } from '../api/patching';
import { listSessions } from '../api/sessions';
import { getAlerts } from '../api/devices';
import { isRateLimited } from '../api/client';
import { acquireFleet, fleetReads, invalidateFleet, resetFleetPoller, useFleetStore } from './fleet';
import { useFleet } from '../hooks/useFleet';
import { filterAlerts, isAlertOpen, openAlertCount } from '../utils/alerts';
import type { Alert } from '../types/alert';
import type { Device } from '../types/device';

const device = (name: string): Device => ({
  id: name,
  name,
  hostname: name,
  os: 'linux',
  arch: 'amd64',
  agent_version: '2.2.0',
  status: 'online',
  last_seen: new Date().toISOString(),
  public_ip: '',
  private_ip: '',
  created_at: '',
  updated_at: '',
});

const readsFor = (key: string) => fleetReads().filter((r) => r.key === key).length;

describe('fleet read model (2.2-2)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetFleetPoller();
    vi.useFakeTimers();
    vi.mocked(getDevices).mockResolvedValue([device('a'), device('b')]);
    vi.mocked(getPatchOverview).mockResolvedValue({ devices: [], totals: undefined } as never);
    vi.mocked(listPolicies).mockResolvedValue([]);
    vi.mocked(listSessions).mockResolvedValue([]);
    vi.mocked(getAlerts).mockResolvedValue([]);
    vi.mocked(isRateLimited).mockReturnValue(false);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('serves two subscribers with one list read', async () => {
    const releaseA = acquireFleet('devices');
    await settled();

    // A subscriber arriving after the list landed reads the state: the
    // collection is fresh, so it issues nothing.
    const releaseB = acquireFleet('devices');
    expect(readsFor('devices')).toBe(1);
    expect(useFleetStore.getState().devices).toHaveLength(2);
    releaseA();
    releaseB();
  });

  it('joins a read that is already on the wire', async () => {
    const first = acquireFleet('devices');
    const second = acquireFleet('devices');
    const third = acquireFleet('devices');

    await settled();
    expect(readsFor('devices')).toBe(1);
    releaseC(first, second, third);
  });

  it('reads nothing when pages mount on a collection at the same moment', async () => {
    // React flushes a page and the layout it nests in one commit, so several
    // subscribers for one collection arrive together. The in-flight join is
    // what makes that one request.
    const a = renderHook(() => useFleet('sessions'));
    const b = renderHook(() => useFleet('sessions'));
    await settled();
    expect(listSessions).toHaveBeenCalledTimes(1);

    const c = renderHook(() => useFleet('sessions'));
    await settled();
    expect(listSessions).toHaveBeenCalledTimes(1);

    act(() => {
      a.unmount();
      b.unmount();
      c.unmount();
    });
  });

  it('re-reads for a surface that owns the collection, once per entry', async () => {
    // The sessions ledger on the sessions page is the operator asking for the
    // state as of now. Two pages mounting in the same commit are one entry.
    const ownerA = renderHook(() => useFleet('patchOverview', { owner: true }));
    const ownerB = renderHook(() => useFleet('patchOverview', { owner: true }));
    await settled();
    expect(getPatchOverview).toHaveBeenCalledTimes(1);

    act(() => {
      ownerA.unmount();
      ownerB.unmount();
    });

    // A later entry — the operator moving from the dashboard to this ledger —
    // is a new question, and reads. The data is no longer "as of now".
    invalidateFleet('patchOverview');
    const ownerC = renderHook(() => useFleet('patchOverview', { owner: true }));
    await settled();
    expect(getPatchOverview).toHaveBeenCalledTimes(2);
    act(() => ownerC.unmount());
  });

  it('re-reads a collection a mutation invalidated', async () => {
    const ledger = renderHook(() => useFleet('sessions', { owner: true }));
    await settled();
    expect(listSessions).toHaveBeenCalledTimes(1);

    invalidateFleet('sessions');
    await settled();
    expect(listSessions).toHaveBeenCalledTimes(2);

    act(() => ledger.unmount());
  });

  it('stops polling when the last subscriber leaves', async () => {
    const releaseA = acquireFleet('devices');
    const releaseB = acquireFleet('devices');
    await settled();

    // A subscriber leaving while another remains keeps the poller alive.
    releaseA();
    vi.advanceTimersByTime(15_000);
    await Promise.resolve();
    expect(readsFor('devices')).toBe(2);

    releaseB();
    vi.advanceTimersByTime(120_000);
    await Promise.resolve();
    expect(readsFor('devices')).toBe(2);
  });

  it('does not re-read a collection that is already fresh', async () => {
    const release = acquireFleet('devices');
    await settled();

    const joiner = acquireFleet('devices');
    expect(readsFor('devices')).toBe(1);

    // An explicit refresh is the operator asking, and costs exactly one read.
    const force = acquireFleet('devices', { force: true });
    force();
    release();
    joiner();
    await settled();
  });

  it('rests during the server rate-limit back-off', async () => {
    const release = acquireFleet('devices');
    await settled();

    vi.mocked(isRateLimited).mockReturnValue(true);
    vi.advanceTimersByTime(90_000);
    release();
    await Promise.resolve();
    expect(readsFor('devices')).toBe(1);
  });

  it('gives a hook subscriber the shared state without its own poller', async () => {
    const one = renderHook(() => useFleet('patchOverview'));
    await settled();
    expect(getPatchOverview).toHaveBeenCalledTimes(1);

    const two = renderHook(() => useFleet('patchOverview'));
    await settled();
    expect(getPatchOverview).toHaveBeenCalledTimes(1);
    expect(two.result.current.loading).toBe(false);
    expect(two.result.current.data).toBe(one.result.current.data);

    act(() => {
      one.unmount();
      two.unmount();
      vi.advanceTimersByTime(120_000);
    });
    expect(getPatchOverview).toHaveBeenCalledTimes(1);
  });

  it('the alert collection is one read for the ledger and the dashboard tile', async () => {
    // The count drift 2.2-4 closes lived in the counts, not in the reads — but
    // the fix is the shared collection, so the read model has to hold for
    // alerts exactly as it holds for units.
    const ledger = renderHook(() => useFleet('alerts', { owner: true }));
    const dashboard = acquireFleet('alerts');
    await settled();
    expect(getAlerts).toHaveBeenCalledTimes(1);
    ledger.unmount();
    dashboard();
  });

  it('acknowledging is open: one count, acknowledged included', () => {
    // Three surfaces called "open alerts" three populations. The shared
    // definition: open is unresolved, and a sign-off does not close a signal.
    const alerts = [
      alertRow('a', { acknowledged: false, resolved: false }),
      alertRow('b', { acknowledged: true, resolved: false }),
      alertRow('c', { acknowledged: true, resolved: true }),
    ];
    expect(openAlertCount(alerts)).toBe(2);
    expect(alerts.filter(isAlertOpen).map((a) => a.id)).toEqual(['a', 'b']);
  });

  it('the chips count the same array the rows render', () => {
    // The ledger's old failure: chips counted the server-filtered page while
    // the dashboard tile counted the fleet. Filters are client-side over one
    // collection, so a chip and its rows are the same predicate.
    const alerts = [
      alertRow('a', { severity: 'critical' }),
      alertRow('b', { severity: 'warning' }),
      alertRow('c', { severity: 'critical', resolved: true }),
      alertRow('d', { severity: 'critical', message: 'disk full on E2E-SERVER-01' }),
    ];
    const all = filterAlerts(alerts, {});
    const critical = filterAlerts(alerts, { severity: 'critical' });
    const searched = filterAlerts(alerts, { search: 'disk full on E2E-SERVER' });
    expect(all).toHaveLength(4);
    expect(critical).toHaveLength(3);
    expect(critical.filter(isAlertOpen)).toHaveLength(2);
    expect(searched).toHaveLength(1);
    // A status chip narrows the rows and the chips measure the searched set,
    // so switching a chip moves the rows without moving what is measured.
    expect(filterAlerts(searched, { status: 'unresolved' })).toHaveLength(1);
  });

  it('does not subscribe for a collection the role may not read', async () => {
    const viewer = renderHook(() => useFleet('patchOverview', { enabled: false }));
    act(() => viewer.unmount());
    vi.advanceTimersByTime(120_000);
    expect(getPatchOverview).not.toHaveBeenCalled();
  });
});

/** Release a batch of subscriptions (test sugar: releases are plain functions). */
function releaseC(...releases: (() => void)[]) {
  for (const release of releases) release();
}

function alertRow(id: string, over: Partial<Alert> = {}): Alert {
  return {
    id,
    device_id: 'dev-' + id,
    device_name: id.toUpperCase(),
    metric: 'disk',
    severity: 'critical',
    value: 95,
    threshold: 90,
    message: `${id} is full`,
    resolved: false,
    acknowledged: false,
    created_at: new Date().toISOString(),
    ...over,
  } as Alert;
}
