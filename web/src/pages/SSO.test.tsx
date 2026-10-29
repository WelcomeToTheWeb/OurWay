import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { SSO } from './SSO';
import { AuthProvider } from '../auth/context';
import type { DiscoveryPreview, SSOProviderFull } from '../api/sso';

vi.mock('../api/sso', () => ({
  listSSOProvidersAdmin: vi.fn(),
  createSSOProvider: vi.fn(),
  updateSSOProvider: vi.fn(),
  deleteSSOProvider: vi.fn(),
  discoverIssuer: vi.fn(),
  testSSOProvider: vi.fn(),
  SSO_PRESETS: [
    { name: 'google', issuer: 'https://accounts.google.com', scope: 'openid profile email' },
    {
      name: 'microsoft',
      issuer: 'https://login.microsoftonline.com/common/v2.0',
      scope: 'openid profile email offline_access',
    },
    { name: 'apple', issuer: 'https://appleid.apple.com', scope: 'openid' },
  ],
}));

import {
  listSSOProvidersAdmin,
  createSSOProvider,
  updateSSOProvider,
  deleteSSOProvider,
  discoverIssuer,
  testSSOProvider,
} from '../api/sso';

const mockedList = vi.mocked(listSSOProvidersAdmin);
const mockedCreate = vi.mocked(createSSOProvider);
const mockedUpdate = vi.mocked(updateSSOProvider);
const mockedDelete = vi.mocked(deleteSSOProvider);
const mockedDiscover = vi.mocked(discoverIssuer);
const mockedTest = vi.mocked(testSSOProvider);

function provider(over: Partial<SSOProviderFull> = {}): SSOProviderFull {
  return {
    id: 'prov-keycloak',
    type: 'oidc',
    name: 'keycloak-corp',
    client_id: 'ourway',
    auth_url: 'https://idp.corp/realms/ourway/protocol/openid-connect/auth',
    token_url: 'https://idp.corp/realms/ourway/protocol/openid-connect/token',
    user_info_url: 'https://idp.corp/realms/ourway/protocol/openid-connect/userinfo',
    scope: 'openid profile email',
    enabled: true,
    created_at: '2026-11-20T10:00:00Z',
    issuer: 'https://idp.corp/realms/ourway',
    discovery_url: '',
    jwks_uri: 'https://idp.corp/realms/ourway/protocol/openid-connect/certs',
    redirect_uri: '',
    discovered_at: '2026-11-20T10:00:00Z',
    pkce_supported: true,
    claims_email: '',
    claims_username: '',
    claims_name: '',
    claims_groups: '',
    allowed_domains: 'corp.example, partner.example',
    require_verified_email: true,
    link_policy: 'pending',
    role_mapping: '{"rmm-admins":"admin"}',
    sort_order: 0,
    ...over,
  };
}

function preview(over: Partial<DiscoveryPreview> = {}): DiscoveryPreview {
  return {
    ok: true,
    issuer: 'https://idp.corp/realms/ourway',
    discovery: 'ok',
    discovery_url: 'https://idp.corp/realms/ourway/.well-known/openid-configuration',
    redirect_uri: 'https://ourway.corp/api/auth/sso/keycloak-corp/callback',
    endpoints: {
      authorization: 'https://idp.corp/realms/ourway/protocol/openid-connect/auth',
      token: 'https://idp.corp/realms/ourway/protocol/openid-connect/token',
      userinfo: 'https://idp.corp/realms/ourway/protocol/openid-connect/userinfo',
      jwks: 'https://idp.corp/realms/ourway/protocol/openid-connect/certs',
    },
    signing_algs: ['RS256'],
    response_types: ['code'],
    scopes: ['openid', 'profile', 'email'],
    pkce_methods: ['S256'],
    checks: [
      { name: 'discovery', state: 'pass', detail: 'https://idp.corp/realms/ourway' },
      { name: 'redirect_uri', state: 'pass', detail: 'https://ourway.corp/api/auth/sso/keycloak-corp/callback' },
      { name: 'jwks', state: 'pass', detail: 'https://idp.corp/realms/ourway/protocol/openid-connect/certs' },
    ],
    ...over,
  };
}

