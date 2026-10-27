import axios from 'axios';

/**
 * Read-request throttle shared by every API call in the console.
 *
 * MAX_CONCURRENT_READS bounds how many GETs may be in flight at once;
 * acquireReadSlot resolves when a slot is free. Exports exist for tests and
 * for the harvest poll, which wants to know how deep the queue is.
 */
export const MAX_CONCURRENT_READS = 4;

let activeReads = 0;
const readQueue: Array<() => void> = [];

export function acquireReadSlot(): Promise<void> {
  if (activeReads < MAX_CONCURRENT_READS) {
    activeReads += 1;
    return Promise.resolve();
  }
  return new Promise<void>((resolve) => readQueue.push(resolve));
}

export function releaseReadSlot(): void {
  // A waiter takes the slot over directly, so the active count only drops
  // when nobody is queued.
  const next = readQueue.shift();
  if (next) {
    next();
  } else {
    activeReads = Math.max(0, activeReads - 1);
  }
}

export function pendingReads(): { active: number; queued: number } {
  return { active: activeReads, queued: readQueue.length };
}

/**
 * Rate-limit awareness. The server answers 429 when a user exceeds 100
 * requests per minute (burst 10). Polling is what burns the budget, so on
 * the first 429 the console backs every poll off for a minute and says so,
 * instead of hammering the limiter and painting every panel red.
 */
export const RATE_LIMIT_BACKOFF_MS = 60_000;

let limitedUntil = 0;
const limitListeners = new Set<() => void>();

export function isRateLimited(): boolean {
  return limitedUntil > Date.now();
}

export function rateLimitedUntil(): number {
  return limitedUntil;
}

export function onRateLimitChange(fn: () => void): () => void {
  limitListeners.add(fn);
  return () => limitListeners.delete(fn);
}

/**
 * The client scope the server last said it applied (2.3-1, read by 2.3-5).
 *
 * Every scoped list read carries `X-OurWay-Client-Scope`. Capturing it here, in
 * the one place every read passes through, is what keeps the shift bar's
 * "which tenants am I looking at" honest on every surface — devices, alerts,
 * sessions, files, the audit ledger — instead of tying it to the one page that
 * happens to ask for it.
 *
 * "The technician cannot find the unit" is the support ticket this release is
 * measured by, and this header is the answer to it.
 */
let lastScope = '';
const scopeListeners = new Set<() => void>();

export function scopeHeader(): string {
  return lastScope;
}

export function onScopeChange(fn: () => void): () => void {
  scopeListeners.add(fn);
  return () => scopeListeners.delete(fn);
}

/**
 * Forget the captured scope. Module state, so a test starts where a page load
 * starts — with no read answered yet — the same reason the fleet store has
 * `resetFleetPoller`. The bar's rule is to print nothing until the server has
 * said something, and only a reset lets a test prove that rule.
 */
export function resetScopeHeader(): void {
  lastScope = '';
  for (const fn of scopeListeners) fn();
}

/**
 * The capture point of the scope header. The response interceptor calls it on
 * every read, success or refusal; it is exported because the header is a
 * server answer, and a test that wants to show what the console does with one
 * should be able to hand it one rather than fake an HTTP round trip.
 */
export function rememberScope(headers: unknown): void {
  const h = headers as Record<string, unknown> | undefined;
  const value = h?.['x-ourway-client-scope'];
  if (typeof value === 'string' && value !== '' && value !== lastScope) {
    lastScope = value;
    for (const fn of scopeListeners) fn();
  }
}

function noteRateLimit(): void {
  const until = Date.now() + RATE_LIMIT_BACKOFF_MS;
  if (until > limitedUntil) {
    limitedUntil = until;
    for (const fn of limitListeners) fn();
  }
}

const client = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
});

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('ourway_access_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  // A dashboard mount fans out to six-plus endpoints at once, and the harvest
  // poll adds more. The server's limiter allows 100 requests per minute per
  // user with an instantaneous burst of 10, so an unthrottled mount burns the
  // whole burst and comes back with 429 panels. Reads are queued four at a
  // time — under the refill rate — so a page paints fully instead of
  // partially. Writes go straight through: acting must never queue behind a
  // poll.
  if ((config.method || 'get').toLowerCase() === 'get') {
    return acquireReadSlot().then(() => config);
  }
  return config;
});

client.interceptors.response.use(
  (response) => {
    if ((response.config?.method || 'get').toLowerCase() === 'get') releaseReadSlot();
    rememberScope(response.headers);
    return response;
  },
  async (error) => {
    if ((error.config?.method || 'get').toLowerCase() === 'get') releaseReadSlot();
    // A refused read carries the scope too: the 403 that says "you may read no
    // ledger" is exactly the answer a technician hunting a missing unit needs.
    rememberScope(error.response?.headers);
    const original = error.config;
    if (error.response?.status === 401 && !original?._retried) {
      // Try to renew the session with the refresh token before forcing a
      // logout — otherwise a single expired access token kills the session
      // even when a valid refresh token exists (e.g. SSO logins).
      const refreshToken = localStorage.getItem('ourway_refresh_token');
      if (refreshToken) {
        try {
          const { data } = await axios.post('/api/auth/refresh', {
            refresh_token: refreshToken,
          });
          localStorage.setItem('ourway_access_token', data.access_token);
          original._retried = true;
          original.headers.Authorization = `Bearer ${data.access_token}`;
          return client(original);
        } catch {
          // Refresh failed — fall through to logout.
        }
      }
    }
    if (error.response?.status === 429) {
      noteRateLimit();
    }
    if (error.response?.status === 401) {
      localStorage.removeItem('ourway_access_token');
      localStorage.removeItem('ourway_refresh_token');
      localStorage.removeItem('ourway_user');
      if (window.location.pathname !== '/login') {
        window.location.replace('/login');
      }
    }
    return Promise.reject(error);
  }
);

export default client;
