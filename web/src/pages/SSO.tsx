import { useState, useEffect } from 'react';
import { Plus, Trash2, KeyRound, ScanSearch, Pencil, Cable } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  listSSOProvidersAdmin,
  createSSOProvider,
  updateSSOProvider,
  deleteSSOProvider,
  discoverIssuer,
  testSSOProvider,
  SSO_PRESETS,
  type SSOProviderForm,
  type SSOProviderFull,
  type DiscoveryCheck,
  type DiscoveryPreview,
  type ConnectionTest,
} from '../api/sso';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';

interface FormState {
  id: string;
  preset: string;
  name: string;
  issuer: string;
  client_id: string;
  client_secret: string;
  scope: string;
  redirect_uri: string;
  claims_email: string;
  claims_username: string;
  claims_name: string;
  claims_groups: string;
  allowed_domains: string;
  require_verified_email: boolean;
  link_policy: string;
  role_mapping: string;
  sort_order: number;
  enabled: boolean;
}

const emptyForm: FormState = {
  id: '',
  preset: '',
  name: '',
  issuer: '',
  client_id: '',
  client_secret: '',
  scope: '',
  redirect_uri: '',
  claims_email: '',
  claims_username: '',
  claims_name: '',
  claims_groups: '',
  allowed_domains: '',
  require_verified_email: false,
  link_policy: 'pending',
  role_mapping: '',
  sort_order: 0,
  enabled: true,
};

function formFromProvider(p: SSOProviderFull): FormState {
  return {
    ...emptyForm,
    id: p.id,
    name: p.name,
    issuer: p.issuer,
    client_id: p.client_id,
    scope: p.scope,
    redirect_uri: p.redirect_uri,
    claims_email: p.claims_email,
    claims_username: p.claims_username,
    claims_name: p.claims_name,
    claims_groups: p.claims_groups,
    allowed_domains: p.allowed_domains,
    require_verified_email: p.require_verified_email,
    link_policy: p.link_policy || 'pending',
    role_mapping: p.role_mapping,
    sort_order: p.sort_order,
    enabled: p.enabled,
  };
}

/**
 * Providers, as the doors they are (2.4-4).
 *
 * The operator names an **issuer** and OurWay asks the IdP what it publishes;
 * the ledger shows what the IdP answered, the identity contract that governs
 * logins through this door, and the order the login page will render.
 */
