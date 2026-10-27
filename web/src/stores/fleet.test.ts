import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// 2.2-2: the point of the shared fleet model is measurable, and these are the
// measurements. A second subscriber must not add a request, and a collection
// nobody is looking at must not poll.
vi.mock('../api/devices', () => ({ getDevices: vi.fn() }));
vi.mock('../api/patching', () => ({
  getPatchOverview: vi.fn(),
  listPolicies: vi.fn(),
}));
vi.mock('../api/sessions', () => ({ listSessions: vi.fn() }));
vi.mock('../api/client', () => ({ isRateLimited: vi.fn(() => false) }));

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
import { isRateLimited } from '../api/client';
import { acquireFleet, fleetReads, invalidateFleet, resetFleetPoller, useFleetStore } from './fleet';
import { useFleet } from '../hooks/useFleet';
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
