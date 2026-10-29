import { useCallback, useEffect, useMemo, useState } from 'react';
import { Plus, Pencil, Trash2, Gauge, FlaskConical } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import {
  createThreshold,
  deleteThreshold,
  getThresholds,
  previewThresholds,
  simulateThreshold,
  updateThreshold,
  type ThresholdDefault,
  type ThresholdForm,
  type ThresholdMetric,
  type ThresholdPreview,
  type ThresholdRule,
  type ThresholdScope,
  type ThresholdSimulation,
} from '../api/thresholds';
import { getDevices } from '../api/devices';
import { getClients } from '../api/clients';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';

const METRICS: ThresholdMetric[] = ['cpu', 'ram', 'disk'];
const SCOPES: ThresholdScope[] = ['device', 'tag', 'client', 'global'];

interface FormState {
  id: string;
  scope_type: ThresholdScope;
  scope_value: string;
  metric: ThresholdMetric;
  warning: string;
  critical: string;
  dedup_window_seconds: string;
  enabled: boolean;
}

/** What a rule points at, in words. The server names a global rule in English;
    the console owns the phrase, because it is the console that speaks the user's
    language. */
function scopeLabel(rule: ThresholdRule, globalName: string) {
  if (rule.scope_type === 'global') return globalName;
  return rule.scope_name || rule.scope_value || '—';
}

const emptyForm: FormState = {
  id: '',
  scope_type: 'client',
  scope_value: '',
  metric: 'cpu',
  warning: '80',
  critical: '90',
  dedup_window_seconds: '0',
  enabled: true,
};

/**
 * Threshold rules (2.4-5).
 *
 * One ledger of the levels a unit is measured against, the order they win in, and
 * a preview that answers "why did this unit alert at 62?" without reading a ledger.
 */
