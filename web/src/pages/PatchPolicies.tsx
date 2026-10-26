import { useEffect, useState, Fragment } from 'react';
import {
  Play,
  Shield,
  Plus,
  Activity,
  ChevronDown,
  RotateCcw,
} from 'lucide-react';
import {
  listPolicies,
  createPolicy,
  deletePolicy,
  listDeployments,
  deployNow,
  listDeploymentResults,
  rollbackDeployment,
} from '../api/patching';
import { useTranslation } from 'react-i18next';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { listTags, type TagCount } from '../api/tags';
import { useAuth } from '../auth/context';
import { browserTimeZone, formatWindow, timeZoneOptions } from '../utils/patchWindow';
import type { PatchPolicy, PatchDeployment, DeploymentResult } from '../types/patch';
import { isRateLimited } from '../api/client';

function timeAgo(dateStr: string): string {
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return 'Unknown';
    const diff = Date.now() - d.getTime();
    if (diff < 60000) return 'Just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    return d.toISOString().slice(0, 10);
  } catch {
    return 'Unknown';
  }
}

export function PatchPolicies() {
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const isAdmin = hasRole('admin');
  const [policies, setPolicies] = useState<PatchPolicy[]>([]);
  const [deployments, setDeployments] = useState<PatchDeployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newPolicy, setNewPolicy] = useState({
    name: '',
    scope: 'all',
    scope_value: '',
    schedule: 'weekly',
    window_start: '02:00',
    window_hours: 4,
    timezone: browserTimeZone(),
    auto_reboot: false,
    approval_required: true,
    max_devices_per_batch: 10,
  });
  const [knownTags, setKnownTags] = useState<TagCount[]>([]);
  useEffect(() => {
    listTags().then(setKnownTags).catch(() => setKnownTags([]));
  }, []);
  const [resultsFor, setResultsFor] = useState<string | null>(null);
  const guide = useGuideWords(deployments.length);
  const [results, setResults] = useState<DeploymentResult[]>([]);
  const [resultsLoading, setResultsLoading] = useState(false);
  const [rollingBack, setRollingBack] = useState<string | null>(null);

  async function load() {
    try {
      setLoading(true);
      const [p, d] = await Promise.all([listPolicies(), listDeployments()]);
      setPolicies(p);
      setDeployments(d);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
    const interval = setInterval(() => {
      if (!isRateLimited()) void load();
    }, 30000);
    return () => clearInterval(interval);
  }, []);

  async function toggleResults(deploymentId: string) {
    if (resultsFor === deploymentId) {
      setResultsFor(null);
      return;
    }
    setResultsFor(deploymentId);
    setResults([]);
    setResultsLoading(true);
    try {
      setResults(await listDeploymentResults(deploymentId));
    } catch {
      // ignore
    } finally {
      setResultsLoading(false);
    }
  }

  async function handleRollback(deploymentId: string) {
    setRollingBack(deploymentId);
    try {
      await rollbackDeployment(deploymentId);
      await load();
    } catch {
      // ignore
    } finally {
      setRollingBack(null);
    }
  }

  async function handleDelete(policy: PatchPolicy) {
    if (!window.confirm(`Delete policy"${policy.name}"? Queued deployments for it are dropped.`)) return;
    try {
      await deletePolicy(policy.id);
      await load();
    } catch {
      // ignore
    }
  }

  async function handleCreate() {
    if (!newPolicy.name) return;
    try {
      setCreating(true);
      await createPolicy(newPolicy);
      setShowCreate(false);
      setNewPolicy({
        name: '',
        scope: 'all',
        scope_value: '',
        schedule: 'weekly',
        window_start: '02:00',
        window_hours: 4,
        timezone: browserTimeZone(),
        auto_reboot: false,
        approval_required: true,
        max_devices_per_batch: 10,
      });
    } catch {
      // ignore
    } finally {
      setCreating(false);
    }
  }

  async function handleDeploy() {
    try {
      setCreating(true);
      await deployNow();
    } catch {
      // ignore
    } finally {
      setCreating(false);
    }
  }

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Activity className="h-8 w-8 animate-spin text-[var(--service)]" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      {/* Policies section */}
      <div>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-[26px]">{t('patchPolicies.title')}</h1>
            <p className="text-sm text-text-secondary">
              {t('patchPolicies.activePolicies', { count: policies.length })}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleDeploy}
              disabled={creating}
              className="flex items-center gap-1.5 rounded-lg btn btn-primary px-2.5 py-1.5 text-[11px] disabled:opacity-50"
            >
              <Play className="h-3.5 w-3.5" />
              {t('patches.deployNow')}
            </button>
            {isAdmin && (
              <button
                onClick={() => setShowCreate(!showCreate)}
                className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-bg"
              >
                <Plus className="h-3.5 w-3.5" />
                {t('patchPolicies.newPolicy')}
              </button>
            )}
          </div>
        </div>

        {showCreate && (
          <div className="mt-4 panel p-6">
            <h3 className="text-sm font-semibold text-text-primary">{t('patchPolicies.createPolicy')}</h3>
            <div className="mt-4 grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs text-text-secondary">{t('common.name')}</label>
                <input
                  type="text"
                  value={newPolicy.name}
                  onChange={(e) =>
                    setNewPolicy({ ...newPolicy, name: e.target.value })
                  }
                  placeholder={t('patchPolicies.namePlaceholder')}
                  className="input mt-1 w-full"
                />
              </div>
              <div>
                <label className="block text-xs text-text-secondary">{t('patchPolicies.schedule')}</label>
                <select
                  value={newPolicy.schedule}
                  onChange={(e) =>
                    setNewPolicy({ ...newPolicy, schedule: e.target.value })
                  }
                  className="input mt-1 w-full"
                >
                  <option value="daily">{t('patchPolicies.daily')}</option>
                  <option value="weekly">{t('patchPolicies.weekly')}</option>
                  <option value="monthly">{t('patchPolicies.monthly')}</option>
                </select>
              </div>
              <div>
                <label className="block text-xs text-text-secondary" htmlFor="policy-window-start">Window starts</label>
                <input
                  id="policy-window-start"
                  type="time"
                  value={newPolicy.window_start}
                  onChange={(e) => setNewPolicy({ ...newPolicy, window_start: e.target.value })}
                  className="input mt-1 w-full"
                />
              </div>
              <div>
                <label className="block text-xs text-text-secondary" htmlFor="policy-window-hours">Window length (hours)</label>
                <input
                  id="policy-window-hours"
                  type="number"
                  min={1}
                  max={24}
                  value={newPolicy.window_hours}
                  onChange={(e) => setNewPolicy({ ...newPolicy, window_hours: parseInt(e.target.value) || 4 })}
                  className="input mt-1 w-full"
                />
              </div>
              <div>
                <label className="block text-xs text-text-secondary" htmlFor="policy-timezone">Time zone</label>
                <input
                  id="policy-timezone"
                  list="policy-timezones"
                  value={newPolicy.timezone}
                  onChange={(e) => setNewPolicy({ ...newPolicy, timezone: e.target.value })}
                  className="input mt-1 w-full"
                />
                <datalist id="policy-timezones">
                  {timeZoneOptions().map((z) => <option key={z} value={z} />)}
                </datalist>
              </div>
              <div>
                <label className="block text-xs text-text-secondary">{t('patchPolicies.scope')}</label>
                <select
                  value={newPolicy.scope}
                  onChange={(e) =>
                    setNewPolicy({ ...newPolicy, scope: e.target.value })
                  }
                  className="input mt-1 w-full"
                >
                  <option value="all">{t('patchPolicies.allDevices')}</option>
                  <option value="tags">{t('patchPolicies.byTag')}</option>
                  <option value="devices">{t('patchPolicies.specificDevices')}</option>
                </select>
              </div>
              {newPolicy.scope !== 'all' && (
                <div>
                  <label className="block text-xs text-text-secondary" htmlFor="policy-scope-value">
                    {newPolicy.scope === 'tags'
                      ? t('patchPolicies.scopeTagsHelp')
                      : t('patchPolicies.scopeDevicesHelp')}
                  </label>
                  <input
                    id="policy-scope-value"
                    list="policy-tag-suggestions"
                    value={newPolicy.scope_value}
                    onChange={(e) => setNewPolicy({ ...newPolicy, scope_value: e.target.value })}
                    placeholder={newPolicy.scope === 'tags' ? 'prod, web' : 'server-01, laptop-7'}
                    className="input mt-1 w-full"
                  />
                  {newPolicy.scope === 'tags' && (
                    <datalist id="policy-tag-suggestions">
                      {knownTags.map((tg) => (
                        <option key={tg.tag} value={tg.tag} />
                      ))}
                    </datalist>
                  )}
                </div>
              )}
              <div>
                <label className="block text-xs text-text-secondary">
                  {t('patchPolicies.maxBatch')}
                </label>
                <input
                  type="number"
                  value={newPolicy.max_devices_per_batch}
                  onChange={(e) =>
                    setNewPolicy({
                      ...newPolicy,
                      max_devices_per_batch: parseInt(e.target.value) || 10,
                    })
                  }
                  className="input mt-1 w-full"
                />
              </div>
            </div>
            <div className="mt-4 flex items-center gap-6">
              <label className="flex items-center gap-2 text-sm text-text-primary">
                <input
                  type="checkbox"
                  checked={newPolicy.auto_reboot}
                  onChange={(e) =>
                    setNewPolicy({ ...newPolicy, auto_reboot: e.target.checked })
                  }
                  className="h-4 w-4" style={{ accentColor: 'var(--service)' }}
                />
                {t('patchPolicies.autoRebootIfNeeded')}
              </label>
              <label className="flex items-center gap-2 text-sm text-text-primary">
                <input
                  type="checkbox"
                  checked={newPolicy.approval_required}
                  onChange={(e) =>
                    setNewPolicy({
                      ...newPolicy,
                      approval_required: e.target.checked,
                    })
                  }
                  className="h-4 w-4" style={{ accentColor: 'var(--service)' }}
                />
                {t('patchPolicies.requireApproval')}
              </label>
            </div>
            <div className="mt-4 flex items-center gap-2">
              <button
                onClick={handleCreate}
                disabled={creating || !newPolicy.name}
                className="btn-primary"
              >
                {t('patchPolicies.createPolicy')}
              </button>
              <button
                onClick={() => setShowCreate(false)}
                className="btn"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        )}

        {policies.length === 0 ? (
          <div className="panel px-4 py-8 text-center">
            <Shield className="mx-auto h-5 w-5 text-text-muted" aria-hidden="true" />
            <p className="cap cap-sm mt-2 text-text-secondary">{t('patchPolicies.none')}</p>
            <p className="mt-1 text-sm text-text-muted">{t('patchPolicies.noneDesc')}</p>
          </div>
        ) : (
          <section className="panel mt-3 overflow-hidden">
            <GuideWordsHead
              total={policies.length}
              unit={t('sheet.inView')}
              first={policies[guide.first]?.name}
              last={policies[guide.last]?.name}
            />
            <div ref={guide.ref} className="max-h-[24rem] overflow-y-auto">
              <table className="ledger">
                <thead>
                  <tr>
                    <th>{t('common.name')}</th>
                    <th className="w-36">Scope</th>
                    <th className="w-24">Schedule</th>
                    <th className="w-40">Window</th>
                    <th className="w-16">Batch</th>
                    <th className="w-44">Conditions</th>
                    <th className="w-32">Last run</th>
                    <th className="w-16 text-right"> </th>
                  </tr>
                </thead>
                <tbody>
                  {policies.map((policy) => (
                    <tr key={policy.id} data-guide title={policy.name}>
                      <td>
                        <span className="cap cap-sm text-text-primary">{policy.name}</span>
                      </td>
                      <td>
                        <span className="cap cap-sm text-text-muted">
                          {policy.scope === 'all'
                            ? t('patchPolicies.allDevices')
                            : policy.scope === 'tags'
                              ? t('patchPolicies.scopeTag', { value: policy.scope_value })
                              : t('patchPolicies.scopeDevices', { value: policy.scope_value })}
                        </span>
                      </td>
                      <td>
                        <Stamp tone="neutral">{t(`patchPolicies.${policy.schedule}`)}</Stamp>
                      </td>
                      <td>
                        <span className="ow-num text-[12px]">{formatWindow(policy)}</span>
                        {policy.timezone && (
                          <span className="cap cap-sm block text-text-muted">{policy.timezone}</span>
                        )}
                      </td>
                      <td>
                        <span className="ow-num text-[12px]">{policy.max_devices_per_batch}</span>
                      </td>
                      <td>
                        <span className="flex flex-wrap gap-1.5">
                          {policy.approval_required && <Stamp tone="service">{t('patchPolicies.approvalRequired')}</Stamp>}
                          {policy.auto_reboot && <Stamp tone="due">{t('patchPolicies.autoReboot')}</Stamp>}
                          {!policy.approval_required && !policy.auto_reboot && (
                            <span className="cap cap-sm text-text-muted">none</span>
                          )}
                        </span>
                      </td>
                      <td>
                        <span className="ow-num text-[12px] text-text-secondary">
                          {policy.last_run_at
                            ? new Date(policy.last_run_at).toISOString().slice(0, 16).replace('T', ' ')
                            : 'never'}
                        </span>
                      </td>
                      <td className="text-right">
                        {isAdmin && (
                          <button
                            onClick={() => handleDelete(policy)}
                            className="btn-quiet btn text-[var(--stamp)]"
                            aria-label={`Delete policy ${policy.name}`}
                          >
                            {t('common.delete')}
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}
      </div>

      {/* Deployments section */}
      <div>
        <h2 className="text-lg font-semibold text-text-primary">{t('patchPolicies.deployments')}</h2>
        {deployments.length === 0 ? (
          <div className="panel px-4 py-8 text-center">
            <p className="cap cap-sm text-text-secondary">{t('patchPolicies.noDeployments')}</p>
          </div>
        ) : (
          <section className="panel mt-3 overflow-hidden">
            <GuideWordsHead
              total={Math.min(deployments.length, 10)}
              unit={t('sheet.inView')}
              first={deployments.slice(0, 10)[guide.first]?.id.slice(0, 8)}
              last={deployments.slice(0, 10)[guide.last]?.id.slice(0, 8)}
            />
            <div ref={guide.ref} className="max-h-[26rem] overflow-y-auto">
              <table className="ledger">
                <thead>
                  <tr>
                    <th className="w-32">Deployment</th>
                    <th className="w-32">Units</th>
                    <th className="w-28">State</th>
                    <th className="w-24">Progress</th>
                    <th className="w-24">Stated</th>
                    <th className="w-40 text-right"> </th>
                  </tr>
                </thead>
                <tbody>
                  {deployments.slice(0, 10).map((dep) => {
                    const progress =
                      dep.devices_total > 0
                        ? Math.round((dep.devices_success / dep.devices_total) * 100)
                        : 0;
                    return (
                      <Fragment key={dep.id}>
                        <tr data-guide title={dep.id.slice(0, 8)}>
                          <td>
                            <span className="ow-num text-[12px]">
                              {t('patchPolicies.deployment', { id: dep.id.slice(0, 8) })}
                            </span>
                          </td>
                          <td>
                            <span className="ow-num text-[12px]">
                              {t('patchPolicies.devicesCount', {
                                success: dep.devices_success,
                                total: dep.devices_total,
                              })}
                            </span>
                            {dep.devices_failed > 0 && (
                              <span className="cap cap-sm block text-[var(--error)]">
                                {t('patchPolicies.failedCount', { count: dep.devices_failed })}
                              </span>
                            )}
                          </td>
                          <td>
                            <Stamp
                              tone={
                                dep.status === 'completed'
                                  ? 'online'
                                  : dep.status === 'failed'
                                    ? 'alert'
                                    : dep.status === 'running'
                                      ? 'service'
                                      : 'neutral'
                              }
                            >
                              {t(`patchPolicies.deployStatuses.${dep.status}`)}
                            </Stamp>
                          </td>
                          <td>
                            <span className="ow-num text-[12px]">{progress}%</span>
                          </td>
                          <td>
                            <span className="ow-num text-[12px] text-text-secondary">{timeAgo(dep.created_at)}</span>
                          </td>
                          <td className="text-right">
                            <div className="flex justify-end gap-1.5">
                              <button onClick={() => toggleResults(dep.id)} className="btn-quiet btn">
                                <ChevronDown
                                  className={`h-3 w-3 transition-transform ${
                                    resultsFor === dep.id ? 'rotate-180' : ''
                                  }`}
                                  aria-hidden="true"
                                />
                                {t('patchPolicies.results')}
                              </button>
                              {(dep.status === 'completed' || dep.status === 'failed') && (
                                <button
                                  onClick={() => handleRollback(dep.id)}
                                  disabled={rollingBack === dep.id}
                                  className="btn-quiet btn"
                                >
                                  <RotateCcw
                                    className={`h-3 w-3 ${rollingBack === dep.id ? 'animate-spin' : ''}`}
                                    aria-hidden="true"
                                  />
                                  {t('patchPolicies.rollback')}
                                </button>
                              )}
                            </div>
                          </td>
                        </tr>
                        {resultsFor === dep.id && (
                          <tr>
                            <td colSpan={6} className="bg-[var(--field)]">
                              {resultsLoading ? (
                                <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
                              ) : results.length === 0 ? (
                                <p className="cap cap-sm text-text-muted">{t('patchPolicies.noResults')}</p>
                              ) : (
                                <ul className="space-y-1">
                                  {results.map((r) => (
                                    <li key={r.id} className="flex flex-wrap items-baseline gap-2 text-[12px]">
                                      <Stamp tone={r.kind === 'rollback' ? 'service' : 'neutral'}>
                                        {r.kind === 'rollback'
                                          ? t('patchPolicies.kindRollback')
                                          : t('patchPolicies.kindDeploy')}
                                      </Stamp>
                                      <span className="ow-num text-text-secondary">{r.device_id.slice(0, 8)}</span>
                                      <span
                                        className={
                                          r.result === 'success' ? 'text-[var(--online)]' : 'text-[var(--error)]'
                                        }
                                      >
                                        {r.result === 'success'
                                          ? t('patchPolicies.resultSuccess')
                                          : t('patchPolicies.resultFailed')}
                                      </span>
                                      {r.message && (
                                        <span className="cap cap-sm truncate text-text-muted" title={r.message}>
                                          {r.message}
                                        </span>
                                      )}
                                      <span className="ow-num ml-auto text-text-muted">{timeAgo(r.updated_at)}</span>
                                    </li>
                                  ))}
                                </ul>
                              )}
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>
        )}
      </div>
    </div>
  );
}
