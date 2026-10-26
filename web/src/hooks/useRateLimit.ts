import { useEffect, useState } from 'react';
import { RATE_LIMIT_BACKOFF_MS, isRateLimited, onRateLimitChange, rateLimitedUntil } from '../api/client';

/**
 * Live view of the server rate-limit back-off, so the console can say what
 * it is doing when the limiter pushes back instead of showing a bare 429.
 */
export function useRateLimit(): { limited: boolean; secondsLeft: number } {
  const [, bump] = useState(0);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const off = onRateLimitChange(() => bump((n) => n + 1));
    const id = window.setInterval(() => {
      if (isRateLimited()) setTick((n) => n + 1);
    }, 1000);
    return () => {
      off();
      window.clearInterval(id);
    };
  }, []);

  void tick;
  const secondsLeft = isRateLimited() ? Math.ceil((rateLimitedUntil() - Date.now()) / 1000) : 0;
  return { limited: isRateLimited(), secondsLeft };
}

export { RATE_LIMIT_BACKOFF_MS };
