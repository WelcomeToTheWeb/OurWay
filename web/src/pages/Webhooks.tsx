import { useCallback, useEffect, useMemo, useState } from 'react';
import { Plus, Pencil, Trash2, Send, X, RefreshCw, Mail, Webhook } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import {
  createChannel,
  deleteChannel,
  listChannelDeliveries,
  listChannels,
  parseChannelConfig,
  parseChannelEvents,
  retryChannelDelivery,
  testChannel,
  updateChannel,
  type Channel,
  type ChannelDelivery,
  type ChannelKind,
  type ChannelSeverity,
  type MailSummary,
} from '../api/channels';
import { getClients } from '../api/clients';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';

const KINDS: ChannelKind[] = ['webhook', 'slack', 'teams', 'email'];

const EVENT_TYPES = [
  'alert_created',
  'alert_resolved',
  'device_online',
  'device_offline',
  'device_registered',
  'patch_deployed',
  'session_started',
  'test',
];

const SEVERITY_FLOORS: ChannelSeverity[] = ['', 'info', 'warning', 'critical'];

interface FormState {
  id: string;
  kind: ChannelKind;
  name: string;
  url: string;
  recipients: string;
  min_severity: ChannelSeverity;
  scope_clients: string[];
  tags: string;
  events: string[];
  enabled: boolean;
}

const emptyForm: FormState = {
  id: '',
  kind: 'webhook',
  name: '',
  url: '',
  recipients: '',
  min_severity: '',
  scope_clients: [],
  tags: '',
  events: ['alert_created'],
  enabled: true,
};

function splitList(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((part) => part.trim())
    .filter(Boolean);
}

/** The destination in words: a mail channel names people, every other kind an
    endpoint. A mail channel has no URL, and the ledger says so. */
function destinationOf(channel: Channel, anyAddress: string) {
  if (channel.kind === 'email') {
    const to = parseChannelConfig(channel.config).to ?? [];
    return { primary: to.length ? to.join(', ') : anyAddress, secondary: channel.kind };
  }
  return { primary: channel.url, secondary: channel.kind };
}

/**
 * Notification channels (2.4-6).
 *
 * The webhook page generalized: one ledger for every way a fact about the fleet
 * reaches a person, with the routing (tenants, tags, severity floor) stated on
 * the row, and one delivery history behind all four kinds — including the test
 * send, which is queued through the same routing and lands in the same ledger.
 */
