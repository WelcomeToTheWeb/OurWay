import client from './client';

export interface APIKey {
  id: string;
  name: string;
  key: string;
  scopes: string;
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
  rotated_at: string | null;
  created_at: string;
}

export async function createAPIKey(key: {
  name: string;
  scopes?: string[];
  expires?: string;
}): Promise<APIKey> {
  const res = await client.post('/v2/api-keys', key);
  return res.data.key;
}

export async function listAPIKeys(): Promise<APIKey[]> {
  const res = await client.get('/v2/api-keys');
  return res.data.keys || [];
}

export async function getAPIKey(id: string): Promise<APIKey> {
  const res = await client.get(`/v2/api-keys/${id}`);
  return res.data.key;
}

export async function updateAPIKey(
  id: string,
  key: Partial<{ name: string; scopes: string[] }>
): Promise<APIKey> {
  const res = await client.put(`/v2/api-keys/${id}`, key);
  return res.data.key;
}

export async function deleteAPIKey(id: string): Promise<void> {
  await client.delete(`/v2/api-keys/${id}`);
}

export async function revokeAPIKey(id: string): Promise<APIKey> {
  const res = await client.post(`/v2/api-keys/${id}/revoke`);
  return res.data.key;
}

export async function rotateAPIKey(id: string, expires?: string): Promise<APIKey> {
  const res = await client.post(`/v2/api-keys/${id}/rotate`, { expires });
  return res.data.key;
}
