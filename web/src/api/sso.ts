import client from './client';

export interface SSOProvider {
  type: string;
  name: string;
}

/**
 * A provider as the admin API serializes it (2.4-1/2.4-4). `client_secret` is
 * `json:"-"` on the server, so it never round-trips to the browser: the edit form
 * leaves it empty to mean "keep the stored one", and the answer never contains it.
 */
export interface SSOProviderFull {
  id: string;
  type: string;
  name: string;
  client_id: string;
  auth_url: string;
  token_url: string;
  user_info_url: string;
  scope: string;
  enabled: boolean;
  created_at: string;
  issuer: string;
  discovery_url: string;
  jwks_uri: string;
  redirect_uri: string;
  discovered_at: string | null;
  pkce_supported: boolean;
  claims_email: string;
  claims_username: string;
  claims_name: string;
  claims_groups: string;
  allowed_domains: string;
  require_verified_email: boolean;
  link_policy: 'pending' | 'link' | string;
  role_mapping: string;
  sort_order: number;
}

/** The fields the console sends when it saves a provider. */
export type SSOProviderForm = Partial<
  Pick<
    SSOProviderFull,
    | 'type'
    | 'name'
    | 'issuer'
    | 'discovery_url'
    | 'redirect_uri'
    | 'auth_url'
    | 'token_url'
    | 'user_info_url'
    | 'scope'
    | 'claims_email'
    | 'claims_username'
    | 'claims_name'
    | 'claims_groups'
    | 'allowed_domains'
    | 'require_verified_email'
    | 'link_policy'
    | 'role_mapping'
    | 'sort_order'
    | 'enabled'
  > & { client_id: string; client_secret: string }
>;

/**
 * What an IdP published (the discovery preview, 2.4-1) and the named checks an
 * operator acts on. `checks[].state` is pass / warn / fail / skipped; `discovery`
 * names why a preview was refused (`discovery_unreachable`, `discovery_document`,
 * `issuer_mismatch`, `no_code_flow`).
 */
export interface DiscoveryCheck {
  name: string;
  state: 'pass' | 'warn' | 'fail' | 'skipped';
  detail?: string;
}

export interface DiscoveryPreview {
  ok: boolean;
  issuer: string;
  discovery: string;
  discovery_url: string;
  redirect_uri: string;
  endpoints: { authorization: string; token: string; userinfo: string; jwks: string };
  signing_algs: string[];
  response_types: string[];
  scopes: string[];
  pkce_methods: string[];
  checks: DiscoveryCheck[];
  document?: unknown;
}

export interface ConnectionTest {
  ok: boolean;
  provider: string;
  checks: DiscoveryCheck[];
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
  // The login page's order is the operator's promise to the people signing in, so
  // the ledger shows it in the same order (2.4-4).
  return (res.data.providers || []).slice().sort((a: SSOProviderFull, b: SSOProviderFull) =>
    a.sort_order === b.sort_order ? a.name.localeCompare(b.name) : a.sort_order - b.sort_order
  );
}

export async function createSSOProvider(provider: SSOProviderForm): Promise<SSOProviderFull> {
  const res = await client.post('/sso/providers', provider);
  return res.data.provider;
}

/**
 * Edit a saved provider in place (2.4-4). Fields the caller omits are left alone
 * server-side, so the enable/order toggles cannot wipe a policy. The name is
 * immutable: it is the identity key stored on every account signed in through this
 * provider.
 */
export async function updateSSOProvider(id: string, form: SSOProviderForm): Promise<SSOProviderFull> {
  const res = await client.put(`/sso/providers/${id}`, form);
  return res.data.provider;
}

export async function deleteSSOProvider(id: string): Promise<void> {
  await client.delete(`/sso/providers/${id}`);
}

/** Ask an issuer what it publishes, before anything is saved (admin, audited). */
export async function discoverIssuer(issuer: string, name = ''): Promise<DiscoveryPreview> {
  const res = await client.post('/sso/providers/discover', { issuer, name });
  return res.data;
}

/** Test-connection for a saved provider, as named states (admin, audited). */
export async function testSSOProvider(id: string): Promise<ConnectionTest> {
  const res = await client.post(`/sso/providers/${id}/test`, {});
  return res.data;
}

/**
 * The three presets are starting points of the same discovery path (2.4-1): they
 * supply an issuer and a scope, and the IdP publishes the rest. A provider named
 * anything else is configured the same way — the presets are a convenience, not a
 * whitelist.
 */
export const SSO_PRESETS: { name: string; issuer: string; scope: string }[] = [
  { name: 'google', issuer: 'https://accounts.google.com', scope: 'openid profile email' },
  {
    name: 'microsoft',
    issuer: 'https://login.microsoftonline.com/common/v2.0',
    scope: 'openid profile email offline_access',
  },
  { name: 'apple', issuer: 'https://appleid.apple.com', scope: 'openid' },
];