export function Thresholds() {
  const { t } = useTranslation();
  const [rules, setRules] = useState<ThresholdRule[]>([]);
  const [defaults, setDefaults] = useState<ThresholdDefault[]>([]);
  const [devices, setDevices] = useState<{ id: string; name: string }[]>([]);
  const [clients, setClients] = useState<{ id: string; name: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [formError, setFormError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const [previewDevice, setPreviewDevice] = useState('');
  const [preview, setPreview] = useState<ThresholdPreview | null>(null);
  const [previewBusy, setPreviewBusy] = useState(false);
  const [simMetric, setSimMetric] = useState<ThresholdMetric>('cpu');
  const [simValue, setSimValue] = useState('85');
  const [sim, setSim] = useState<ThresholdSimulation | null>(null);
  const guide = useGuideWords(rules.length);

  const load = useCallback(async () => {
    try {
      const [thresholds, deviceData] = await Promise.all([getThresholds(), getDevices()]);
      setRules(thresholds.rules);
      setDefaults(thresholds.defaults);
      setDevices(deviceData.map((d) => ({ id: d.id, name: d.name })));
      try {
        const tenants = await getClients();
        setClients(tenants.map((c) => ({ id: c.id, name: c.name })));
      } catch {
        // A manager without tenant-admin rights has no client list: the scope
        // picker keeps the tenants its rules already name.
        setClients([]);
      }
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const scopeOptions = useMemo(() => {
    if (form.scope_type === 'device') return devices;
    if (form.scope_type === 'client') return clients;
    return [];
  }, [form.scope_type, devices, clients]);

  const openCreate = () => {
    setForm({ ...emptyForm, scope_value: devices[0]?.id ?? clients[0]?.id ?? '' });
    setFormError('');
    setShowForm(true);
  };

  const openEdit = (rule: ThresholdRule) => {
    setForm({
      id: rule.id,
      scope_type: rule.scope_type,
      scope_value: rule.scope_value,
      metric: rule.metric,
      warning: String(rule.warning),
      critical: String(rule.critical),
      dedup_window_seconds: String(rule.dedup_window_seconds),
      enabled: rule.enabled,
    });
    setFormError('');
    setShowForm(true);
  };

  const handleSave = async () => {
    const warning = Number(form.warning);
    const critical = Number(form.critical);
    if (Number.isNaN(warning) || Number.isNaN(critical)) {
      setFormError(t('thresholds.needNumbers'));
      return;
    }
    const payload: ThresholdForm = {
      scope_type: form.scope_type,
      scope_value: form.scope_type === 'global' ? '' : form.scope_value,
      metric: form.metric,
      warning,
      critical,
      dedup_window_seconds: Number(form.dedup_window_seconds) || 0,
      enabled: form.enabled,
    };
    setSaving(true);
    setFormError('');
    try {
      if (form.id) {
        // The scope is immutable server-side; the form locks it too.
        await updateThreshold(form.id, {
          warning: payload.warning,
          critical: payload.critical,
          dedup_window_seconds: payload.dedup_window_seconds,
          enabled: payload.enabled,
        });
      } else {
        await createThreshold(payload);
      }
      setShowForm(false);
      setForm(emptyForm);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async (rule: ThresholdRule, enabled: boolean) => {
    setFormError('');
    try {
      await updateThreshold(rule.id, { enabled });
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const handleDelete = async (rule: ThresholdRule) => {
    setFormError('');
    try {
      await deleteThreshold(rule.id);
      setPending(null);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setPending(null);
    }
  };

  const handlePreview = async (deviceId: string) => {
    if (!deviceId) return;
    setPreviewBusy(true);
    setFormError('');
    try {
      const res = await previewThresholds(deviceId);
      setPreview(res.data);
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setPreview(null);
    } finally {
      setPreviewBusy(false);
    }
  };

  const handleSimulate = async () => {
    if (!previewDevice) return;
    setFormError('');
    try {
      const res = await simulateThreshold(previewDevice, simMetric, Number(simValue));
      setSim(res.data);
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setSim(null);
    }
  };

  if (loading) {
    return <div className="cap cap-sm p-6 text-text-muted">{t('common.loading')}</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('thresholds.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">{t('thresholds.subtitle')}</p>
        </div>
        <button onClick={openCreate} className="btn-primary">
          <Plus className="h-3.5 w-3.5" />
          {t('thresholds.addRule')}
        </button>
      </div>

      {formError && (
        <div role="alert" className="panel px-3 py-2">
          <span className="cap cap-sm text-[var(--stamp)]">{formError}</span>
        </div>
      )}

      {/* The floor every unit falls back to. Stated, so "no rule" is an answer an
          operator can read rather than a number they have to remember. */}
      <div className="panel px-4 py-3">
        <span className="cap cap-sm panel-head">{t('thresholds.floor')}</span>
        <div className="mt-2 flex flex-wrap gap-4">
          {defaults.map((d) => (
            <span key={d.metric} className="cap cap-sm text-text-secondary">
              {t(`thresholds.metric.${d.metric}`)}{' '}
              <span className="ow-num text-text-primary">{d.warning}/{d.critical}</span>
            </span>
          ))}
        </div>
        <p className="cap cap-sm mt-2 text-text-muted">{t('thresholds.floorBody')}</p>
      </div>

      {showForm && (
        <form
          className="panel p-4"
          aria-label={form.id ? t('thresholds.editRule') : t('thresholds.addRule')}
          onSubmit={(e) => {
            e.preventDefault();
            void handleSave();
          }}
        >
          <h2 className="panel-head">{form.id ? t('thresholds.editRule') : t('thresholds.addRule')}</h2>
          <p className="cap cap-sm mt-1 text-text-muted">{t('thresholds.formBody')}</p>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <label className="block">
              <span className="field-label">{t('thresholds.scope')}</span>
              <select
                value={form.scope_type}
                onChange={(e) => {
                  const scope = e.target.value as ThresholdScope;
                  setForm({
                    ...form,
                    scope_type: scope,
                    scope_value: scope === 'device' ? devices[0]?.id ?? '' : scope === 'client' ? clients[0]?.id ?? '' : '',
                  });
                }}
                disabled={form.id !== ''}
                className="select mt-1 w-full"
              >
                {SCOPES.map((scope) => (
                  <option key={scope} value={scope}>
                    {t(`thresholds.scopeKind.${scope}`)}
                  </option>
                ))}
              </select>
              {form.id !== '' && (
                <span className="cap cap-sm mt-1 block text-text-muted">{t('thresholds.scopeLocked')}</span>
              )}
            </label>

            {form.scope_type === 'tag' && (
              <label className="block">
                <span className="field-label">{t('thresholds.tag')}</span>
                <input
                  type="text"
                  value={form.scope_value}
                  onChange={(e) => setForm({ ...form, scope_value: e.target.value })}
                  className="input mt-1 w-full"
                  placeholder="prod"
                />
              </label>
            )}
            {form.scope_type !== 'tag' && form.scope_type !== 'global' && (
              <label className="block">
                <span className="field-label">
                  {form.scope_type === 'device' ? t('thresholds.device') : t('thresholds.client')}
                </span>
                <select
                  value={form.scope_value}
                  onChange={(e) => setForm({ ...form, scope_value: e.target.value })}
                  disabled={form.id !== ''}
                  className="select mt-1 w-full"
                >
                  {scopeOptions.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {form.scope_type === 'global' && (
              <div className="cap cap-sm text-text-muted self-end">{t('thresholds.globalBody')}</div>
            )}

            <label className="block">
              <span className="field-label">{t('thresholds.metricLabel')}</span>
              <select
                value={form.metric}
                onChange={(e) => setForm({ ...form, metric: e.target.value as ThresholdMetric })}
                disabled={form.id !== ''}
                className="select mt-1 w-full"
              >
                {METRICS.map((metric) => (
                  <option key={metric} value={metric}>
                    {t(`thresholds.metric.${metric}`)}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              <span className="field-label">{t('thresholds.warning')}</span>
              <input
                type="number"
                step="0.5"
                value={form.warning}
                onChange={(e) => setForm({ ...form, warning: e.target.value })}
                className="input mt-1 w-full"
              />
            </label>
            <label className="block">
              <span className="field-label">{t('thresholds.critical')}</span>
              <input
                type="number"
                step="0.5"
                value={form.critical}
                onChange={(e) => setForm({ ...form, critical: e.target.value })}
                className="input mt-1 w-full"
              />
            </label>
            <label className="block">
              <span className="field-label">{t('thresholds.dedup')}</span>
              <input
                type="number"
                value={form.dedup_window_seconds}
                onChange={(e) => setForm({ ...form, dedup_window_seconds: e.target.value })}
                className="input mt-1 w-full"
              />
              <span className="cap cap-sm mt-1 block text-text-muted">{t('thresholds.dedupBody')}</span>
            </label>
            <label className="flex items-center gap-2 self-end">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                className="h-4 w-4"
                style={{ accentColor: 'var(--online)' }}
              />
              <span className="cap cap-sm">{t('thresholds.enabled')}</span>
            </label>
          </div>
          <div className="mt-4 flex flex-wrap gap-3">
            <button type="submit" disabled={saving} className="btn-primary">
              {saving ? t('common.loading') : t('common.save')}
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="btn">
              {t('common.cancel')}
            </button>
          </div>
        </form>
      )}

      <section className="panel overflow-hidden" aria-label={t('thresholds.title')}>
        <GuideWordsHead
          total={rules.length}
          unit={t('sheet.inView')}
          first={rules[guide.first]?.scope_name}
          last={rules[guide.last]?.scope_name}
        />
        <div ref={guide.ref} className="max-h-[24rem] overflow-y-auto overflow-x-auto">
          <table className="ledger min-w-[720px]">
            <thead>
              <tr>
                <th className="w-40">{t('thresholds.scope')}</th>
                <th className="w-24">{t('thresholds.metricLabel')}</th>
                <th className="w-28">{t('thresholds.levels')}</th>
                <th className="w-28">{t('thresholds.dedup')}</th>
                <th className="w-24">{t('thresholds.state')}</th>
                <th className="w-36 text-right">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {rules.length === 0 && (
                <tr>
                  <td colSpan={6} className="cap cap-sm py-6 text-center text-text-muted">
                    {t('thresholds.none')}
                  </td>
                </tr>
              )}
              {rules.map((rule) => (
                <tr key={rule.id} data-guide title={rule.scope_name ?? rule.scope_value}>
                  <td>
                    <Stamp tone={rule.scope_type === 'global' ? 'alert' : rule.scope_type === 'device' ? 'service' : 'neutral'}>
                      {t(`thresholds.scopeKind.${rule.scope_type}`)}
                    </Stamp>
                    <span className="cap cap-sm block truncate text-text-primary" title={scopeLabel(rule, t('thresholds.globalName'))}>
                      {scopeLabel(rule, t('thresholds.globalName'))}
                    </span>
                  </td>
                  <td>
                    <span className="cap cap-sm">{t(`thresholds.metric.${rule.metric}`)}</span>
                  </td>
                  <td>
                    <span className="ow-num text-[12px]">
                      {rule.warning} / {rule.critical}
                    </span>
                  </td>
                  <td>
                    <span className="ow-num text-[12px] text-text-secondary">
                      {rule.dedup_window_seconds > 0 ? `${rule.dedup_window_seconds}s` : t('thresholds.dedupDefault')}
                    </span>
                  </td>
                  <td>
                    <label className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={rule.enabled}
                        disabled={rule.editable === false}
                        onChange={(e) => void handleToggle(rule, e.target.checked)}
                        className="h-4 w-4"
                        style={{ accentColor: 'var(--online)' }}
                        aria-label={t('thresholds.enabled')}
                      />
                      <Stamp tone={rule.enabled ? 'online' : 'neutral'}>
                        {rule.enabled ? t('common.enabled') : t('common.disabled')}
                      </Stamp>
                    </label>
                  </td>
                  <td className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <button
                        onClick={() => openEdit(rule)}
                        disabled={rule.editable === false}
                        className="btn-quiet btn"
                        title={t('thresholds.editRule')}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                        <span className="sr-only">{t('thresholds.editRule')}</span>
                      </button>
                      <button
                        onClick={() =>
                          setPending(
                            pendingConfirm(t, 'deleteThreshold', () => void handleDelete(rule), {
                              scope: t(`thresholds.scopeKind.${rule.scope_type}`),
                              target: rule.scope_name || rule.scope_value,
                              metric: t(`thresholds.metric.${rule.metric}`),
                            })
                          )
                        }
                        disabled={rule.editable === false}
                        className="btn-quiet btn text-[var(--stamp)]"
                        title={t('common.delete')}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        <span className="sr-only">{t('common.delete')}</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* The precedence preview: what applies to one unit, per metric, and every
          rule that was in the running. The question it answers is "why did this
          unit alert at 62?". */}
      <section className="panel p-4" aria-label={t('thresholds.previewTitle')}>
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <h2 className="panel-head">{t('thresholds.previewTitle')}</h2>
            <p className="cap cap-sm mt-1 text-text-muted">{t('thresholds.previewBody')}</p>
          </div>
          <label className="block ml-auto">
            <span className="field-label">{t('thresholds.device')}</span>
            <select
              value={previewDevice}
              onChange={(e) => {
                setPreviewDevice(e.target.value);
                setSim(null);
                void handlePreview(e.target.value);
              }}
              className="select mt-1 w-full"
              aria-label={t('thresholds.previewDeviceLabel')}
            >
              <option value="">{t('thresholds.pickDevice')}</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          </label>
        </div>

        {preview && (
          <div className="mt-4 space-y-3">
            {preview.metrics.map((entry) => (
              <div key={entry.metric} className="border border-rule p-3" data-metric-block={entry.metric}>
                <div className="flex flex-wrap items-baseline gap-2" data-applies={entry.metric} data-testid={`applies-${entry.metric}`}>
                  <Gauge className="h-3.5 w-3.5 text-text-muted" aria-hidden="true" />
                  <span className="cap cap-sm text-text-primary">
                    {t(`thresholds.metric.${entry.metric}`)}
                  </span>
                  <span className="ow-num text-[12px]" data-metric={entry.metric}>
                    {entry.applies.warning} / {entry.applies.critical}
                  </span>
                  <Stamp tone={entry.applies.source === 'built-in default' ? 'neutral' : 'service'}>
                    {entry.applies.source === 'built-in default'
                      ? t('thresholds.floor')
                      : t(`thresholds.scopeKind.${entry.applies.scope_type}`)}
                  </Stamp>
                  {entry.applies.scope_name || entry.applies.scope_value ? (
                    <span className="cap cap-sm text-text-muted">
                      {entry.applies.scope_name || entry.applies.scope_value}
                    </span>
                  ) : null}
                </div>
                <ul className="mt-2 space-y-1" data-chain={entry.metric} data-testid={`chain-${entry.metric}`}>
                  {entry.candidates.map((candidate, index) => (
                    <li
                      key={`${candidate.id}-${index}`}
                      className="cap cap-sm flex flex-wrap items-baseline gap-2 text-text-secondary"
                    >
                      <span className="ow-num text-[11px] text-text-muted">{index + 1}</span>
                      <Stamp tone={index === 0 ? 'service' : 'neutral'}>
                        {candidate.source === 'built-in default'
                          ? t('thresholds.floor')
                          : t(`thresholds.scopeKind.${candidate.scope_type}`)}
                      </Stamp>
                      <span className="ow-num">{candidate.warning} / {candidate.critical}</span>
                      {candidate.scope_value && (
                        <span className="truncate text-text-muted">{candidate.scope_value}</span>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            ))}

            <div className="flex flex-wrap items-end gap-3 border border-rule p-3">
              <div>
                <h3 className="cap cap-sm panel-head flex items-center gap-2">
                  <FlaskConical className="h-3.5 w-3.5" aria-hidden="true" />
                  {t('thresholds.simulateTitle')}
                </h3>
                <p className="cap cap-sm mt-1 text-text-muted">{t('thresholds.simulateBody')}</p>
              </div>
              <label className="block">
                <span className="field-label">{t('thresholds.metricLabel')}</span>
                <select
                  value={simMetric}
                  onChange={(e) => setSimMetric(e.target.value as ThresholdMetric)}
                  className="select mt-1"
                >
                  {METRICS.map((metric) => (
                    <option key={metric} value={metric}>
                      {t(`thresholds.metric.${metric}`)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="block">
                <span className="field-label">{t('thresholds.simulateValue')}</span>
                <input
                  type="number"
                  step="0.5"
                  value={simValue}
                  onChange={(e) => setSimValue(e.target.value)}
                  className="input mt-1 w-24"
                />
              </label>
              <button onClick={() => void handleSimulate()} className="btn">
                {t('thresholds.simulateGo')}
              </button>
              {sim && (
                <div className="min-w-0 flex-1" role="status">
                  <div className="flex flex-wrap items-baseline gap-2">
                    <Stamp tone={sim.would_fire ? (sim.severity === 'critical' ? 'alert' : 'due') : 'online'}>
                      {sim.would_fire
                        ? t(`alerts.${sim.severity}`)
                        : t('thresholds.simulateQuiet')}
                    </Stamp>
                    <span className="cap cap-sm text-text-primary">{sim.message}</span>
                  </div>
                  <p className="cap cap-sm mt-1 text-text-muted">
                    {t('thresholds.simulateFrom', {
                      scope: t(`thresholds.scopeKind.${sim.scope_type}`),
                      target:
                        sim.scope_type === 'global'
                          ? t('thresholds.floor')
                          : `${sim.scope_name || sim.scope_value} · ${sim.warning}/${sim.critical}`,
                    })}
                  </p>
                </div>
              )}
            </div>
          </div>
        )}
        {previewBusy && <p className="cap cap-sm mt-3 text-text-muted">{t('common.loading')}</p>}
      </section>

      <ConfirmSheet
        request={pending?.request ?? null}
        onCancel={() => setPending(null)}
        onConfirm={() => pending?.run()}
      />
    </div>
  );
}
