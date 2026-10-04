import type { PatchPolicy } from '../types/patch';

/** "02:00–06:00 UTC" for a policy's maintenance window ("Any time" when unset). */
export function formatWindow(p: Pick<PatchPolicy, 'window_start' | 'window_hours' | 'timezone'>): string {
  const tz = p.timezone || 'UTC';
  if (!p.window_start && !p.window_hours) return `Any time (${tz})`;
  const start = p.window_start || '00:00';
  const hours = p.window_hours && p.window_hours > 0 && p.window_hours <= 24 ? p.window_hours : 24;
  const [h, m] = start.split(':').map((n) => parseInt(n, 10));
  if (Number.isNaN(h) || Number.isNaN(m)) return `Any time (${tz})`;
  const endMinutes = (h * 60 + m + hours * 60) % (24 * 60);
  const end = `${String(Math.floor(endMinutes / 60)).padStart(2, '0')}:${String(endMinutes % 60).padStart(2, '0')}`;
  return hours === 24 ? `All day (${tz})` : `${start}–${end} ${tz}`;
}

/** Time zones the browser knows, falling back to a short common list. */
export function timeZoneOptions(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] };
  try {
    const all = intl.supportedValuesOf?.('timeZone');
    if (all && all.length > 0) return ['UTC', ...all.filter((z) => z !== 'UTC')];
  } catch {
    /* older browsers */
  }
  return ['UTC', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'Europe/London', 'Europe/Berlin', 'Asia/Tokyo', 'Australia/Sydney'];
}

export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}
