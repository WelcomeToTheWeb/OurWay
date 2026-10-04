import { describe, it, expect } from 'vitest';
import { formatWindow } from './patchWindow';

describe('formatWindow', () => {
  it('formats a normal window', () => {
    expect(formatWindow({ window_start: '02:00', window_hours: 4, timezone: 'UTC' })).toBe('02:00–06:00 UTC');
  });
  it('wraps past midnight', () => {
    expect(formatWindow({ window_start: '22:30', window_hours: 6, timezone: 'America/New_York' })).toBe('22:30–04:30 America/New_York');
  });
  it('treats 24h and unset as open-ended', () => {
    expect(formatWindow({ window_start: '00:00', window_hours: 24, timezone: 'UTC' })).toBe('All day (UTC)');
    expect(formatWindow({ window_start: '', window_hours: 0, timezone: '' })).toBe('Any time (UTC)');
  });
});