function setRoles(roles: string[]) {
  localStorage.setItem('ourway_roles', JSON.stringify(roles));
}

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={['/sso']}>
        <Routes>
          <Route path="/sso" element={<SSO />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}


/** The ledger row for a provider. Scoping to the table keeps the assertion about
 * the row, not about the page's "first / last in view" words. */
async function ledgerRow(name: string): Promise<HTMLTableRowElement> {
  const table = await screen.findByRole('table');
  const rows = within(table).getAllByRole('row').slice(1);
  const row = rows.find((r) => (r.textContent ?? '').includes(name));
  if (!row) throw new Error(`no ledger row for ${name}`);
  return row as HTMLTableRowElement;
}

describe('SSO provider ledger (2.4-4)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(['admin']);
    mockedList.mockResolvedValue([provider()]);
    mockedCreate.mockResolvedValue(provider());
    mockedUpdate.mockResolvedValue(provider());
    mockedDelete.mockResolvedValue(undefined);
    mockedDiscover.mockResolvedValue(preview());
    mockedTest.mockResolvedValue({ ok: true, provider: 'keycloak-corp', checks: [{ name: 'discovery', state: 'pass' }] });
  });

  it('states the issuer, the contract and the order — not a preset dropdown', async () => {
    renderPage();
    const row = await ledgerRow('keycloak-corp');
    expect(row.textContent).toContain('https://idp.corp/realms/ourway');
    expect(row.textContent).toContain('2 domains');
    expect(row.textContent).toContain('verified email');
    expect(row.textContent).toContain('role mapping');
    expect(row.textContent).toContain('pending');
  });

  it('names a provider that links by email, because that is a decision an operator made', async () => {
    mockedList.mockResolvedValue([provider({ link_policy: 'link' })]);
    renderPage();
    const row = await ledgerRow('keycloak-corp');
    expect(row.textContent).toContain('links by email');
  });

  it('saves an issuer and a policy, not endpoints, on create', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Add Provider/ }));
    const form = await screen.findByRole('form');

    fireEvent.change(within(form).getByLabelText(/Provider name/), { target: { value: 'authentik-corp' } });
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: 'https://idp.example/application/openid-client/ourway' },
    });
    fireEvent.change(within(form).getByLabelText(/Client ID/), { target: { value: 'ourway' } });
    fireEvent.change(within(form).getByLabelText(/Client Secret/), { target: { value: 'a-secret' } });
    fireEvent.submit(form);

    await waitFor(() => expect(mockedCreate).toHaveBeenCalled());
    const payload = mockedCreate.mock.calls[0][0];
    expect(payload.issuer).toBe('https://idp.example/application/openid-client/ourway');
    expect(payload.name).toBe('authentik-corp');
    expect(payload.link_policy).toBe('pending');
    // The endpoints are the IdP's to publish (2.4-1): the form has none to type.
    expect(within(form).queryByLabelText(/Authorization endpoint|Token endpoint/)).toBeNull();
  });

  it('prefills a preset and leaves room for any other IdP', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /^Google$/ }));
    const form = await screen.findByRole('form');
    const issuer = within(form).getByLabelText(/^Issuer/) as HTMLInputElement;
    expect(issuer.value).toBe('https://accounts.google.com');
    // A preset is a starting point, not the only door: the name field is editable
    // for a new provider, so any issuer can be named.
    const name = within(form).getByLabelText(/Provider name/) as HTMLInputElement;
    expect(name.disabled).toBe(false);
  });

  it('shows what the IdP published, including the redirect URI to register', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Add Provider/ }));
    const form = await screen.findByRole('form');
    fireEvent.change(within(form).getByLabelText(/^Issuer/), {
      target: { value: 'https://idp.corp/realms/ourway' },
    });
    fireEvent.click(within(form).getByRole('button', { name: /Ask the IdP what it publishes/ }));

    const panel = await screen.findByRole('status', { name: /What the IdP published/ });
    expect(panel.textContent).toContain('https://idp.corp/realms/ourway/protocol/openid-connect/auth');
    expect(panel.textContent).toContain('https://ourway.corp/api/auth/sso/keycloak-corp/callback');
    expect(panel.textContent).toContain('RS256');
    expect(mockedDiscover).toHaveBeenCalledWith('https://idp.corp/realms/ourway', '');
  });

  it('names a refused discovery by kind, and says nothing was saved', async () => {
    mockedDiscover.mockResolvedValue(
      preview({
        ok: false,
        issuer: '',
        discovery: 'issuer_mismatch',
        checks: [
          {
            name: 'discovery',
            state: 'fail',
            detail: 'issuer_mismatch: document says https://other.example',
          },
        ],
      })
    );
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Add Provider/ }));
    const form = await screen.findByRole('form');
    fireEvent.change(within(form).getByLabelText(/^Issuer/), { target: { value: 'https://idp.corp' } });
    fireEvent.click(within(form).getByRole('button', { name: /Ask the IdP what it publishes/ }));
    const panel = await screen.findByRole('status', { name: /What the IdP published/ });
    expect(panel.textContent).toContain('issuer_mismatch');
    expect(panel.textContent).toContain('Nothing was saved by this call.');
  });

  it('edits a provider in place and sends only the fields it touched for a toggle', async () => {
    renderPage();
    const row = await ledgerRow('keycloak-corp');
    fireEvent.click(within(row).getByRole('button', { name: /^Test$/ }));
    await waitFor(() => expect(mockedTest).toHaveBeenCalledWith('prov-keycloak'));

    // The enable toggle is one field — it cannot carry a policy wipe.
    fireEvent.click(within(row).getByRole('checkbox'));
    await waitFor(() => expect(mockedUpdate).toHaveBeenCalledWith('prov-keycloak', { enabled: false }));
  });

  it('opens the edit form with the stored values and never the secret', async () => {
    renderPage();
    const row = await ledgerRow('keycloak-corp');
    fireEvent.click(within(row).getByRole('button', { name: /Edit provider/ }));
    const form = await screen.findByRole('form');
    const secret = within(form).getByLabelText(/Client Secret/) as HTMLInputElement;
    expect(secret.value).toBe('');
    expect(secret.placeholder).toBe('Saved — type to replace it');
    // The name is the identity key: the form says so and locks it.
    const name = within(form).getByLabelText(/Provider name/) as HTMLInputElement;
    expect(name.disabled).toBe(true);
    expect(form.textContent).toContain('identity key stored on every account');
    expect((within(form).getByLabelText(/Allowed domains/) as HTMLInputElement).value).toBe(
      'corp.example, partner.example'
    );
  });

  it('reports a refused save at page level, in the server\'s words', async () => {
    mockedCreate.mockRejectedValue(
      new Error('provider not saved: issuer_mismatch: document says https://other.example')
    );
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /Add Provider/ }));
    const form = await screen.findByRole('form');
    fireEvent.change(within(form).getByLabelText(/Provider name/), { target: { value: 'broken-corp' } });
    fireEvent.change(within(form).getByLabelText(/^Issuer/), { target: { value: 'https://idp.corp' } });
    fireEvent.change(within(form).getByLabelText(/Client ID/), { target: { value: 'ourway' } });
    fireEvent.submit(form);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('issuer_mismatch');
  });

  it('deletes through the sheet, naming the provider', async () => {
    renderPage();
    const row = await ledgerRow('keycloak-corp');
    fireEvent.click(within(row).getByRole('button', { name: /Delete/ }));
    const sheet = await screen.findByRole('dialog');
    expect(sheet.textContent).toContain('keycloak-corp');
    fireEvent.click(within(sheet).getByRole('button', { name: /Delete binding/ }));
    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith('prov-keycloak'));
  });

  it('lists providers in the order the login page will render them', async () => {
    mockedList.mockResolvedValue([
      provider({ id: 'p1', name: 'first-door', sort_order: 1 }),
      provider({ id: 'p2', name: 'second-door', sort_order: 2 }),
      provider({ id: 'p3', name: 'third-door', sort_order: 3 }),
    ]);
    renderPage();
    // The page renders what the server answered, in that order (the server's
    // `sort_order, name` ordering is proven in the API suite).
    const table = await screen.findByRole('table');
    const rows = within(table)
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '');
    expect(rows.findIndex((text) => text.includes('first-door'))).toBe(0);
    expect(rows.findIndex((text) => text.includes('second-door'))).toBe(1);
    expect(rows.findIndex((text) => text.includes('third-door'))).toBe(2);
  });
});
