import { useEffect, useState } from 'react';
import {
  Calendar,
  Check,
  Play,
  RefreshCw,
  Shield,
  Plus,
  Activity,
  Clock,
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
import { listTags, type TagCount } from '../api/tags';
import { useAuth } from '../auth/context';
import { browserTimeZone, formatWindow, timeZoneOptions } from '../utils/patchWindow';
import type { PatchPolicy, PatchDeployment, DeploymentResult } from '../types/patch';

function timeAgo(dateStr: string): string {
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return 'Unknown';
    const diff = Date.now() - d.getTime();
    if (diff < 60000) return 'Just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    return d.toLocaleDateString();
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
    const interval = setInterval(load, 30000);
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
    if (!window.confirm(`Delete policy "${policy.name}"? Queued deployments for it are dropped.`)) return;
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
        <Activity className="h-8 w-8 animate-spin text-accent" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      {/* Policies section */}
      <div>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold text-text-primary">{t('patchPolicies.title')}</h1>
            <p className="text-sm text-text-secondary">
              {t('patchPolicies.activePolicies', { count: policies.length })}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleDeploy}
              disabled={creating}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
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
          <div className="mt-4 rounded-xl border border-bg-border bg-bg-card p-6">
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
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
              </div>
              <div>
                <label className="block text-xs text-text-secondary">{t('patchPolicies.schedule')}</label>
                <select
                  value={newPolicy.schedule}
                  onChange={(e) =>
                    setNewPolicy({ ...newPolicy, schedule: e.target.value })
                  }
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
                />
              </div>
              <div>
                <label className="block text-xs text-text-secondary" htmlFor="policy-timezone">Time zone</label>
                <input
                  id="policy-timezone"
                  list="policy-timezones"
                  value={newPolicy.timezone}
                  onChange={(e) => setNewPolicy({ ...newPolicy, timezone: e.target.value })}
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                    className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                  className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
                  className="h-4 w-4 accent-accent"
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
                  className="h-4 w-4 accent-accent"
                />
                {t('patchPolicies.requireApproval')}
              </label>
            </div>
            <div className="mt-4 flex items-center gap-2">
              <button
                onClick={handleCreate}
                disabled={creating || !newPolicy.name}
                className="rounded-lg bg-accent px-4 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/80 disabled:opacity-50"
              >
                {t('patchPolicies.createPolicy')}
              </button>
              <button
                onClick={() => setShowCreate(false)}
                className="rounded-lg bg-bg-secondary px-4 py-2 text-xs font-medium text-text-primary transition-colors hover:bg-bg"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        )}

        {policies.length === 0 ? (
          <div className="mt-4 flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-12 text-text-secondary">
            <Shield className="h-10 w-10 text-text-muted" />
            <p className="mt-3 text-sm font-medium text-text-primary">
              {t('patchPolicies.none')}
            </p>
            <p className="mt-1 text-xs text-text-muted">
              {t('patchPolicies.noneDesc')}
            </p>
          </div>
        ) : (
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            {policies.map((policy) => (
              <div
                key={policy.id}
                className="rounded-xl border border-bg-border bg-bg-card p-4 transition-colors hover:border-accent/50"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent/15 text-accent">
                      <Shield className="h-5 w-5" />
                    </div>
                    <div>
                      <p className="text-sm font-medium text-text-primary">
                        {policy.name}
                      </p>
                      <p className="text-xs text-text-secondary">
                        {policy.scope === 'all'
                          ? t('patchPolicies.allDevices')
                          : policy.scope === 'tags'
                            ? t('patchPolicies.scopeTag', { value: policy.scope_value })
                            : t('patchPolicies.scopeDevices', { value: policy.scope_value })}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="flex items-center gap-1 text-xs text-text-secondary">
                      <Calendar className="h-3.5 w-3.5" />
                      {t(`patchPolicies.${policy.schedule}`)}
                    </span>
                  </div>
                </div>
                <div className="mt-3 flex items-center gap-4 text-xs text-text-secondary">
                  {policy.approval_required && (
                    <span className="flex items-center gap-1">
                      <Check className="h-3 w-3 text-status-online" />
                      {t('patchPolicies.approvalRequired')}
                    </span>
                  )}
                  {policy.auto_reboot && (
                    <span className="flex items-center gap-1">
                      <RefreshCw className="h-3 w-3 text-accent" />
                      {t('patchPolicies.autoReboot')}
                    </span>
                  )}
                  <span>{t('patchPolicies.batch', { count: policy.max_devices_per_batch })}</span>
                </div>
                <div className="mt-2 flex items-center justify-between text-xs text-text-muted">
                  <span>
                    Window: {formatWindow(policy)}
                    {policy.last_run_at && ` · last ran ${new Date(policy.last_run_at).toLocaleString()}`}
                  </span>
                  {isAdmin && (
                    <button
                      onClick={() => handleDelete(policy)}
                      className="rounded-md px-2 py-1 text-status-error hover:bg-status-error/10"
                      aria-label={`Delete policy ${policy.name}`}
                    >
                      Delete
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Deployments section */}
      <div>
        <h2 className="text-lg font-semibold text-text-primary">{t('patchPolicies.deployments')}</h2>
        {deployments.length === 0 ? (
          <div className="mt-4 rounded-xl border border-bg-border bg-bg-card p-8 text-center text-sm text-text-secondary">
            {t('patchPolicies.noDeployments')}
          </div>
        ) : (
          <div className="mt-4 space-y-3">
            {deployments.slice(0, 10).map((dep) => {
              const progress =
                dep.devices_total > 0
                  ? Math.round(
                      (dep.devices_success / dep.devices_total) * 100
                    )
                  : 0;
              return (
                <div
                  key={dep.id}
                  className="rounded-xl border border-bg-border bg-bg-card p-4"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <div
                        className={`flex h-10 w-10 items-center justify-center rounded-lg ${
                          dep.status === 'completed'
                            ? 'bg-status-online/15 text-status-online'
                            : dep.status === 'failed'
                              ? 'bg-status-error/15 text-status-error'
                              : dep.status === 'running'
                                ? 'bg-accent/15 text-accent'
                                : 'bg-bg text-text-secondary'
                        }`}
                      >
                        {dep.status === 'running' ? (
                          <Activity className="h-5 w-5 animate-spin" />
                        ) : (
                          <Clock className="h-5 w-5" />
                        )}
                      </div>
                      <div>
                        <p className="text-sm font-medium text-text-primary">
                          {t('patchPolicies.deployment', { id: dep.id.slice(0, 8) })}
                        </p>
                        <p className="text-xs text-text-secondary">
                          {t('patchPolicies.devicesCount', { success: dep.devices_success, total: dep.devices_total })}
                          {dep.devices_failed > 0 &&
                            ` • ${t('patchPolicies.failedCount', { count: dep.devices_failed })}`}
                        </p>
                      </div>
                    </div>
                    <div className="text-right">
                      <span
                        className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase ${
                          dep.status === 'completed'
                            ? 'bg-status-online/15 text-status-online'
                            : dep.status === 'failed'
                              ? 'bg-status-error/15 text-status-error'
                              : dep.status === 'running'
                                ? 'bg-accent/15 text-accent'
                                : 'bg-bg text-text-secondary'
                        }`}
                      >
                        {t(`patchPolicies.deployStatuses.${dep.status}`)}
                      </span>
                      <p className="mt-1 text-xs text-text-muted">
                        {timeAgo(dep.created_at)}
                      </p>
                    </div>
                  </div>
                  {dep.status === 'running' && (
                    <div className="mt-3">
                      <div className="h-1.5 w-full overflow-hidden rounded-full bg-bg">
                        <div
                          className="h-full rounded-full bg-accent transition-all"
                          style={{ width: `${progress}%` }}
                        />
                      </div>
                      <p className="mt-1 text-xs text-text-secondary">
                        {t('patchPolicies.percentComplete', { progress })}
                      </p>
                    </div>
                  )}
                  <div className="mt-3 flex items-center gap-2">
                    <button
                      onClick={() => toggleResults(dep.id)}
                      className="inline-flex items-center gap-1 rounded-lg border border-bg-border px-2 py-1 text-xs text-text-secondary transition-colors hover:text-text-primary"
                    >
                      <ChevronDown
                        className={`h-3 w-3 transition-transform ${
                          resultsFor === dep.id ? 'rotate-180' : ''
                        }`}
                      />
                      {t('patchPolicies.results')}
                    </button>
                    {(dep.status === 'completed' || dep.status === 'failed') && (
                      <button
                        onClick={() => handleRollback(dep.id)}
                        disabled={rollingBack === dep.id}
                        className="inline-flex items-center gap-1 rounded-lg border border-bg-border px-2 py-1 text-xs text-text-secondary transition-colors hover:text-text-primary disabled:opacity-50"
                      >
                        <RotateCcw
                          className={`h-3 w-3 ${rollingBack === dep.id ? 'animate-spin' : ''}`}
                        />
                        {t('patchPolicies.rollback')}
                      </button>
                    )}
                  </div>
                  {resultsFor === dep.id && (
                    <div className="mt-2 space-y-1 rounded-lg bg-bg p-2">
                      {resultsLoading ? (
                        <p className="flex items-center gap-1 text-xs text-text-muted">
                          <Activity className="h-3 w-3 animate-spin" />
                        </p>
                      ) : results.length === 0 ? (
                        <p className="text-xs text-text-muted">{t('patchPolicies.noResults')}</p>
                      ) : (
                        results.map((r) => (
                          <div key={r.id} className="flex items-center gap-2 text-xs">
                            <span
                              className={`inline-flex w-20 shrink-0 items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ${
                                r.kind === 'rollback'
                                  ? 'bg-accent/15 text-accent'
                                  : 'bg-bg-card text-text-secondary'
                              }`}
                            >
                              {t(
                                r.kind === 'rollback'
                                  ? 'patchPolicies.kindRollback'
                                  : 'patchPolicies.kindDeploy',
                              )}
                            </span>
                            <span className="font-mono text-text-secondary">
                              {r.device_id.slice(0, 8)}
                            </span>
                            <span
                              className={
                                r.result === 'success'
                                  ? 'text-status-online'
                                  : 'text-status-error'
                              }
                            >
                              {t(
                                r.result === 'success'
                                  ? 'patchPolicies.resultSuccess'
                                  : 'patchPolicies.resultFailed',
                              )}
                            </span>
                            {r.message && (
                              <span className="truncate text-text-muted" title={r.message}>
                                {r.message}
                              </span>
                            )}
                            <span className="ml-auto shrink-0 text-text-muted">
                              {timeAgo(r.updated_at)}
                            </span>
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
