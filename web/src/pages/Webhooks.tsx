import { useState, useEffect } from 'react';
import { Plus, Trash2, Webhook, TestTube2, RefreshCw, ExternalLink } from 'lucide-react';
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
  { value: 'device_registered', label: 'Device Registered' },
  { value: 'device_online', label: 'Device Online' },
  { value: 'alert_created', label: 'Alert Created' },
  { value: 'alert_resolved', label: 'Alert Resolved' },
  { value: 'patch_deployed', label: 'Patch Deployed' },
  { value: 'session_started', label: 'Session Started' },
];

export function Webhooks() {
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
    if (!confirm('Delete this webhook?')) return;
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
          <h1 className="text-2xl font-bold text-text-primary">Webhooks</h1>
          <p className="text-sm text-text-secondary">Receive event notifications at your HTTP endpoint</p>
        </div>
        <button
          onClick={() => setShowForm(!showForm)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary hover:bg-accent-dark"
        >
          <Plus className="h-4 w-4" />
          Add Webhook
        </button>
      </div>

      {showForm && (
        <div className="mb-6 rounded-2xl border border-bg-border bg-bg-card p-6">
          <h2 className="mb-4 text-lg font-semibold text-text-primary">Add Webhook</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">Name</label>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                placeholder="Webhook name"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-text-secondary">URL</label>
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
            <label className="mb-2 block text-xs font-medium text-text-secondary">Events</label>
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
                  {e.label}
                </label>
              ))}
            </div>
          </div>
          <button
            onClick={handleCreate}
            className="mt-4 w-full rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary hover:bg-accent-dark"
          >
            Save
          </button>
        </div>
      )}

      {loading ? (
        <p className="text-sm text-text-secondary">Loading...</p>
      ) : webhooks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-bg-border py-12">
          <Webhook className="mb-3 h-10 w-10 text-text-muted" />
          <p className="text-sm text-text-secondary">No webhooks configured</p>
          <p className="text-xs text-text-muted">Add a webhook to receive event notifications</p>
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
                    Events: {JSON.parse(w.events).join(', ')}
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
                  {w.enabled ? 'Enabled' : 'Disabled'}
                </button>
                <button
                  onClick={() => handleTest(w.id)}
                  disabled={testing === w.id}
                  className="rounded-lg p-2 text-text-secondary hover:bg-accent/15 hover:text-accent"
                  title="Send test event"
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
                  title="View deliveries"
                >
                  <ExternalLink className="h-4 w-4" />
                </button>
                <button
                  onClick={() => handleDelete(w.id)}
                  className="rounded-lg p-2 text-text-secondary hover:bg-status-error/15 hover:text-status-error"
                  title="Delete"
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
                Delivery History — {selectedWebhook.name}
              </h2>
              <button
                onClick={() => setSelectedWebhook(null)}
                className="text-text-secondary hover:text-text-primary"
              >
                Close
              </button>
            </div>
            <div className="max-h-96 overflow-y-auto">
              {deliveries.length === 0 ? (
                <p className="text-sm text-text-secondary">No deliveries yet</p>
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
                          {new Date(d.created_at).toLocaleString()} — {d.attempts} attempt(s)
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
                            title="Retry"
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
