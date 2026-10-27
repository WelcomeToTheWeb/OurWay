import { useCallback, useEffect } from 'react';
import { acquireFleet, useFleetStore, type FleetData, type FleetKey } from '../stores/fleet';

/**
 * How recent the data must be for an owning page's mount to skip the read.
 *
 * Three seconds is not a tuning knob: it is longer than React takes to flush a
 * page and the layout it nests (one commit), and shorter than any navigation a
 * person makes. So a page entering reads, and a second subscriber appearing in
 * the same mount does not.
 */
const OWNER_FRESH_MS = 3_000;

/**
 * Subscribe to one collection of the shared fleet read model (2.2-2).
 *
 * The component declares what it needs; the store decides whether a request is
 * in order. The first subscriber on a collection triggers the read and starts
 * its poller; every later subscriber reads the same state and joins the
 * in-flight read if one is on the wire. When the last subscriber unmounts, the
 * poller is cleared — a surface that is not open does not poll.
 */
export function useFleet<K extends FleetKey>(
  key: K,
  // A surface that is not allowed to see a collection must not subscribe to it:
  // a viewer's 403 is a wasted read and a console error, not a feature.
  options: {
    enabled?: boolean;
    /**
     * This surface *is* this collection — the sessions ledger on the sessions
     * page, the unit ledger on the devices page. The operator who opened it is
     * asking for the state as of now, so the mount reads unless the data
     * landed in the last few seconds — which is exactly the case of several
     * subscribers mounting in the same commit, and the reason a page and a
     * dashboard opened on the same fleet cost one read, not two.
     *
     * A collection that changed (a session started, a unit deleted) is
     * invalidated by the mutation that changed it, not by every page entering.
     */
    owner?: boolean;
  } = {},
): {
  data: FleetData[K];
  loading: boolean;
  error: string | null;
  /** Ask for a fresh read now (the operator pressed refresh). */
  refresh: () => void;
} {
  const { enabled = true, owner = false } = options;
  const data = useFleetStore((s) => s[key]);
  const loading = useFleetStore((s) => s.loading[key]);
  const error = useFleetStore((s) => s.error[key]);

  useEffect(
    () => (enabled ? acquireFleet(key, owner ? { freshMs: OWNER_FRESH_MS } : undefined) : undefined),
    [key, enabled, owner],
  );

  const refresh = useCallback(() => {
    if (!enabled) return;
    const release = acquireFleet(key, { force: true });
    release();
  }, [key, enabled]);

  return { data, loading, error: error ?? null, refresh };
}