export function Webhooks() {
  const { t } = useTranslation();
  const [channels, setChannels] = useState<Channel[]>([]);
  const [mail, setMail] = useState<MailSummary>({ configured: false });
  const [clients, setClients] = useState<{ id: string; name: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [formError, setFormError] = useState('');
  const [actionNotice, setActionNotice] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const [selected, setSelected] = useState<Channel | null>(null);
  const [deliveries, setDeliveries] = useState<ChannelDelivery[]>([]);
  const guide = useGuideWords(channels.length);

  const load = useCallback(async () => {
    try {
      const { channels: rows, mail: transport } = await listChannels();
      setChannels(rows);
      setMail(transport);
      try {
        const tenants = await getClients();
        setClients(tenants.map((c) => ({ id: c.id, name: c.name })));
      } catch {
        // A manager without tenant-admin rights has no tenant list: the scope
        // picker keeps the tenants the stored channels already name.
        setClients([]);
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const scopeNames = useMemo(() => {
    const named = new Map(clients.map((c) => [c.id, c.name]));
    return (scope: string) => {
      let ids: string[] = [];
      try {
        const parsed = JSON.parse(scope);
        if (Array.isArray(parsed)) ids = parsed.map(String);
      } catch {
        ids = [];
      }
      if (ids.length === 0) return t('channels.scopeAll');
      return ids.map((id) => named.get(id) ?? id).join(', ');
    };
  }, [clients, t]);

  function openCreate() {
    setForm({ ...emptyForm });
    setFormError('');
    setShowForm(true);
  }

  function openEdit(channel: Channel) {
    setForm({
      id: channel.id,
      kind: channel.kind,
      name: channel.name,
      url: channel.url,
      recipients: (parseChannelConfig(channel.config).to ?? []).join(', '),
      min_severity: channel.min_severity || '',
      scope_clients: (() => {
        try {
          const parsed = JSON.parse(channel.scope_clients);
          return Array.isArray(parsed) ? parsed.map(String) : [];
        } catch {
          return [];
        }
      })(),
      tags: (() => {
        try {
          const parsed = JSON.parse(channel.tags);
          return Array.isArray(parsed) ? parsed.join(', ') : '';
        } catch {
          return '';
        }
      })(),
      events: parseChannelEvents(channel.events),
      enabled: channel.enabled,
    });
    setFormError('');
    setShowForm(true);
  }

  async function handleSave() {
    setSaving(true);
    setFormError('');
    const payload = {
      name: form.name,
      url: form.kind === 'email' ? '' : form.url,
      events: form.events,
      min_severity: form.min_severity,
      scope_clients: form.scope_clients,
      tags: splitList(form.tags),
      enabled: form.enabled,
      config: form.kind === 'email' ? { to: splitList(form.recipients) } : {},
    };
    try {
      if (form.id) {
        await updateChannel(form.id, payload);
      } else {
        await createChannel({ ...payload, kind: form.kind });
      }
      setShowForm(false);
      setForm(emptyForm);
      await load();
    } catch (err) {
      // The refusal stays on screen: a channel that was not saved must not look
      // like a channel that was.
      setFormError(err instanceof Error ? err.message : t('common.error'));
    } finally {
      setSaving(false);
    }
  }

  async function handleToggle(channel: Channel) {
    setFormError('');
    try {
      await updateChannel(channel.id, { enabled: !channel.enabled });
      await load();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
    }
  }

  async function handleTest(channel: Channel) {
    setTesting(channel.id);
    setFormError('');
    setActionNotice('');
    try {
      await testChannel(channel.id);
      setActionNotice(t('channels.testSent'));
      if (selected?.id === channel.id) void openDeliveries(channel);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
    } finally {
      setTesting(null);
    }
  }

  async function handleDelete(channel: Channel) {
    try {
      await deleteChannel(channel.id);
      setPending(null);
      if (selected?.id === channel.id) setSelected(null);
      await load();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
      setPending(null);
    }
  }

  async function openDeliveries(channel: Channel) {
    setSelected(channel);
    try {
      setDeliveries(await listChannelDeliveries(channel.id));
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
    }
  }

  async function handleRetry(delivery: ChannelDelivery) {
    if (!selected) return;
    try {
      await retryChannelDelivery(selected.id, delivery.id);
      await openDeliveries(selected);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('common.error'));
    }
  }


  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('channels.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">{t('channels.subtitle')}</p>
        </div>
        <button onClick={showForm ? () => setShowForm(false) : openCreate} className="btn-primary">
          <Plus className="h-3.5 w-3.5" />
          {t('channels.addChannel')}
        </button>
      </div>

      {formError && (
        <div
          role="alert"
          className="panel flex flex-wrap items-center justify-between gap-3 px-4 py-3"
          data-testid="channel-refusal"
        >
          <p className="text-sm text-[var(--error)]">{formError}</p>
          <button
            onClick={() => setFormError('')}
            className="btn-quiet btn"
            title={t('common.dismiss')}
          >
            {t('common.dismiss')}
          </button>
        </div>
      )}

      {actionNotice && (
        <p role="status" className="cap cap-sm text-text-secondary" data-testid="channel-notice">
          {actionNotice}
        </p>
      )}

      {showForm && (
        <form
          role="form"
          className="panel p-4"
          aria-label={form.id ? t('channels.editChannel') : t('channels.addChannel')}
          onSubmit={(e) => {
            e.preventDefault();
            void handleSave();
          }}
        >
          <h2 className="panel-head">{form.id ? t('channels.editChannel') : t('channels.addChannel')}</h2>
          <p className="cap cap-sm mt-1 text-text-muted">{t('channels.formBody')}</p>

          <fieldset className="mt-3">
            <legend className="field-label">{t('channels.kind')}</legend>
            <div className="mt-1 flex flex-wrap gap-2">
              {KINDS.map((kind) => (
                <label
                  key={kind}
                  className={`flex cursor-pointer items-center gap-2 border px-3 py-2 text-sm transition-colors ${
                    form.kind === kind
                      ? 'border-[var(--service)] bg-[var(--field)] text-text-primary'
                      : 'border-rule text-text-muted hover:bg-bg-secondary'
                  }`}
                >
                  <input
                    type="radio"
                    name="channel-kind"
                    value={kind}
                    checked={form.kind === kind}
                    onChange={() => setForm({ ...form, kind })}
                    className="h-4 w-4"
                    style={{ accentColor: 'var(--service)' }}
                  />
                  {t(`channels.kindLabel.${kind}`)}
                </label>
              ))}
            </div>
            <p className="cap cap-sm mt-2 text-text-muted">{t('channels.kindBody')}</p>
          </fieldset>

          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="field-label">{t('common.name')}</span>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="input mt-1 w-full"
                placeholder={t('channels.namePlaceholder')}
              />
            </label>
            {form.kind === 'email' ? (
              <label className="block">
                <span className="field-label">{t('channels.recipients')}</span>
                <input
                  type="text"
                  value={form.recipients}
                  onChange={(e) => setForm({ ...form, recipients: e.target.value })}
                  className="input mt-1 w-full"
                  placeholder="oncall@corp.example, netops@corp.example"
                />
                <span className="cap cap-sm mt-1 block text-text-muted">{t('channels.recipientsBody')}</span>
              </label>
            ) : (
              <label className="block">
                <span className="field-label">{t('channels.endpoint')}</span>
                <input
                  type="url"
                  value={form.url}
                  onChange={(e) => setForm({ ...form, url: e.target.value })}
                  className="input mt-1 w-full"
                  placeholder={t('channels.endpointPlaceholder')}
                />
              </label>
            )}
          </div>

          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
            <label className="block">
              <span className="field-label">{t('channels.minSeverity')}</span>
              <select
                value={form.min_severity}
                onChange={(e) => setForm({ ...form, min_severity: e.target.value as ChannelSeverity })}
                className="input mt-1 w-full"
              >
                {SEVERITY_FLOORS.map((floor) => (
                  <option key={floor || 'none'} value={floor}>
                    {t(floor ? `channels.floor.${floor}` : 'channels.floor.none')}
                  </option>
                ))}
              </select>
              <span className="cap cap-sm mt-1 block text-text-muted">{t('channels.minSeverityBody')}</span>
            </label>

            <label className="block">
              <span className="field-label">{t('channels.scope')}</span>
              <select
                multiple
                size={4}
                value={form.scope_clients}
                onChange={(e) =>
                  setForm({
                    ...form,
                    scope_clients: Array.from(e.target.selectedOptions).map((o) => o.value),
                  })
                }
                className="input mt-1 w-full"
                data-testid="channel-scope"
              >
                {clients.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              <span className="cap cap-sm mt-1 block text-text-muted">{t('channels.scopeBody')}</span>
            </label>

            <label className="block">
              <span className="field-label">{t('channels.tags')}</span>
              <input
                type="text"
                value={form.tags}
                onChange={(e) => setForm({ ...form, tags: e.target.value })}
                className="input mt-1 w-full"
                placeholder="prod, edge"
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('channels.tagsBody')}</span>
            </label>
          </div>

          <fieldset className="mt-3">
            <legend className="field-label">{t('channels.events')}</legend>
            <div className="mt-1 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {EVENT_TYPES.map((event) => (
                <label
                  key={event}
                  className="flex cursor-pointer items-center gap-2 border border-rule bg-[var(--field)] px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={form.events.includes(event)}
                    onChange={(ev) =>
                      setForm({
                        ...form,
                        events: ev.target.checked
                          ? [...form.events, event]
                          : form.events.filter((v) => v !== event),
                      })
                    }
                    className="h-4 w-4"
                    style={{ accentColor: 'var(--service)' }}
                  />
                  {t(`channels.types.${event}`)}
                </label>
              ))}
            </div>
          </fieldset>

          <div className="mt-3 flex items-center gap-3">
            <button type="submit" className="btn-primary" disabled={saving}>
              {t('common.save')}
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="btn-quiet btn">
              {t('common.cancel')}
            </button>
          </div>
        </form>
      )}

      {loading ? (
        <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
      ) : channels.length === 0 ? (
        <div className="panel px-4 py-6 text-center">
          <Webhook className="mx-auto h-5 w-5 text-text-muted" aria-hidden="true" />
          <p className="cap cap-sm mt-2 text-text-secondary">{t('channels.none')}</p>
          <p className="mt-1 text-sm text-text-muted">{t('channels.noneDesc')}</p>
        </div>
      ) : (
        <section className="panel overflow-hidden" aria-label={t('channels.title')}>
          <GuideWordsHead
            total={channels.length}
            unit={t('sheet.inView')}
            first={channels[guide.first]?.name}
            last={channels[guide.last]?.name}
          />
          <div ref={guide.ref} className="max-h-[30rem] overflow-y-auto">
            <table className="ledger">
              <thead>
                <tr>
                  <th>{t('common.name')}</th>
                  <th>{t('channels.kind')}</th>
                  <th>{t('channels.endpoint')}</th>
                  <th>{t('channels.routing')}</th>
                  <th>{t('channels.events')}</th>
                  <th className="w-24">{t('channels.state')}</th>
                  <th className="w-44 text-right"> </th>
                </tr>
              </thead>
              <tbody>
                {channels.map((channel) => {
                  const destination = destinationOf(channel, t('channels.recipients'));
                  return (
                    <tr key={channel.id} data-guide title={channel.name}>
                      <td>
                        <span className="cap cap-sm text-text-primary">{channel.name}</span>
                      </td>
                      <td>
                        <Stamp tone={channel.kind === 'email' ? 'due' : 'service'}>
                          {t(`channels.kindLabel.${channel.kind}`)}
                        </Stamp>
                      </td>
                      <td>
                        <span className="ow-num block truncate text-[12px]" title={destination.primary}>
                          {destination.primary}
                        </span>
                      </td>
                      <td>
                        <span className="cap cap-sm block text-text-primary">
                          {scopeNames(channel.scope_clients)}
                        </span>
                        <span className="cap cap-sm block text-text-muted">
                          {t(channel.min_severity ? `channels.floor.${channel.min_severity}` : 'channels.floor.none')}
                          {splitList(channel.tags.replace(/[[\]"]/g, ' ')).length > 0 &&
                            ` · ${splitList(channel.tags.replace(/[[\]"]/g, ' ')).join(', ')}`}
                        </span>
                      </td>
                      <td>
                        <span className="cap cap-sm text-text-muted">
                          {parseChannelEvents(channel.events)
                            .map((event) => t(`channels.types.${event}`))
                            .join(', ')}
                        </span>
                      </td>
                      <td>
                        <Stamp tone={channel.enabled ? 'online' : 'neutral'}>
                          {channel.enabled ? t('common.enabled') : t('common.disabled')}
                        </Stamp>
                      </td>
                      <td className="text-right">
                        <div className="flex justify-end gap-1.5">
                          <button
                            onClick={() => handleToggle(channel)}
                            className="btn-quiet btn"
                            title={channel.enabled ? t('common.disabled') : t('common.enabled')}
                          >
                            {channel.enabled ? t('common.disable') : t('common.enable')}
                          </button>
                          <button
                            onClick={() => void handleTest(channel)}
                            disabled={testing === channel.id}
                            className="btn-quiet btn"
                            title={t('channels.testSend')}
                          >
                            {testing === channel.id ? (
                              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <Send className="h-3.5 w-3.5" />
                            )}
                          </button>
                          <button
                            onClick={() => openEdit(channel)}
                            className="btn-quiet btn"
                            title={t('channels.editChannel')}
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </button>
                          <button
                            onClick={() => void openDeliveries(channel)}
                            className="btn-quiet btn"
                            title={t('channels.deliveries')}
                          >
                            <Mail className="h-3.5 w-3.5" />
                          </button>
                          <button
                            onClick={() =>
                              setPending(
                                pendingConfirm(t, 'deleteChannel', () => void handleDelete(channel), {
                                  name: channel.name,
                                  events: parseChannelEvents(channel.events)
                                    .map((event) => t(`channels.types.${event}`))
                                    .join(', '),
                                })
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
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      <p
        className="cap cap-sm text-text-muted"
        data-testid="mail-transport"
        role={mail.configured ? undefined : 'status'}
      >
        {mail.configured
          ? t('channels.mailReady', { host: mail.host ?? '', port: mail.port ?? 0, from: mail.sender ?? '' })
          : t('channels.mailNotice')}
      </p>

      {selected && (
        <div
          className="fixed inset-0 z-50 flex justify-end bg-[var(--scrim)] p-4"
          role="dialog"
          aria-modal="true"
          aria-label={t('channels.deliveries')}
          onClick={() => setSelected(null)}
        >
          <div
            className="panel flex h-full w-full max-w-2xl flex-col p-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="panel-head mb-3 flex items-center justify-between">
              <span>
                {t('channels.deliveries')} · {selected.name}
              </span>
              <button
                onClick={() => setSelected(null)}
                className="btn-quiet btn"
                aria-label={t('common.close')}
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {deliveries.length === 0 ? (
                <p className="cap cap-sm text-text-muted">{t('channels.noDeliveries')}</p>
              ) : (
                <table className="ledger">
                  <thead>
                    <tr>
                      <th>{t('channels.deliveryEvent')}</th>
                      <th className="w-36">{t('channels.deliveryStated')}</th>
                      <th className="w-20">{t('channels.deliveryTries')}</th>
                      <th className="w-28">{t('channels.deliveryResult')}</th>
                      <th>{t('channels.deliveryAnswer')}</th>
                      <th className="w-16 text-right"> </th>
                    </tr>
                  </thead>
                  <tbody>
                    {deliveries.map((delivery) => (
                      <tr key={delivery.id}>
                        <td>
                          <span className="cap cap-sm text-text-primary">
                            {t(`channels.types.${delivery.event}`, { defaultValue: delivery.event })}
                          </span>
                        </td>
                        <td>
                          <span className="ow-num text-[12px] text-text-secondary">
                            {new Date(delivery.created_at).toISOString().slice(0, 16).replace('T', ' ')}
                          </span>
                        </td>
                        <td>
                          <span className="ow-num text-[12px]">{delivery.attempts}</span>
                        </td>
                        <td>
                          <Stamp
                            tone={
                              delivery.status === 'delivered'
                                ? 'online'
                                : delivery.status === 'failed'
                                  ? 'alert'
                                  : 'neutral'
                            }
                          >
                            {delivery.status}
                          </Stamp>
                        </td>
                        <td>
                          <span className="cap cap-sm block text-text-muted" title={delivery.response_body}>
                            {delivery.status_code > 0 && (
                              <span className="ow-num mr-2 text-[12px]">{delivery.status_code}</span>
                            )}
                            {delivery.next_retry_at
                              ? `${delivery.response_body} · ${t('channels.nextRetry')} ${new Date(
                                  delivery.next_retry_at
                                )
                                  .toISOString()
                                  .slice(0, 16)
                                  .replace('T', ' ')}`
                              : delivery.response_body}
                          </span>
                        </td>
                        <td className="text-right">
                          {delivery.status === 'pending' && (
                            <button
                              onClick={() => void handleRetry(delivery)}
                              className="btn-quiet btn"
                              title={t('channels.retry')}
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

      <ConfirmSheet
        request={pending?.request ?? null}
        onCancel={() => setPending(null)}
        onConfirm={() => pending?.run()}
      />
    </div>
  );
}
