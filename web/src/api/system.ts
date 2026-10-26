import client from './client';

/**
 * The server's stamped version. `GET /api/agent/version` is unauthenticated
 * and is the same answer agents poll for self-update, so the console reports
 * the fleet's version from the server's own statement rather than from a
 * build-time guess. See docs/versioning.md.
 */
export interface ServerVersion {
  version: string;
  git_commit: string;
}

export async function getServerVersion(): Promise<ServerVersion> {
  const { data } = await client.get('/agent/version');
  return {
    version: typeof data?.version === 'string' ? data.version : '',
    git_commit: typeof data?.git_commit === 'string' ? data.git_commit : '',
  };
}
