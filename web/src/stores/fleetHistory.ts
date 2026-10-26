import { create } from 'zustand';

/**
 * A session-sized history of the fleet's headline figures.
 *
 * The console has no server-side series for "how many units were online
 * an hour ago", so it keeps its own: every sample is a truth the operator
 * watched happen. The strip under a headline number is this session, and
 * the caption says so.
 */
export interface FleetSample {
  t: number;
  online: number;
  offline: number;
  alerts: number;
  critical: number;
  rebootPending: number;
  compliant: number;
  pending: number;
}

const MAX_SAMPLES = 96;

interface FleetHistoryState {
  samples: FleetSample[];
  push: (sample: FleetSample) => void;
  reset: () => void;
}

export const useFleetHistory = create<FleetHistoryState>((set) => ({
  samples: [],
  push: (sample) =>
    set((state) => {
      const last = state.samples[state.samples.length - 1];
      if (
        last &&
        last.online === sample.online &&
        last.alerts === sample.alerts &&
        last.critical === sample.critical &&
        last.pending === sample.pending
      ) {
        // Nothing moved; keep the newest timestamp only.
        return { samples: [...state.samples.slice(0, -1), { ...sample }] };
      }
      return { samples: [...state.samples, sample].slice(-MAX_SAMPLES) };
    }),
  reset: () => set({ samples: [] }),
}));

/** Index where a series changed state — the onset a micro-field marks. */
export function onsetIndex(values: number[]): number | undefined {
  if (values.length < 2) return undefined;
  const now = values[values.length - 1];
  for (let i = values.length - 2; i >= 0; i -= 1) {
    if (values[i] !== now) return i + 1;
  }
  return undefined;
}
