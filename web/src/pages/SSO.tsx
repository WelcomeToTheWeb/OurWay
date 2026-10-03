import { useState, useEffect } from 'react';
import { Plus, Trash2, KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { listSSOProvidersAdmin, createSSOProvider, deleteSSOProvider, type SSOProviderFull } from '../api/sso';

const PROVIDERS = [
  { name: 'google', label: 'Google', type: 'oauth2' },
  { name: 'microsoft', label: 'Microsoft', type: 'oauth2' },
  { name: 'apple', label: 'Apple', type: 'oauth2' },
];

export function SSO() {
  const { t } = useTranslation();
  const [providers, setProviders] = useState<SSOProviderFull[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<Partial<SSOProviderFull>>({
    name: 'google',
    type: 'oauth2',
    client_id: '',
    client_secret: '',
  });

  async function loadProviders() {
    try {
      const p = await listSSOProvidersAdmin();
      setProviders(p);
    } catch (err) {
      console.error('Failed to load SSO providers', err);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadProviders();
  }, []);

  async function handleCreate() {
    if (!form.name || !form.client_id) return;
    try {
      await createSSOProvider(form);
      setShowForm(false);
      setForm({ name: 'google', type: 'oauth2', client_id: '', client_secret: '' });
      loadProviders();
    } catch (err) {
      console.error('Failed to create SSO provider', err);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm(t('sso.deleteConfirm'))) return;
    try {
      await deleteSSOProvider(id);
      loadProviders();
    } catch (err) {
      console.error('Failed to delete SSO provider', err);
    }
  }

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">{t('sso.title')}</h1>
          <p className="text-sm text-text-secondary">{t('sso.subtitle')}</p>
        </div>
        <button
          onClick={() => setShowForm(!showForm)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-dark"
        >
          <Plus className="h-4 w-4" />
          {t('sso.addProvider')}
        </button>
      </div>

      {showForm && (
        <div className="mb-6 rounded-2xl border border-bg-border bg-bg-card p-6">
          <h2 className="mb-4 text-lg font-semibold text-text-primary">{t('sso.addProviderTitle')}</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">{t('sso.provider')}</label>
              <select
                value={form.name}
                onChange={(e) => {
                  const p = PROVIDERS.find((p) => p.name === e.target.value);
                  setForm({ ...form, name: e.target.value, type: p?.type });
                }}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
              >
                {PROVIDERS.map((p) => (
                  <option key={p.name} value={p.name}>
                    {t(`sso.${p.name}`)}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">{t('sso.clientId')}</label>
              <input
                type="text"
                value={form.client_id || ''}
                onChange={(e) => setForm({ ...form, client_id: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                placeholder={t('sso.clientIdPlaceholder')}
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">{t('sso.clientSecret')}</label>
              <input
                type="password"
                value={form.client_secret || ''}
                onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                placeholder={t('sso.clientSecretPlaceholder')}
              />
            </div>
            <div className="flex items-end">
              <button
                onClick={handleCreate}
                className="w-full rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent-dark"
              >
                {t('common.save')}
              </button>
            </div>
          </div>
        </div>
      )}

      {loading ? (
        <p className="text-sm text-text-secondary">{t('common.loading')}</p>
      ) : providers.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-bg-border py-12">
          <KeyRound className="mb-3 h-10 w-10 text-text-muted" />
          <p className="text-sm text-text-secondary">{t('sso.none')}</p>
          <p className="text-xs text-text-muted">{t('sso.noneDesc')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {providers.map((p) => (
            <div key={p.id} className="flex items-center justify-between rounded-2xl border border-bg-border bg-bg-card p-4">
              <div className="flex items-center gap-3">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent/10">
                  <KeyRound className="h-5 w-5 text-accent" />
                </div>
                <div>
                  <p className="font-medium text-text-primary capitalize">{p.name}</p>
                  <p className="text-xs text-text-secondary">{t('sso.clientInfo', { id: p.client_id })}</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <span
                  className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                    p.enabled ? 'bg-status-online/15 text-status-online' : 'bg-bg-border text-text-secondary'
                  }`}
                >
                  {p.enabled ? t('common.enabled') : t('common.disabled')}
                </span>
                <button
                  onClick={() => handleDelete(p.id)}
                  className="rounded-lg p-2 text-text-secondary hover:bg-status-error/15 hover:text-status-error"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
