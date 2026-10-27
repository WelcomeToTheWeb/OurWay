import { useState, useEffect } from 'react';
import { Plus, Trash2, KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  listSSOProvidersAdmin,
  createSSOProvider,
  deleteSSOProvider,
  type SSOProviderFull,
} from '../api/sso';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';

const PROVIDERS = [
  { name: 'google', label: 'Google', type: 'oauth2' },
  { name: 'microsoft', label: 'Microsoft', type: 'oauth2' },
  { name: 'apple', label: 'Apple', type: 'oauth2' },
];

/**
 * SSO providers, kept as a ledger of doors: provider, the client id it is
 * registered under, and whether the door is open. The add form is a ruled
 * panel above the sheet, not a modal — nothing here needs a stage.
 */
export function SSO() {
  const { t } = useTranslation();
  const [providers, setProviders] = useState<SSOProviderFull[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const [form, setForm] = useState<Partial<SSOProviderFull>>({
    name: 'google',
    type: 'oauth2',
    client_id: '',
    client_secret: '',
  });
  const guide = useGuideWords(providers.length);

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
    if (!form.name || !form.client_id) {
      setFormError(t('sso.clientId'));
      return;
    }
    setSaving(true);
    setFormError(null);
    try {
      await createSSOProvider(form);
      setShowForm(false);
      setForm({ name: 'google', type: 'oauth2', client_id: '', client_secret: '' });
      loadProviders();
    } catch (err) {
      setFormError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete(p: SSOProviderFull) {
    try {
      await deleteSSOProvider(p.id);
      loadProviders();
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
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          <Plus className="h-3.5 w-3.5" />
          {t('sso.addProvider')}
        </button>
      </div>

      {showForm && (
        <form
          className="panel p-4"
          onSubmit={(e) => {
            e.preventDefault();
            void handleCreate();
          }}
        >
          <h2 className="panel-head">{t('sso.addProviderTitle')}</h2>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <label className="block">
              <span className="field-label">{t('sso.provider')}</span>
              <select
                value={form.name}
                onChange={(e) => {
                  const p = PROVIDERS.find((opt) => opt.name === e.target.value);
                  setForm({ ...form, name: e.target.value, type: p?.type });
                }}
                className="select mt-1 w-full"
              >
                {PROVIDERS.map((p) => (
                  <option key={p.name} value={p.name}>
                    {t(`sso.${p.name}`)}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              <span className="field-label">{t('sso.clientId')}</span>
              <input
                type="text"
                value={form.client_id || ''}
                onChange={(e) => setForm({ ...form, client_id: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('sso.clientIdPlaceholder')}
              />
            </label>
            <label className="block">
              <span className="field-label">{t('sso.clientSecret')}</span>
              <input
                type="password"
                value={form.client_secret || ''}
                onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('sso.clientSecretPlaceholder')}
              />
            </label>
            <div className="flex items-end gap-2">
              <button type="submit" disabled={saving} className="btn-primary w-full">
                {saving ? t('common.loading') : t('common.save')}
              </button>
            </div>
          </div>
          {formError && <p className="cap cap-sm mt-2 text-[var(--error)]">{formError}</p>}
        </form>
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
          <div ref={guide.ref} className="max-h-[26rem] overflow-y-auto">
          <table className="ledger">
            <thead>
              <tr>
                <th className="w-40">{t('sso.provider')}</th>
                <th>{t('sso.clientId')}</th>
                <th className="w-28">{t('common.enabled', 'State')}</th>
                <th className="w-24 text-right"> </th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.id} data-guide title={p.name}>
                  <td>
                    <span className="cap cap-sm text-text-primary">{p.name}</span>
                  </td>
                  <td>
                    <span className="ow-num block truncate text-[12px]" title={p.client_id}>
                      {p.client_id}
                    </span>
                  </td>
                  <td>
                    <Stamp tone={p.enabled ? 'online' : 'neutral'}>
                      {p.enabled ? t('common.enabled') : t('common.disabled')}
                    </Stamp>
                  </td>
                  <td className="text-right">
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
