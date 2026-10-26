import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { getDevices } from '../api/devices';
import { getPatchOverview, listPolicies } from '../api/patching';
import { listSessions, type Session } from '../api/sessions';
import { isRateLimited } from '../api/client';
import type { Device, Metrics } from '../types/device';
import type { PatchOverview, PatchPolicy } from '../types/patch';

/**
 * The shared fleet read model (2.2-2).
 *
 * Before this, every page owned its own poller: `useDevices` fetched the list
 * on mount, `Layout` fetched the policies, Dashboard fetched the overview and
 * sessions, DeviceDetail fetched the policies *again*. Four subscribers to the
 * same four collections meant four pollers, and the finish review measured a
 * dashboard mount at 14 reads with a 429 on arrival
 * (`.impeccable/review/verdict.md`, debt #1).
 *
 * Here the collections live once, each with **one** poller whose lifetime is
 * ref-counted by its subscribers. A second page subscribing to a collection
 * that is already polled adds no request: it reads the same state and joins the
 * same in-flight read. When the last subscriber leaves, the timer is cleared —
 * no background polling on a surface that is not open.
 *
 * The client's read queue (`api/client.ts`) and its 429 back-off stay what they
 * were: the back-stop, not the mechanism. A tick that fires inside a back-off
 * window is skipped, exactly like `useRateLimitAwarePoll` did per page.
 */
export type FleetKey = 'devices' | 'patchOverview' | 'patchPolicies' | 'sessions';

export interface FleetData {
  devices: Device[];
  patchOverview: PatchOverview | null;
  patchPolicies: PatchPolicy[];
  sessions: Session[];
}

interface FleetState extends FleetData {
  /** When each collection last landed, 0 when never. */
  fetchedAt: Record<FleetKey, number>;
  loading: Record<FleetKey, boolean>;
  error: Partial<Record<FleetKey, string>>;
  /** The device whose 2 s stream this session turned on (per-detail). */
  streamingDeviceId: string | null;

  setCollection: <K extends FleetKey>(key: K, data: FleetData[K]) => void;
  setFleetError: (key: FleetKey, message: string | null) => void;
  setStreaming: (deviceId: string | null) => void;
  /** Live status from the device WebSocket: cheaper than re-reading the list. */
  applyDeviceStatus: (deviceId: string, status: Device['status']) => void;
  resetFleet: () => void;
}

/**
 * Poll and staleness budgets, per collection.
 *
 * They differ on purpose: a unit's status is worth 15 s, patch compliance
 * moves on the scale of hours, and a policy list only changes when an operator
 * edits it. The numbers are the mechanism the acceptance criteria measure —
 * the mount budget, not a vibe.
 */
const POLL_MS: Record<FleetKey, number> = {
  devices: 15_000,
  patchOverview: 60_000,
  patchPolicies: 5 * 60_000,
  sessions: 30_000,
};

/** A collection fresher than this is not re-read when a page subscribes. */
const FRESH_MS: Record<FleetKey, number> = {
  devices: 10_000,
  patchOverview: 30_000,
  patchPolicies: 5 * 60_000,
  sessions: 15_000,
};

const collectors: Record<FleetKey, () => Promise<unknown>> = {
  devices: () => getDevices(),
  patchOverview: () => getPatchOverview().catch(() => null),
  patchPolicies: () => listPolicies().catch(() => [] as PatchPolicy[]),
  sessions: () => listSessions().catch(() => [] as Session[]),
};

const emptyFlags = { devices: false, patchOverview: false, patchPolicies: false, sessions: false };
const emptyStamps: Record<FleetKey, number> = {
  devices: 0,
  patchOverview: 0,
  patchPolicies: 0,
  sessions: 0,
};

export const useFleetStore = create<FleetState>()(
  persist(
    (set) => ({
      devices: [],
      patchOverview: null,
      patchPolicies: [],
      sessions: [],
      fetchedAt: { ...emptyStamps },
      loading: { ...emptyFlags },
      error: {},
      streamingDeviceId: null,

      setCollection: (key, data) =>
        set((state) => ({
          [key]: data,
          fetchedAt: { ...state.fetchedAt, [key]: Date.now() },
          loading: { ...state.loading, [key]: false },
          error: { ...state.error, [key]: undefined },
        }) as Partial<FleetState>),

      setFleetError: (key, message) =>
        set((state) => ({
          error: { ...state.error, [key]: message ?? undefined },
          loading: { ...state.loading, [key]: false },
        }) as Partial<FleetState>),

      setStreaming: (deviceId) => set({ streamingDeviceId: deviceId }),

      applyDeviceStatus: (deviceId, status) =>
        set((state) => ({
          devices: state.devices.map((d) =>
            d.id === deviceId ? { ...d, status, last_seen: new Date().toISOString() } : d,
          ),
        })),

      resetFleet: () =>
        set({
          devices: [],
          patchOverview: null,
          patchPolicies: [],
          sessions: [],
          fetchedAt: { ...emptyStamps },
          loading: { ...emptyFlags },
          error: {},
          streamingDeviceId: null,
        }),
    }),
    {
      // The unit list is the one collection worth keeping across a reload: the
      // console paints the fleet immediately and the poller refreshes it.
      //
      // Only the list — and deliberately not the `fetchedAt` stamps. A stamp
      // that outlives its data is the worst of both: the reloaded ledger would
      // answer "no sessions" for three seconds because the store believed a
      // read it no longer had was recent. Freshness lives with the data.
      name: 'ourway-fleet',
      partialize: (state) => ({ devices: state.devices }) as Partial<FleetState>,
    }
  )
);

