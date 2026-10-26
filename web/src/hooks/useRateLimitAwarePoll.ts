import { useEffect, useRef } from 'react';
import { isRateLimited } from '../api/client';

/**
 * Rate-limit-aware polling.
 *
 * The server allows each user 100 requests per minute with a burst of 10.
 * Every page here polls on its own timer, so a technician with a few tabs
 * open can spend the whole budget on polls and start seeing 429s. When a 429
 * happens, the API client records a back-off window; a poll that fires
 * inside that window is skipped rather than sent, and each timer is stretched
 * so a page sitting open does not keep firing during the back-off.
 *
 * Pass the interval in milliseconds. The callback runs on mount immediately,
 * exactly like the plain setInterval it replaces.
 */
export function useRateLimitAwarePoll(ms: number, fn: () => void | Promise<void>) {
  const callback = useRef(fn);
  callback.current = fn;

  useEffect(() => {
    let timer = 0;

    const tick = async () => {
      if (isRateLimited()) {
        // Spend the minute resting, not hammering the limiter.
        timer = window.setTimeout(tick, ms);
        return;
      }
      await callback.current();
      timer = window.setTimeout(tick, isRateLimited() ? ms * 2 : ms);
    };

    void tick();
    return () => window.clearTimeout(timer);
  }, [ms]);
}
