import client from './client';

export interface SSOProvider {
  type: string;
  name: string;
}

export interface SSOProviderFull {
  id: string;
  type: string;
  name: string;
  client_id: string;
  client_secret: string;
  auth_url: string;
  token_url: string;
  user_info_url: string;
  scope: string;
  enabled: boolean;
  created_at: string;
}

export async function listSSOProviders(): Promise<SSOProvider[]> {
  const res = await client.get('/auth/sso/providers');
  return res.data.providers || [];
}

export function authorizeProvider(provider: string): string {
  return `/api/auth/sso/${provider}/authorize`;
}

export interface SSOExchangeResult {
  access_token: string;
  refresh_token: string;
}

// exchangeSSOCode redeems the one-time SSO login code from the callback
// redirect for the token pair. The tokens never appear in the URL (C6).
export async function exchangeSSOCode(code: string): Promise<SSOExchangeResult> {
  const res = await client.post('/auth/sso/exchange', { code });
  return res.data;
}

export async function listSSOProvidersAdmin(): Promise<SSOProviderFull[]> {
  const res = await client.get('/sso/providers');
  return res.data.providers || [];
}

export async function createSSOProvider(provider: Partial<SSOProviderFull>): Promise<SSOProviderFull> {
  const res = await client.post('/sso/providers', provider);
  return res.data.provider;
}

export async function deleteSSOProvider(id: string): Promise<void> {
  await client.delete(`/sso/providers/${id}`);
}