// ── Poller (module scope: one per collection, not one per component) ────────

const subscribers = new Map<FleetKey, number>();
const inflight = new Map<FleetKey, Promise<void>>();
const timers = new Map<FleetKey, number>();
/** Test hook: counts every read the poller actually issued. */
const readLog: { key: FleetKey; at: number }[] = [];

async function run(key: FleetKey): Promise<void> {
  const existing = inflight.get(key);
  if (existing) {
    // A second caller joins the read that is already on the wire. This is the
    // whole trick: subscribers never stack requests.
    return existing;
  }
  // The collection is already fresh: nothing to read. Two pages mounting at
  // once (React's parallel effect flush) must cost one request, not two.
  if (Date.now() - useFleetStore.getState().fetchedAt[key] < FRESH_MS[key]) return Promise.resolve();

  useFleetStore.setState((state) => ({ loading: { ...state.loading, [key]: true } }) as Partial<FleetState>);

  // The collector is invoked synchronously: a second subscriber arriving in
  // the same tick must join this read, not watch the map while a microtask
  // empties it. That is the whole claim of 2.2-2, measured.
  const run$ = (async () => {
    readLog.push({ key, at: Date.now() });
    try {
      const data = await collectors[key]();
      useFleetStore.getState().setCollection(key, data as never);
    } catch (err) {
      useFleetStore.getState().setFleetError(key, (err as Error).message);
    } finally {
      inflight.delete(key);
    }
  })();
  inflight.set(key, run$);

  return run$;
}

function schedule(key: FleetKey) {
  if (timers.has(key)) return;
  const id = window.setInterval(() => {
    if (isRateLimited()) return; // rest during the server's back-off
    void run(key);
  }, POLL_MS[key]);
  timers.set(key, id);
}

function unschedule(key: FleetKey) {
  const id = timers.get(key);
  if (id !== undefined) window.clearInterval(id);
  timers.delete(key);
}

/**
 * Subscribe to a fleet collection.
 *
 * Returns the release function. The first subscriber triggers the read;
 * later subscribers get the state, and the in-flight read if there is one.
 * `force` bypasses the freshness window (an operator pressed refresh).
 */
export function acquireFleet(
  key: FleetKey,
  opts: { force?: boolean; freshMs?: number } = {},
): () => void {
  subscribers.set(key, (subscribers.get(key) ?? 0) + 1);
  schedule(key);

  const { fetchedAt } = useFleetStore.getState();
  const freshMs = opts.freshMs ?? FRESH_MS[key];
  const fresh = Date.now() - fetchedAt[key] < freshMs;
  if (opts.force || !fresh) {
    if (!isRateLimited() || opts.force) void run(key);
  }

  return () => {
    const left = (subscribers.get(key) ?? 1) - 1;
    subscribers.set(key, Math.max(left, 0));
    if (left <= 0) unschedule(key);
  };
}

/**
 * Mark a collection stale because something on it changed (a session ended,
 * a unit was deleted, the operator pressed reload on a panel). Subscribers
 * re-read on their next tick; a page that subscribes after this point reads
 * immediately, because its stamp says the data is old.
 */
export function invalidateFleet(key: FleetKey): void {
  useFleetStore.setState((state) => ({ fetchedAt: { ...state.fetchedAt, [key]: 0 } }) as Partial<FleetState>);
  if ((subscribers.get(key) ?? 0) > 0) void run(key);
}

/** Number of live subscribers (diagnostics, tests). */
export const fleetSubscribers = (key: FleetKey): number => subscribers.get(key) ?? 0;
/** Reads the poller issued, newest last (diagnostics, tests). */
export const fleetReads = (): readonly { key: FleetKey; at: number }[] => readLog;
/** Drop every poller, subscriber and cached collection (tests). */
export function resetFleetPoller() {
  for (const key of [...timers.keys()]) unschedule(key);
  subscribers.clear();
  inflight.clear();
  readLog.length = 0;
  useFleetStore.getState().resetFleet();
}

// Metric history stays per device (the detail page's own concern) and lives in
// stores/metrics.ts; fleetHistory.ts is the dashboard's own session-long
// samples. Both keep their shape — 2.2-2 is about the list reads.
export type { Metrics };
