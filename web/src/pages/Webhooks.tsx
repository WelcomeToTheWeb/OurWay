import { useState, useEffect } from 'react';
import { Plus, Trash2, Webhook, TestTube2, RefreshCw, ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  createWebhook,
  listWebhooks,
  updateWebhook,
  deleteWebhook,
  testWebhook,
  listWebhookDeliveries,
  retryWebhookDelivery,
  type Webhook as WebhookType,
  type WebhookDelivery,
} from '../api/webhooks';

const EVENT_TYPES = [
  { value: 'device_registered', key: 'deviceRegistered' },
  { value: 'device_online', key: 'deviceOnline' },
  { value: 'device_offline', key: 'deviceOffline' },
  { value: 'alert_created', key: 'alertCreated' },
  { value: 'alert_resolved', key: 'alertResolved' },
  { value: 'patch_deployed', key: 'patchDeployed' },
  { value: 'session_started', key: 'sessionStarted' },
  { value: 'session_frame', key: 'sessionFrame' },
];

function parseEvents(events: string): string {
  try {
    return JSON.parse(events).join(', ');
  } catch {
    return events;
  }
}

export function Webhooks() {
  const { t } = useTranslation();
  const [webhooks, setWebhooks] = useState<WebhookType[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [selectedWebhook, setSelectedWebhook] = useState<WebhookType | null>(null);
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [form, setForm] = useState({
    name: '',
    url: '',
    events: [] as string[],
    enabled: true,
  });
  const [testing, setTesting] = useState<string | null>(null);

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
    if (!form.name || !form.url || form.events.length === 0) return;
    try {
      await createWebhook(form);
      setShowForm(false);
      setForm({ name: '', url: '', events: [], enabled: true });
      loadWebhooks();
    } catch (err) {
      console.error('Failed to create webhook', err);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm(t('webhooks.deleteConfirm'))) return;
    try {
      await deleteWebhook(id);
      loadWebhooks();
    } catch (err) {
      console.error('Failed to delete webhook', err);
    }
  }

  async function handleTest(id: string) {
    setTesting(id);
    try {
      await testWebhook(id);
    } catch (err) {
      console.error('Failed to test webhook', err);
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
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">{t('webhooks.title')}</h1>
          <p className="text-sm text-text-secondary">{t('webhooks.subtitle')}</p>
        </div>
        <button
          onClick={() => setShowForm(!showForm)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary hover:bg-accent-dark"
        >
          <Plus className="h-4 w-4" />
          {t('webhooks.addWebhook')}
        </button>
      </div>

      {showForm && (
        <div className="mb-6 rounded-2xl border border-bg-border bg-bg-card p-6">
          <h2 className="mb-4 text-lg font-semibold text-text-primary">{t('webhooks.addWebhook')}</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">{t('common.name')}</label>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                placeholder={t('webhooks.namePlaceholder')}
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">{t('webhooks.url')}</label>
              <input
                type="url"
                value={form.url}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                placeholder="https://example.com/webhook"
              />
            </div>
          </div>
          <div className="mt-4">
            <label className="mb-2 block text-xs font-medium text-text-secondary">{t('webhooks.events')}</label>
            <div className="grid grid-cols-3 gap-2">
              {EVENT_TYPES.map((e) => (
                <label
                  key={e.value}
                  className="flex cursor-pointer items-center gap-2 rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary hover:bg-slate-800"
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
                    className="accent-accent"
                  />
                  {t(`webhooks.types.${e.key}`)}
                </label>
              ))}
            </div>
          </div>
          <button
            onClick={handleCreate}
            className="mt-4 w-full rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary hover:bg-accent-dark"
          >
            {t('common.save')}
          </button>
        </div>
      )}

      {loading ? (
        <p className="text-sm text-text-secondary">{t('common.loading')}</p>
      ) : webhooks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-bg-border py-12">
          <Webhook className="mb-3 h-10 w-10 text-text-muted" />
          <p className="text-sm text-text-secondary">{t('webhooks.none')}</p>
          <p className="text-xs text-text-muted">{t('webhooks.noneDesc')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {webhooks.map((w) => (
            <div key={w.id} className="flex items-center justify-between rounded-2xl border border-bg-border bg-bg-card p-4">
              <div className="flex items-center gap-3">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent/10">
                  <Webhook className="h-5 w-5 text-accent" />
                </div>
                <div>
                  <p className="font-medium text-text-primary">{w.name}</p>
                  <p className="text-xs text-text-secondary">{w.url}</p>
                  <p className="text-xs text-text-muted">
                    {t('webhooks.events')}: {parseEvents(w.events)}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => handleToggleEnabled(w)}
                  className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                    w.enabled ? 'bg-status-success/15 text-status-success' : 'bg-slate-700 text-text-secondary'
                  }`}
                >
                  {w.enabled ? t('common.enabled') : t('common.disabled')}
                </button>
                <button
                  onClick={() => handleTest(w.id)}
                  disabled={testing === w.id}
                  className="rounded-lg p-2 text-text-secondary hover:bg-accent/15 hover:text-accent"
                  title={t('webhooks.sendTestEvent')}
                >
                  {testing === w.id ? (
                    <RefreshCw className="h-4 w-4 animate-spin" />
                  ) : (
                    <TestTube2 className="h-4 w-4" />
                  )}
                </button>
                <button
                  onClick={() => handleViewDeliveries(w)}
                  className="rounded-lg p-2 text-text-secondary hover:bg-accent/15 hover:text-accent"
                  title={t('webhooks.viewDeliveries')}
                >
                  <ExternalLink className="h-4 w-4" />
                </button>
                <button
                  onClick={() => handleDelete(w.id)}
                  className="rounded-lg p-2 text-text-secondary hover:bg-status-error/15 hover:text-status-error"
                  title={t('common.delete')}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Delivery History Modal */}
      {selectedWebhook && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="w-full max-w-2xl rounded-2xl border border-bg-border bg-bg-card p-6">
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-lg font-semibold text-text-primary">
                {t('webhooks.deliveryHistory')} — {selectedWebhook.name}
              </h2>
              <button
                onClick={() => setSelectedWebhook(null)}
                className="text-text-secondary hover:text-text-primary"
              >
                {t('common.close')}
              </button>
            </div>
            <div className="max-h-96 overflow-y-auto">
              {deliveries.length === 0 ? (
                <p className="text-sm text-text-secondary">{t('webhooks.noDeliveries')}</p>
              ) : (
                <div className="space-y-2">
                  {deliveries.map((d) => (
                    <div
                      key={d.id}
                      className="flex items-center justify-between rounded-lg border border-bg-border bg-bg p-3"
                    >
                      <div>
                        <p className="text-sm font-medium text-text-primary">{d.event}</p>
                        <p className="text-xs text-text-secondary">
                          {new Date(d.created_at).toLocaleString()} — {t('webhooks.attempts', { count: d.attempts })}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <span
                          className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                            d.status === 'delivered'
                              ? 'bg-status-success/15 text-status-success'
                              : d.status === 'failed'
                              ? 'bg-status-error/15 text-status-error'
                              : 'bg-slate-700 text-text-secondary'
                          }`}
                        >
                          {d.status}
                        </span>
                        {d.status === 'failed' && (
                          <button
                            onClick={() => handleRetry(selectedWebhook.id, d.id)}
                            className="rounded-lg p-1.5 text-text-secondary hover:bg-accent/15 hover:text-accent"
                            title={t('webhooks.retry')}
                          >
                            <RefreshCw className="h-3.5 w-3.5" />
                          </button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
