import client from './client';

export interface APIKey {
  id: string;
  name: string;
  // The plaintext secret is only present in create/rotate responses
  // (as key_value); list/get/update responses never include it.
  key?: string;
  scopes: string;
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
  rotated_at: string | null;
  created_at: string;
}

// mergeKeyValue folds the one-time plaintext secret (key_value) from a
// create/rotate response into the key object, so the UI can display it.
function mergeKeyValue(data: {
  key: APIKey;
  key_value?: string;
}): APIKey {
  return data.key_value ? { ...data.key, key: data.key_value } : data.key;
}

export async function createAPIKey(key: {
  name: string;
  scopes?: string[];
  expires?: string;
}): Promise<APIKey> {
  const res = await client.post('/v2/api-keys', key);
  return mergeKeyValue(res.data);
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

export async function revokeAPIKey(id: string): Promise<void> {
  await client.post(`/v2/api-keys/${id}/revoke`);
}

export async function rotateAPIKey(id: string, expires?: string): Promise<APIKey> {
  const res = await client.post(`/v2/api-keys/${id}/rotate`, { expires });
  return mergeKeyValue(res.data);
}
