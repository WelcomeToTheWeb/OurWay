import client from './client';

/**
 * The server's stamped version. `GET /api/agent/version` is unauthenticated
 * and is the same answer agents poll for self-update, so the console reports
 * the fleet's version from the server's own statement rather than from a
 * build-time guess. See docs/versioning.md.
 *
 * It also states the per-user request budget (docs/configuration.md:
 * RATE_LIMIT_PER_MINUTE / RATE_LIMIT_BURST), so the operator sees the ceiling
 * their polling is measured against.
 */
export interface ServerVersion {
  version: string;
  git_commit: string;
  rate_limit_per_minute: number | null;
  rate_limit_burst: number | null;
}

export async function getServerVersion(): Promise<ServerVersion> {
  const { data } = await client.get('/agent/version');
  const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
  return {
    version: typeof data?.version === 'string' ? data.version : '',
    git_commit: typeof data?.git_commit === 'string' ? data.git_commit : '',
    rate_limit_per_minute: num(data?.rate_limit_per_minute),
    rate_limit_burst: num(data?.rate_limit_burst),
  };
}
