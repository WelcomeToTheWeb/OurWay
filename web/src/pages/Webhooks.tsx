import { useState, useEffect } from 'react';
import { Plus, Trash2, Webhook, TestTube2, RefreshCw, ExternalLink, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  listWebhooks,
  createWebhook,
  deleteWebhook,
  updateWebhook,
  testWebhook,
  listWebhookDeliveries,
  retryWebhookDelivery,
  type Webhook as WebhookType,
  type WebhookDelivery,
} from '../api/webhooks';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';

const EVENT_TYPES = [
  { key: 'alert', value: 'alert' },
  { key: 'deviceOffline', value: 'device_offline' },
  { key: 'deviceOnline', value: 'device_online' },
  { key: 'patchDeployed', value: 'patch_deployed' },
  { key: 'patchDetected', value: 'patch_detected' },
  { key: 'rebootPending', value: 'reboot_pending' },
];

function parseEvents(events: string): string {
  try {
    const parsed = JSON.parse(events);
    return Array.isArray(parsed) ? parsed.join(', ') : events;
  } catch {
    return events;
  }
}

/**
 * Outbound webhooks as a ledger of the shop's signalling lines: where each
 * event goes, whether the line is open, and what the last deliveries did.
 */
export function Webhooks() {
  const { t } = useTranslation();
  const [webhooks, setWebhooks] = useState<WebhookType[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ name: '', url: '', events: ['alert'] });
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [selectedWebhook, setSelectedWebhook] = useState<WebhookType | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const guide = useGuideWords(webhooks.length);

  async function loadWebhooks() {
    try {
      const w = await listWebhooks();
      setWebhooks(w);
    } catch (err) {
      console.error('Failed to load webhooks', err);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadWebhooks();
  }, []);

  async function handleCreate() {
    if (!form.name || !form.url) return;
    setActionError(null);
    try {
      await createWebhook({
        name: form.name,
        url: form.url,
        events: form.events,
      });
      setShowForm(false);
      setForm({ name: '', url: '', events: ['alert'] });
      loadWebhooks();
    } catch (err) {
      setActionError((err as Error).message);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm(`${t('common.delete')} webhook?`)) return;
    try {
      await deleteWebhook(id);
      loadWebhooks();
    } catch (err) {
      console.error('Failed to delete webhook', err);
    }
  }

  async function handleTest(id: string) {
    setTesting(id);
    setActionError(null);
    try {
      await testWebhook(id);
    } catch (err) {
      setActionError(`Test delivery failed: ${(err as Error).message}`);
    } finally {
      setTesting(null);
    }
  }

  async function handleToggleEnabled(webhook: WebhookType) {
    try {
      await updateWebhook(webhook.id, { enabled: !webhook.enabled });
      loadWebhooks();
    } catch (err) {
      console.error('Failed to update webhook', err);
    }
  }

  async function handleViewDeliveries(webhook: WebhookType) {
    setSelectedWebhook(webhook);
    try {
      const d = await listWebhookDeliveries(webhook.id);
      setDeliveries(d);
    } catch (err) {
      console.error('Failed to load deliveries', err);
    }
  }

  async function handleRetry(webhookId: string, deliveryId: string) {
    try {
      await retryWebhookDelivery(webhookId, deliveryId);
      if (selectedWebhook) {
        handleViewDeliveries(selectedWebhook);
      }
    } catch (err) {
      console.error('Failed to retry delivery', err);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('webhooks.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">{t('webhooks.subtitle')}</p>
        </div>
        <button onClick={() => setShowForm(!showForm)} className="btn-primary">
          <Plus className="h-3.5 w-3.5" />
          {t('webhooks.addWebhook')}
        </button>
      </div>

      {actionError && (
        <div className="flex flex-wrap items-center justify-between gap-3 panel border-[var(--error)] px-4 py-3">
          <p className="text-sm text-[var(--error)]">{actionError}</p>
          <button onClick={() => setActionError(null)} className="btn-quiet btn">
            {t('common.dismiss')}
          </button>
        </div>
      )}

      {showForm && (
        <form
          className="panel p-4"
          onSubmit={(e) => {
            e.preventDefault();
            void handleCreate();
          }}
        >
          <h2 className="panel-head">{t('webhooks.addWebhook')}</h2>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="field-label">{t('common.name')}</span>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('webhooks.namePlaceholder')}
              />
            </label>
            <label className="block">
              <span className="field-label">{t('webhooks.url')}</span>
              <input
                type="url"
                value={form.url}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                className="input mt-1 w-full"
                placeholder="https://example.com/webhook"
              />
            </label>
          </div>
          <fieldset className="mt-3">
            <legend className="field-label">{t('webhooks.events')}</legend>
            <div className="mt-1 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {EVENT_TYPES.map((e) => (
                <label
                  key={e.value}
                  className="flex cursor-pointer items-center gap-2 border border-rule bg-[var(--field)] px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={form.events.includes(e.value)}
                    onChange={(ev) => {
                      if (ev.target.checked) {
                        setForm({ ...form, events: [...form.events, e.value] });
                      } else {
                        setForm({ ...form, events: form.events.filter((v) => v !== e.value) });
                      }
                    }}
                    className="h-4 w-4"
                    style={{ accentColor: 'var(--service)' }}
                  />
                  {t(`webhooks.types.${e.key}`)}
                </label>
              ))}
            </div>
          </fieldset>
          <button type="submit" className="btn-primary mt-3">
            {t('common.save')}
          </button>
        </form>
      )}

      {loading ? (
        <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
      ) : webhooks.length === 0 ? (
        <div className="panel px-4 py-6 text-center">
          <Webhook className="mx-auto h-5 w-5 text-text-muted" aria-hidden="true" />
          <p className="cap cap-sm mt-2 text-text-secondary">{t('webhooks.none')}</p>
          <p className="mt-1 text-sm text-text-muted">{t('webhooks.noneDesc')}</p>
        </div>
      ) : (
        <section className="panel overflow-hidden" aria-label={t('webhooks.title')}>
          <GuideWordsHead
            total={webhooks.length}
            unit={t('sheet.inView')}
            first={webhooks[guide.first]?.name}
            last={webhooks[guide.last]?.name}
          />
          <div ref={guide.ref} className="max-h-[30rem] overflow-y-auto">
            <table className="ledger">
              <thead>
                <tr>
                  <th>{t('common.name')}</th>
                  <th>{t('webhooks.url')}</th>
                  <th>{t('webhooks.events')}</th>
                  <th className="w-24">Line</th>
                  <th className="w-44 text-right"> </th>
                </tr>
              </thead>
              <tbody>
                {webhooks.map((w) => (
                  <tr key={w.id} data-guide title={w.name}>
                    <td>
                      <span className="cap cap-sm text-text-primary">{w.name}</span>
                    </td>
                    <td>
                      <span className="ow-num block truncate text-[12px]" title={w.url}>
                        {w.url}
                      </span>
                    </td>
                    <td>
                      <span className="cap cap-sm text-text-muted">{parseEvents(w.events)}</span>
                    </td>
                    <td>
                      <Stamp tone={w.enabled ? 'online' : 'neutral'}>
                        {w.enabled ? t('common.enabled') : t('common.disabled')}
                      </Stamp>
                    </td>
                    <td className="text-right">
                      <div className="flex justify-end gap-1.5">
                        <button
                          onClick={() => handleToggleEnabled(w)}
                          className="btn-quiet btn"
                          title={w.enabled ? 'Close line' : 'Open line'}
                        >
                          {w.enabled ? 'close' : 'open'}
                        </button>
                        <button
                          onClick={() => handleTest(w.id)}
                          disabled={testing === w.id}
                          className="btn-quiet btn"
                          title={t('webhooks.sendTestEvent')}
                        >
                          {testing === w.id ? (
                            <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <TestTube2 className="h-3.5 w-3.5" />
                          )}
                        </button>
                        <button
                          onClick={() => handleViewDeliveries(w)}
                          className="btn-quiet btn"
                          title={t('webhooks.viewDeliveries')}
                        >
                          <ExternalLink className="h-3.5 w-3.5" />
                        </button>
                        <button
                          onClick={() => handleDelete(w.id)}
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

      {/* Delivery history */}
      {selectedWebhook && (
        <div
          className="fixed inset-0 z-50 flex justify-end bg-[var(--scrim)] p-4"
          role="dialog"
          aria-modal="true"
          aria-label={t('webhooks.deliveryHistory')}
          onClick={() => setSelectedWebhook(null)}
        >
          <div
            className="flex h-full w-full max-w-xl flex-col panel p-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="panel-head mb-3 flex items-center justify-between">
              <span>
                {t('webhooks.deliveryHistory')} · {selectedWebhook.name}
              </span>
              <button
                onClick={() => setSelectedWebhook(null)}
                className="btn-quiet btn"
                aria-label={t('common.close')}
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {deliveries.length === 0 ? (
                <p className="cap cap-sm text-text-muted">{t('webhooks.noDeliveries')}</p>
              ) : (
                <table className="ledger">
                  <thead>
                    <tr>
                      <th>Event</th>
                      <th className="w-36">Stated</th>
                      <th className="w-20">Tries</th>
                      <th className="w-28">Result</th>
                      <th className="w-16 text-right"> </th>
                    </tr>
                  </thead>
                  <tbody>
                    {deliveries.map((d) => (
                      <tr key={d.id}>
                        <td>
                          <span className="cap cap-sm text-text-primary">{d.event}</span>
                        </td>
                        <td>
                          <span className="ow-num text-[12px] text-text-secondary">
                            {new Date(d.created_at).toISOString().slice(0, 16).replace('T', ' ')}
                          </span>
                        </td>
                        <td>
                          <span className="ow-num text-[12px]">{d.attempts}</span>
                        </td>
                        <td>
                          <Stamp
                            tone={
                              d.status === 'delivered' ? 'online' : d.status === 'failed' ? 'alert' : 'neutral'
                            }
                          >
                            {d.status}
                          </Stamp>
                        </td>
                        <td className="text-right">
                          {d.status === 'failed' && (
                            <button
                              onClick={() => handleRetry(selectedWebhook.id, d.id)}
                              className="btn-quiet btn"
                              title={t('webhooks.retry')}
                            >
                              <RefreshCw className="h-3.5 w-3.5" />
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