export function SSO() {
  const { t } = useTranslation();
  const [providers, setProviders] = useState<SSOProviderFull[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [preview, setPreview] = useState<DiscoveryPreview | null>(null);
  const [previewBusy, setPreviewBusy] = useState(false);
  const [testing, setTesting] = useState<ConnectionTest | null>(null);
  const [testBusyId, setTestBusyId] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const guide = useGuideWords(providers.length);
  const editing = form.id !== '';

  async function loadProviders() {
    try {
      setProviders(await listSSOProvidersAdmin());
    } catch (err) {
      console.error('Failed to load SSO providers', err);
      setFormError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadProviders();
  }, []);

  function openCreate(presetName = '') {
    const preset = SSO_PRESETS.find((p) => p.name === presetName);
    setForm({
      ...emptyForm,
      preset: presetName,
      name: presetName,
      issuer: preset?.issuer ?? '',
      scope: preset?.scope ?? '',
    });
    setPreview(null);
    setFormError(null);
    setShowForm(true);
  }

  function openEdit(p: SSOProviderFull) {
    setForm(formFromProvider(p));
    setPreview(null);
    setFormError(null);
    setShowForm(true);
  }

  function payload(): SSOProviderForm {
    return {
      type: 'oidc',
      name: form.name.trim(),
      issuer: form.issuer.trim(),
      client_id: form.client_id.trim(),
      // An empty secret means "keep the stored one": the stored value never
      // reaches the browser to be sent back.
      client_secret: form.client_secret,
      redirect_uri: form.redirect_uri.trim(),
      scope: form.scope.trim(),
      claims_email: form.claims_email.trim(),
      claims_username: form.claims_username.trim(),
      claims_name: form.claims_name.trim(),
      claims_groups: form.claims_groups.trim(),
      allowed_domains: form.allowed_domains.trim(),
      require_verified_email: form.require_verified_email,
      link_policy: form.link_policy,
      role_mapping: form.role_mapping.trim(),
      sort_order: Number(form.sort_order) || 0,
      enabled: form.enabled,
    };
  }

  async function handleSave() {
    if (!form.name.trim() || !form.client_id.trim()) {
      setFormError(t('sso.needNameAndClient'));
      return;
    }
    setSaving(true);
    setFormError(null);
    try {
      if (editing) {
        await updateSSOProvider(form.id, payload());
      } else {
        await createSSOProvider(payload());
      }
      setShowForm(false);
      setForm(emptyForm);
      setPreview(null);
      await loadProviders();
    } catch (err) {
      setFormError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  async function handleQuickToggle(p: SSOProviderFull, enabled: boolean) {
    try {
      await updateSSOProvider(p.id, { enabled });
      await loadProviders();
    } catch (err) {
      setFormError((err as Error).message);
    }
  }

  async function handleDiscover() {
    setPreviewBusy(true);
    setFormError(null);
    setPreview(null);
    try {
      setPreview(await discoverIssuer(form.issuer.trim(), form.name.trim()));
    } catch (err) {
      setFormError((err as Error).message);
    } finally {
      setPreviewBusy(false);
    }
  }

  async function handleTest(p: SSOProviderFull) {
    setTestBusyId(p.id);
    setFormError(null);
    try {
      setTesting(await testSSOProvider(p.id));
    } catch (err) {
      setFormError((err as Error).message);
    } finally {
      setTestBusyId(null);
    }
  }

  async function handleDelete(p: SSOProviderFull) {
    try {
      await deleteSSOProvider(p.id);
      setPending(null);
      await loadProviders();
    } catch (err) {
      setFormError((err as Error).message);
      setPending(null);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('sso.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">{t('sso.subtitle')}</p>
        </div>
        <button onClick={() => openCreate()} className="btn-primary">
          <Plus className="h-3.5 w-3.5" />
          {t('sso.addProvider')}
        </button>
      </div>

      {formError && (
        <div role="alert" className="panel px-3 py-2">
          <span className="cap cap-sm text-[var(--stamp)]">{formError}</span>
        </div>
      )}

      {/* The presets are the same discovery path, pre-filled. Any other IdP is
          configured below them the same way. */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="cap cap-sm text-text-muted">{t('sso.presets')}</span>
        {SSO_PRESETS.map((preset) => (
          <button
            key={preset.name}
            onClick={() => openCreate(preset.name)}
            className="btn-quiet btn"
            title={preset.issuer}
          >
            {t(`sso.${preset.name}`)}
          </button>
        ))}
      </div>

      {showForm && (
        <form
          className="panel p-4"
          aria-label={editing ? t('sso.editTitle') : t('sso.addProviderTitle')}
          onSubmit={(e) => {
            e.preventDefault();
            void handleSave();
          }}
        >
          <h2 className="panel-head">{editing ? t('sso.editTitle') : t('sso.addProviderTitle')}</h2>
          <p className="cap cap-sm mt-1 text-text-muted">{t('sso.formBody')}</p>

          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <label className="block">
              <span className="field-label">{t('sso.nameField')}</span>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                disabled={editing}
                className="input mt-1 w-full"
                placeholder="keycloak-corp"
              />
              {editing && (
                <span className="cap cap-sm mt-1 block text-text-muted">{t('sso.nameLocked')}</span>
              )}
            </label>
            <label className="block sm:col-span-2">
              <span className="field-label">{t('sso.issuer')}</span>
              <input
                type="url"
                value={form.issuer}
                onChange={(e) => {
                  setForm({ ...form, issuer: e.target.value });
                  setPreview(null);
                }}
                className="input mt-1 w-full"
                placeholder="https://idp.example/realms/ourway"
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('sso.issuerBody')}</span>
            </label>
            <label className="block">
              <span className="field-label">{t('sso.clientId')}</span>
              <input
                type="text"
                value={form.client_id}
                onChange={(e) => setForm({ ...form, client_id: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('sso.clientIdPlaceholder')}
              />
            </label>
            <label className="block">
              <span className="field-label">{t('sso.clientSecret')}</span>
              <input
                type="password"
                value={form.client_secret}
                onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                className="input mt-1 w-full"
                placeholder={editing ? t('sso.clientSecretKept') : t('sso.clientSecretPlaceholder')}
                autoComplete="off"
              />
            </label>
            <label className="block">
              <span className="field-label">{t('sso.scope')}</span>
              <input
                type="text"
                value={form.scope}
                onChange={(e) => setForm({ ...form, scope: e.target.value })}
                className="input mt-1 w-full"
                placeholder="openid profile email"
              />
            </label>
            <label className="block sm:col-span-2">
              <span className="field-label">{t('sso.redirectUri')}</span>
              <input
                type="url"
                value={form.redirect_uri}
                onChange={(e) => setForm({ ...form, redirect_uri: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('sso.redirectUriPlaceholder')}
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('sso.redirectUriBody')}</span>
            </label>
            <label className="block">
              <span className="field-label">{t('sso.sortOrder')}</span>
              <input
                type="number"
                value={form.sort_order}
                onChange={(e) => setForm({ ...form, sort_order: Number(e.target.value) })}
                className="input mt-1 w-full"
              />
            </label>
            <label className="block">
              <span className="field-label">{t('sso.claimsEmail')}</span>
              <input
                type="text"
                value={form.claims_email}
                onChange={(e) => setForm({ ...form, claims_email: e.target.value })}
                className="input mt-1 w-full"
                placeholder="email"
              />
            </label>
            <label className="block">
              <span className="field-label">{t('sso.claimsGroups')}</span>
              <input
                type="text"
                value={form.claims_groups}
                onChange={(e) => setForm({ ...form, claims_groups: e.target.value })}
                className="input mt-1 w-full"
                placeholder="groups"
              />
            </label>
            <label className="block sm:col-span-2">
              <span className="field-label">{t('sso.allowedDomains')}</span>
              <input
                type="text"
                value={form.allowed_domains}
                onChange={(e) => setForm({ ...form, allowed_domains: e.target.value })}
                className="input mt-1 w-full"
                placeholder="corp.example, partner.example"
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('sso.allowedDomainsBody')}</span>
            </label>
            <label className="block">
              <span className="field-label">{t('sso.linkPolicy')}</span>
              <select
                value={form.link_policy}
                onChange={(e) => setForm({ ...form, link_policy: e.target.value })}
                className="select mt-1 w-full"
              >
                <option value="pending">{t('sso.linkPending')}</option>
                <option value="link">{t('sso.linkLink')}</option>
              </select>
              <span className="cap cap-sm mt-1 block text-text-muted">
                {form.link_policy === 'link' ? t('sso.linkPolicyWarning') : t('sso.linkPolicyBody')}
              </span>
            </label>
            <label className="block">
              <span className="field-label">{t('sso.roleMapping')}</span>
              <input
                type="text"
                value={form.role_mapping}
                onChange={(e) => setForm({ ...form, role_mapping: e.target.value })}
                className="input mt-1 w-full"
                placeholder='{"rmm-admins":"admin"}'
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('sso.roleMappingBody')}</span>
            </label>
            <div className="flex flex-wrap items-center gap-4">
              <label className="flex cursor-pointer items-center gap-2">
                <input
                  type="checkbox"
                  checked={form.require_verified_email}
                  onChange={(e) => setForm({ ...form, require_verified_email: e.target.checked })}
                  className="h-4 w-4"
                  style={{ accentColor: 'var(--service)' }}
                />
                <span className="cap cap-sm">{t('sso.requireVerified')}</span>
              </label>
              <label className="flex cursor-pointer items-center gap-2">
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                  className="h-4 w-4"
                  style={{ accentColor: 'var(--online)' }}
                />
                <span className="cap cap-sm">{t('sso.enabled')}</span>
              </label>
            </div>
          </div>

          <div className="mt-4 flex flex-wrap gap-3">
            <button type="button" onClick={handleDiscover} disabled={previewBusy || !form.issuer.trim()} className="btn">
              <ScanSearch className="h-3.5 w-3.5" />
              {previewBusy ? t('common.loading') : t('sso.discover')}
            </button>
            <button type="submit" disabled={saving} className="btn-primary">
              {saving ? t('common.loading') : t('common.save')}
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="btn">
              {t('common.cancel')}
            </button>
          </div>

          {preview && <DiscoveryPanel preview={preview} />}
        </form>
      )}

      {testing && (
        <div className="panel p-4" role="status" aria-label={t('sso.testTitle')}>
          <h2 className="panel-head">
            {t('sso.testTitle')} · {testing.provider}
          </h2>
          <CheckList checks={testing.checks} />
          <div className="mt-3">
            <button onClick={() => setTesting(null)} className="btn-quiet btn">
              {t('common.close')}
            </button>
          </div>
        </div>
      )}

      {loading ? (
        <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
      ) : providers.length === 0 ? (
        <div className="panel px-4 py-6 text-center">
          <KeyRound className="mx-auto h-5 w-5 text-text-muted" aria-hidden="true" />
          <p className="cap cap-sm mt-2 text-text-secondary">{t('sso.none')}</p>
          <p className="mt-1 text-sm text-text-muted">{t('sso.noneDesc')}</p>
        </div>
      ) : (
        <section className="panel overflow-hidden" aria-label={t('sso.title')}>
          <GuideWordsHead
            total={providers.length}
            unit={t('sheet.inView')}
            first={providers[guide.first]?.name}
            last={providers[guide.last]?.name}
          />
          <div ref={guide.ref} className="max-h-[26rem] overflow-y-auto overflow-x-auto">
            <table className="ledger min-w-[760px]">
              <thead>
                <tr>
                  <th className="w-40">{t('sso.provider')}</th>
                  <th>{t('sso.issuer')}</th>
                  <th>{t('sso.contract')}</th>
                  <th className="w-20">{t('sso.sortOrder')}</th>
                  <th className="w-28">{t('common.enabled', 'State')}</th>
                  <th className="w-44 text-right">{t('common.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((p) => (
                  <tr key={p.id} data-guide title={p.name}>
                    <td>
                      <span className="cap cap-sm text-text-primary">{p.name}</span>
                      <span className="cap cap-sm block text-text-muted">{p.client_id}</span>
                    </td>
                    <td>
                      <span className="ow-num block truncate text-[12px]" title={p.issuer}>
                        {p.issuer || t('sso.typedEndpoints')}
                      </span>
                      <span className="cap cap-sm block text-text-muted">
                        {p.pkce_supported ? t('sso.pkce') : t('sso.noPkce')}
                      </span>
                    </td>
                    <td>
                      <div className="flex flex-wrap gap-1.5">
                        <Stamp tone={p.link_policy === 'link' ? 'due' : 'service'}>
                          {p.link_policy === 'link' ? t('sso.stampLink') : t('sso.stampPending')}
                        </Stamp>
                        {p.allowed_domains ? (
                          <Stamp tone="neutral" title={p.allowed_domains}>
                            {t('sso.stampDomains', { count: p.allowed_domains.split(',').filter(Boolean).length })}
                          </Stamp>
                        ) : (
                          <Stamp tone="neutral">{t('sso.stampAnyDomain')}</Stamp>
                        )}
                        {p.require_verified_email && <Stamp tone="online">{t('sso.stampVerified')}</Stamp>}
                        {p.role_mapping && <Stamp tone="alert">{t('sso.stampMapped')}</Stamp>}
                      </div>
                    </td>
                    <td>
                      <span className="ow-num text-[12px]">{p.sort_order}</span>
                    </td>
                    <td>
                      <label className="flex cursor-pointer items-center gap-2" title={t('sso.toggleBody')}>
                        <input
                          type="checkbox"
                          checked={p.enabled}
                          onChange={(e) => void handleQuickToggle(p, e.target.checked)}
                          className="h-4 w-4"
                          style={{ accentColor: 'var(--online)' }}
                          aria-label={t('sso.enabled')}
                        />
                        <Stamp tone={p.enabled ? 'online' : 'neutral'}>
                          {p.enabled ? t('common.enabled') : t('common.disabled')}
                        </Stamp>
                      </label>
                    </td>
                    <td className="text-right">
                      <div className="flex justify-end gap-1.5">
                        <button
                          onClick={() => void handleTest(p)}
                          disabled={testBusyId === p.id}
                          className="btn-quiet btn"
                          title={t('sso.testTitle')}
                        >
                          <Cable className="h-3.5 w-3.5" />
                          {testBusyId === p.id ? t('common.loading') : t('sso.test')}
                        </button>
                        <button
                          onClick={() => openEdit(p)}
                          className="btn-quiet btn"
                          title={t('sso.editTitle')}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                          <span className="sr-only">{t('sso.editTitle')}</span>
                        </button>
                        <button
                          onClick={() =>
                            setPending(
                              pendingConfirm(t, 'deleteSsoBinding', () => void handleDelete(p), { name: p.name })
                            )
                          }
                          className="btn-quiet btn text-[var(--stamp)]"
                          title={t('common.delete')}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      <ConfirmSheet
        request={pending?.request ?? null}
        onCancel={() => setPending(null)}
        onConfirm={() => pending?.run()}
      />
    </div>
  );
}

/**
 * The discovery preview (2.4-1): what the IdP published, the redirect URI this
 * installation will send, and the named checks. An operator registers the URI shown
 * here with the IdP, and the refusals name the kind — nothing is saved by this call.
 */
function DiscoveryPanel({ preview }: { preview: DiscoveryPreview }) {
  const { t } = useTranslation();
  return (
    <div className="panel mt-4 p-4" role="status" aria-label={t('sso.previewTitle')}>
      <h3 className="panel-head">
        {t('sso.previewTitle')} · {preview.issuer || preview.discovery}
      </h3>
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
        {Object.entries(preview.endpoints).map(([key, url]) => (
          <div key={key} className="min-w-0">
            <span className="field-label">{t(`sso.endpoint.${key}`)}</span>
            <span className="ow-num block truncate text-[12px]" title={url}>
              {url || '—'}
            </span>
          </div>
        ))}
        <div className="min-w-0">
          <span className="field-label">{t('sso.redirectUri')}</span>
          <span className="ow-num block truncate text-[12px]" title={preview.redirect_uri}>
            {preview.redirect_uri}
          </span>
        </div>
        <div className="min-w-0">
          <span className="field-label">{t('sso.signingAlgs')}</span>
          <span className="ow-num block text-[12px]">{preview.signing_algs.join(', ') || '—'}</span>
        </div>
        <div className="min-w-0">
          <span className="field-label">{t('sso.pkceMethods')}</span>
          <span className="ow-num block text-[12px]">{preview.pkce_methods.join(', ') || '—'}</span>
        </div>
        <div className="min-w-0">
          <span className="field-label">{t('sso.scopes')}</span>
          <span className="ow-num block truncate text-[12px]" title={preview.scopes.join(' ')}>
            {preview.scopes.join(', ') || '—'}
          </span>
        </div>
      </div>
      <div className="mt-3">
        <CheckList checks={preview.checks} />
      </div>
      <p className="cap cap-sm mt-3 text-text-muted">{t('sso.previewBody')}</p>
    </div>
  );
}

/** Named states, never a stack trace: pass / warn / fail / skipped. */
function CheckList({ checks }: { checks: DiscoveryCheck[] }) {
  const { t } = useTranslation();
  return (
    <ul className="space-y-1.5">
      {checks.map((check, index) => (
        <li key={`${check.name}-${index}`} className="flex flex-wrap items-baseline gap-2">
          <Stamp tone={check.state === 'pass' ? 'online' : check.state === 'fail' ? 'alert' : 'due'}>
            {t(`sso.state.${check.state}`)}
          </Stamp>
          <span className="cap cap-sm text-text-primary">{t(`sso.check.${check.name}`, check.name)}</span>
          {check.detail && (
            <span className="ow-num min-w-0 flex-1 truncate text-[12px] text-text-muted" title={check.detail}>
              {check.detail}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}
