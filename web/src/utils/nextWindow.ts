import type { PatchPolicy } from '../types/patch';

/**
 * When the shop next works on the fleet: the earliest upcoming policy
 * maintenance window. Honest about policy schedule, never invented.
 */
export function nextWindow(
  policies: PatchPolicy[],
  now: Date = new Date(),
): { at: Date; policy: PatchPolicy } | null {
  let best: { at: Date; policy: PatchPolicy } | null = null;
  for (const p of policies) {
    const at = nextOccurrence(p, now);
    if (!at) continue;
    if (!best || at < best.at) best = { at, policy: p };
  }
  return best;
}

function nextOccurrence(p: PatchPolicy, now: Date): Date | null {
  const [hRaw, mRaw] = (p.window_start || '00:00').split(':');
  const hour = parseInt(hRaw, 10);
  const minute = parseInt(mRaw, 10);
  if (Number.isNaN(hour) || Number.isNaN(minute)) return null;

  const candidate = new Date(now);
  candidate.setUTCHours(hour, minute, 0, 0);

  switch (p.schedule) {
    case 'daily':
      if (candidate <= now) candidate.setUTCDate(candidate.getUTCDate() + 1);
      return candidate;
    case 'weekly': {
      const base = p.last_run_at ? new Date(p.last_run_at) : now;
      for (let i = 0; i < 8; i += 1) {
        const c = new Date(candidate);
        c.setUTCDate(candidate.getUTCDate() + i * 7);
        if (c > now && c > base) return c;
      }
      return new Date(candidate.setUTCDate(candidate.getUTCDate() + 7));
    }
    case 'monthly': {
      const c = new Date(candidate);
      if (c <= now) c.setUTCMonth(c.getUTCMonth() + 1);
      return c;
    }
    default:
      return candidate > now ? candidate : null;
  }
}

/** "in 3h 12m" / "in 4d" — how far off the window is. */
export function untilLabel(at: Date, now: Date = new Date()): string {
  const mins = Math.round((at.getTime() - now.getTime()) / 60000);
  if (mins <= 0) return 'now';
  if (mins < 60) return `in ${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `in ${hours}h ${mins % 60}m`;
  const days = Math.floor(hours / 24);
  return `in ${days}d ${hours % 24}h`;
}
