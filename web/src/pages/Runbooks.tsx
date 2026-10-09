import { useEffect, useState } from 'react';
import {
  Calendar,
  Play,
  Plus,
  Clock,
  Terminal,
  ChevronDown,
  Trash2,
} from 'lucide-react';
import {
  listRunbooks,
  createRunbook,
  updateRunbook,
  deleteRunbook,
  runRunbookNow,
  listRunbookRuns,
  type Runbook,
  type RunbookRun,
} from '../api/automation';
import { browserTimeZone, timeZoneOptions } from '../utils/patchWindow';
import { useAuth } from '../auth/context';

function timeAgo(dateStr: string | null): string {
  if (!dateStr) return 'Never';
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

const emptyForm = {
  name: '',
  scope: 'all',
  scope_value: '',
  schedule: 'weekly',
  command: '',
  enabled: true,
  window_start: '02:00',
  window_hours: 4,
  timezone: browserTimeZone(),
};

export function Runbooks() {
  const { hasRole } = useAuth();
  const isAdmin = hasRole('admin');
  const canRun = hasRole('admin') || hasRole('manager');

  const [runbooks, setRunbooks] = useState<Runbook[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ ...emptyForm });
  const [error, setError] = useState<string | null>(null);

  const [runsFor, setRunsFor] = useState<string | null>(null);
  const [runs, setRuns] = useState<RunbookRun[]>([]);
  const [runsLoading, setRunsLoading] = useState(false);
  const [running, setRunning] = useState<string | null>(null);

  async function load() {
    try {
      setRunbooks(await listRunbooks());
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

  async function handleCreate() {
    if (!form.name.trim() || !form.command.trim()) {
      setError('Name and command are required.');
      return;
    }
    setCreating(true);
    setError(null);
    try {
      await createRunbook({
        name: form.name.trim(),
        scope: form.scope,
        scope_value: form.scope_value.trim(),
        schedule: form.schedule,
        command: form.command,
        enabled: form.enabled,
        window_start: form.window_start,
        window_hours: Number(form.window_hours) || 4,
        timezone: form.timezone,
      });
      setShowCreate(false);
      setForm({ ...emptyForm });
      await load();
    } catch {
      setError('Failed to create runbook.');
    } finally {
      setCreating(false);
    }
  }

  async function handleToggle(rb: Runbook) {
    try {
      await updateRunbook(rb.id, { ...rb, enabled: !rb.enabled });
      await load();
    } catch {
      // ignore
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteRunbook(id);
      await load();
    } catch {
      // ignore
    }
  }

  async function handleRun(id: string) {
    setRunning(id);
    try {
      await runRunbookNow(id);
      await load();
    } catch {
      // ignore
    } finally {
      setRunning(null);
    }
  }

  async function toggleRuns(id: string) {
    if (runsFor === id) {
      setRunsFor(null);
      return;
    }
    setRunsFor(id);
    setRuns([]);
    setRunsLoading(true);
    try {
      setRuns(await listRunbookRuns(id));
    } catch {
      // ignore
    } finally {
      setRunsLoading(false);
    }
  }

  const inputCls =
    'w-full rounded-md border border-bg-border bg-bg-primary px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none';
  const labelCls = 'block text-xs font-medium text-text-muted mb-1';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Automation</h1>
          <p className="text-sm text-text-muted">
            Scheduled runbooks: shell commands run on a scope of devices.
          </p>
        </div>
        {isAdmin && (
          <button
            onClick={() => setShowCreate(!showCreate)}
            className="flex items-center gap-2 rounded-md bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent/90"
          >
            <Plus className="h-4 w-4" />
            New runbook
          </button>
        )}
      </div>

      {showCreate && isAdmin && (
        <div className="rounded-lg border border-bg-border bg-bg-secondary p-4">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className={labelCls}>Name</label>
              <input
                className={inputCls}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Nightly disk cleanup"
              />
            </div>
            <div>
              <label className={labelCls}>Command</label>
              <input
                className={`${inputCls} font-mono`}
                value={form.command}
                onChange={(e) => setForm({ ...form, command: e.target.value })}
                placeholder="sh -c 'df -h'"
              />
            </div>
            <div>
              <label className={labelCls}>Scope</label>
              <select
                className={inputCls}
                value={form.scope}
                onChange={(e) => setForm({ ...form, scope: e.target.value })}
              >
                <option value="all">All devices</option>
                <option value="tags">Tags</option>
                <option value="devices">Devices</option>
              </select>
            </div>
            {form.scope !== 'all' && (
              <div>
                <label className={labelCls}>
                  {form.scope === 'tags' ? 'Tags (comma-separated)' : 'Devices (ids/names, comma-separated)'}
                </label>
                <input
                  className={inputCls}
                  value={form.scope_value}
                  onChange={(e) => setForm({ ...form, scope_value: e.target.value })}
                  placeholder={form.scope === 'tags' ? 'prod, web' : 'laptop, desktop'}
                />
              </div>
            )}
            <div>
              <label className={labelCls}>Schedule</label>
              <select
                className={inputCls}
                value={form.schedule}
                onChange={(e) => setForm({ ...form, schedule: e.target.value })}
              >
                <option value="daily">Daily</option>
                <option value="weekly">Weekly (Sundays)</option>
                <option value="monthly">Monthly (1st)</option>
              </select>
            </div>
            <div>
              <label className={labelCls}>Window start (HH:MM)</label>
              <input
                type="time"
                className={inputCls}
                value={form.window_start}
                onChange={(e) => setForm({ ...form, window_start: e.target.value })}
              />
            </div>
            <div>
              <label className={labelCls}>Window length (hours)</label>
              <input
                type="number"
                min={1}
                max={24}
                className={inputCls}
                value={form.window_hours}
                onChange={(e) => setForm({ ...form, window_hours: Number(e.target.value) })}
              />
            </div>
            <div>
              <label className={labelCls}>Timezone</label>
              <select
                className={inputCls}
                value={form.timezone}
                onChange={(e) => setForm({ ...form, timezone: e.target.value })}
              >
                {timeZoneOptions().map((tz) => (
                  <option key={tz} value={tz}>
                    {tz}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex items-end gap-2">
              <input
                id="rb-enabled"
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                className="h-4 w-4 accent-accent"
              />
              <label htmlFor="rb-enabled" className="text-sm text-text-secondary">
                Enabled
              </label>
            </div>
          </div>
          {error && <p className="mt-3 text-sm text-red-400">{error}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button
              onClick={() => setShowCreate(false)}
              className="rounded-md border border-bg-border px-4 py-2 text-sm text-text-secondary hover:bg-bg-primary"
            >
              Cancel
            </button>
            <button
              onClick={handleCreate}
              disabled={creating}
              className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-white hover:bg-accent/90 disabled:opacity-50"
            >
              {creating ? 'Creating…' : 'Create runbook'}
            </button>
          </div>
        </div>
      )}

      {loading ? (
        <div className="rounded-lg border border-bg-border bg-bg-secondary p-8 text-center text-text-muted">
          Loading runbooks…
        </div>
      ) : runbooks.length === 0 ? (
        <div className="rounded-lg border border-bg-border bg-bg-secondary p-8 text-center text-text-muted">
          No runbooks yet. Create one to automate recurring tasks.
        </div>
      ) : (
        <div className="space-y-3">
          {runbooks.map((rb) => (
            <div key={rb.id} className="rounded-lg border border-bg-border bg-bg-secondary p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Terminal className="h-4 w-4 shrink-0 text-accent" />
                    <span className="truncate font-medium text-text-primary">{rb.name}</span>
                    {!rb.enabled && (
                      <span className="rounded bg-bg-primary px-2 py-0.5 text-xs text-text-muted">Disabled</span>
                    )}
                  </div>
                  <code className="mt-1 block truncate font-mono text-xs text-text-muted">{rb.command}</code>
                  <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-text-muted">
                    <span className="flex items-center gap-1">
                      <Calendar className="h-3 w-3" /> {rb.schedule}
                    </span>
                    <span className="flex items-center gap-1">
                      <Clock className="h-3 w-3" /> {rb.window_start || '00:00'}+{rb.window_hours || 24}h {rb.timezone || 'UTC'}
                    </span>
                    <span>scope: {rb.scope}{rb.scope_value ? ` (${rb.scope_value})` : ''}</span>
                    <span>last run: {timeAgo(rb.last_run_at)}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {isAdmin && (
                    <button
                      onClick={() => handleToggle(rb)}
                      className="rounded-md border border-bg-border px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-primary"
                    >
                      {rb.enabled ? 'Disable' : 'Enable'}
                    </button>
                  )}
                  {canRun && (
                    <button
                      onClick={() => handleRun(rb.id)}
                      disabled={running === rb.id}
                      className="flex items-center gap-1 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/90 disabled:opacity-50"
                    >
                      <Play className="h-3 w-3" />
                      {running === rb.id ? 'Dispatching…' : 'Run now'}
                    </button>
                  )}
                  <button
                    onClick={() => toggleRuns(rb.id)}
                    className="flex items-center gap-1 rounded-md border border-bg-border px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-primary"
                  >
                    History
                    <ChevronDown className={`h-3 w-3 transition-transform ${runsFor === rb.id ? 'rotate-180' : ''}`} />
                  </button>
                  {isAdmin && (
                    <button
                      onClick={() => handleDelete(rb.id)}
                      className="rounded-md border border-red-900/50 p-1.5 text-red-400 hover:bg-red-950/30"
                      title="Delete runbook"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>

              {runsFor === rb.id && (
                <div className="mt-4 rounded-md border border-bg-border bg-bg-primary p-3">
                  {runsLoading ? (
                    <p className="text-xs text-text-muted">Loading runs…</p>
                  ) : runs.length === 0 ? (
                    <p className="text-xs text-text-muted">No runs recorded yet.</p>
                  ) : (
                    <div className="space-y-2">
                      {runs.map((run) => (
                        <div key={run.id} className="text-xs">
                          <div className="flex items-center gap-2">
                            <span
                              className={
                                run.status === 'success'
                                  ? 'text-green-400'
                                  : run.status === 'failed'
                                    ? 'text-red-400'
                                    : 'text-text-muted'
                              }
                            >
                              {run.status}
                            </span>
                            <span className="text-text-muted">device {run.device_id.slice(0, 8)}</span>
                            <span className="text-text-muted">{timeAgo(run.created_at)}</span>
                            {run.exit_code !== null && <span className="text-text-muted">exit {run.exit_code}</span>}
                          </div>
                          {run.output && (
                            <pre className="mt-1 max-h-32 overflow-auto rounded bg-bg-secondary p-2 font-mono text-[11px] text-text-secondary">
                              {run.output}
                            </pre>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
