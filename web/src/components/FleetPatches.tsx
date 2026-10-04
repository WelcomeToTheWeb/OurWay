import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Check, EyeOff, Play, RefreshCw, RotateCw, Search, ShieldCheck, ShieldAlert, AlertTriangle } from 'lucide-react';
import {
  bulkApproveUpdates,
  bulkSkipUpdates,
  deployNow,
  getPatchOverview,
  listFleetUpdates,
  scanDevices,
} from '../api/patching';
import { useAuth } from '../auth/context';
import type { FleetUpdate, PatchOverview, Severity } from '../types/patch';

const severityStyle: Record<Severity, string> = {
  critical: 'bg-status-error/15 text-status-error ring-status-error/30',
  important: 'bg-status-warning/15 text-status-warning ring-status-warning/30',
  moderate: 'bg-accent/15 text-accent ring-accent/30',
  low: 'bg-bg text-text-secondary ring-bg-border',
  unspecified: 'bg-bg text-text-muted ring-bg-border',
};

function SeverityBadge({ severity }: { severity: Severity }) {
  return (
    <span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium capitalize ring-1 ring-inset ${severityStyle[severity]}`}>
      {severity}
    </span>
  );
}

function Stat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div className="rounded-xl border border-bg-border bg-bg-card p-4">
      <p className={`text-2xl font-semibold ${tone ?? 'text-text-primary'}`}>{value}</p>
      <p className="text-xs text-text-secondary">{label}</p>
    </div>
  );
}

export function FleetPatches() {
  const { hasAnyRole } = useAuth();
  const canApprove = hasAnyRole(['admin', 'manager']);
  const canScan = hasAnyRole(['admin', 'manager', 'technician']);

  const [overview, setOverview] = useState<PatchOverview | null>(null);
  const [updates, setUpdates] = useState<FleetUpdate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [search, setSearch] = useState('');
  const [severity, setSeverity] = useState<'all' | Severity>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    try {
      const [o, u] = await Promise.all([getPatchOverview(), listFleetUpdates()]);
      setOverview(o);
      setUpdates(u);
      setError(null);
    } catch {
      setError('Failed to load patch data');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return updates.filter(
      (u) =>
        (severity === 'all' || u.severity === severity) &&
        (!q || u.title.toLowerCase().includes(q) || u.kb.toLowerCase().includes(q)),
    );
  }, [updates, search, severity]);

  async function act(fn: () => Promise<string>) {
    setBusy(true);
    setNotice(null);
    try {
      setNotice(await fn());
      setSelected(new Set());
      await load();
    } catch (e) {
      const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error;
      setError(msg ?? 'Action failed');
    } finally {
      setBusy(false);
    }
  }

  const idsFor = (keys: Set<string>) =>
    updates.filter((u) => keys.has(u.key)).flatMap((u) => u.detected_ids);

  const toggle = (key: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(key)) n.delete(key);
      else n.add(key);
      return n;
    });

  if (loading) return <p className="text-sm text-text-secondary">Loading…</p>;

  const totals = overview?.totals;
  const pct = totals && totals.devices > 0 ? Math.round((totals.compliant / totals.devices) * 100) : 100;
  const approvedWaiting = overview?.devices.reduce((n, d) => n + d.approved, 0) ?? 0;

  return (
    <div className="space-y-6">
      {error && (
        <div role="alert" className="flex items-center justify-between rounded-lg border border-status-error/40 bg-status-error/10 px-4 py-3 text-sm text-status-error">
          {error}
          <button onClick={() => setError(null)} className="text-text-secondary hover:text-text-primary">Dismiss</button>
        </div>
      )}
      {notice && <div role="status" className="rounded-lg border border-status-online/30 bg-status-online/10 px-4 py-3 text-sm text-status-online">{notice}</div>}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label={`Compliant (${totals?.compliant ?? 0} of ${totals?.devices ?? 0} devices)`} value={`${pct}%`} tone={pct === 100 ? 'text-status-online' : pct >= 80 ? 'text-status-warning' : 'text-status-error'} />
        <Stat label="Outstanding updates" value={totals?.pending ?? 0} />
        <Stat label="Critical" value={totals?.critical ?? 0} tone={(totals?.critical ?? 0) > 0 ? 'text-status-error' : undefined} />
        <Stat label="Reboot pending" value={totals?.reboot_pending ?? 0} tone={(totals?.reboot_pending ?? 0) > 0 ? 'text-status-warning' : undefined} />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search title or KB"
            aria-label="Search updates"
            className="rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted"
          />
        </div>
        <select
          value={severity}
          onChange={(e) => setSeverity(e.target.value as 'all' | Severity)}
          aria-label="Filter by severity"
          className="rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
        >
          <option value="all">All severities</option>
          {(['critical', 'important', 'moderate', 'low', 'unspecified'] as Severity[]).map((s) => (
            <option key={s} value={s} className="capitalize">{s}</option>
          ))}
        </select>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {canScan && (
            <button
              disabled={busy}
              onClick={() => act(async () => `Scan requested on ${(await scanDevices()).scans_sent} online device(s)`)}
              className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50"
            >
              <RefreshCw className="h-3.5 w-3.5" /> Scan all
            </button>
          )}
          {canApprove && (
            <>
              <button
                disabled={busy || selected.size === 0}
                onClick={() => act(async () => `Approved ${(await bulkApproveUpdates(idsFor(selected))).changed} update(s)`)}
                className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50"
              >
                <Check className="h-3.5 w-3.5" /> Approve selected ({selected.size})
              </button>
              <button
                disabled={busy || approvedWaiting === 0}
                onClick={() => act(async () => { await deployNow(); return 'Deployment started'; })}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
              >
                <Play className="h-3.5 w-3.5" /> Deploy approved ({approvedWaiting})
              </button>
            </>
          )}
        </div>
      </div>

      <section aria-label="Outstanding updates" className="overflow-hidden rounded-xl border border-bg-border bg-bg-card">
        {visible.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-12 text-text-secondary">
            <ShieldCheck className="h-8 w-8 text-status-online" />
            <p className="text-sm">{updates.length === 0 ? 'Nothing outstanding — the fleet is up to date.' : 'No updates match the filters.'}</p>
          </div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b border-bg-border text-xs uppercase tracking-wide text-text-muted">
              <tr>
                {canApprove && <th className="w-10 px-4 py-3"><span className="sr-only">Select</span></th>}
                <th className="px-4 py-3">Update</th>
                <th className="px-4 py-3">Severity</th>
                <th className="px-4 py-3">Devices</th>
                <th className="px-4 py-3">State</th>
                {canApprove && <th className="px-4 py-3 text-right">Actions</th>}
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-border">
              {visible.map((u) => (
                <tr key={u.key} className="hover:bg-bg/50">
                  {canApprove && (
                    <td className="px-4 py-3">
                      <input
                        type="checkbox"
                        aria-label={`Select ${u.title}`}
                        disabled={u.detected_ids.length === 0}
                        checked={selected.has(u.key)}
                        onChange={() => toggle(u.key)}
                      />
                    </td>
                  )}
                  <td className="px-4 py-3">
                    <p className="font-medium text-text-primary">{u.title}</p>
                    <p className="text-xs text-text-muted">{[u.kb && `KB${u.kb}`, u.category, u.source].filter(Boolean).join(' · ')}</p>
                  </td>
                  <td className="px-4 py-3"><SeverityBadge severity={u.severity} /></td>
                  <td className="px-4 py-3 text-text-secondary">{u.devices}</td>
                  <td className="px-4 py-3 text-xs text-text-secondary">
                    {Object.entries(u.statuses).map(([k, v]) => `${v} ${k}`).join(', ')}
                  </td>
                  {canApprove && (
                    <td className="px-4 py-3 text-right">
                      <button
                        disabled={busy || u.detected_ids.length === 0}
                        onClick={() => act(async () => `Approved on ${(await bulkApproveUpdates(u.detected_ids)).changed} device(s)`)}
                        className="rounded-md px-2 py-1 text-xs text-accent hover:bg-accent/10 disabled:opacity-40"
                      >
                        Approve on all
                      </button>
                      <button
                        disabled={busy || u.detected_ids.length === 0}
                        onClick={() => act(async () => `Skipped on ${(await bulkSkipUpdates(u.detected_ids)).changed} device(s)`)}
                        className="ml-1 rounded-md px-2 py-1 text-xs text-text-secondary hover:bg-bg disabled:opacity-40"
                        aria-label={`Skip ${u.title}`}
                      >
                        <EyeOff className="inline h-3.5 w-3.5" /> Skip
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section aria-label="Device compliance">
        <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-text-muted">Devices needing attention</h2>
        <div className="overflow-hidden rounded-xl border border-bg-border bg-bg-card">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-bg-border text-xs uppercase tracking-wide text-text-muted">
              <tr>
                <th className="px-4 py-3">Device</th>
                <th className="px-4 py-3">Detected</th>
                <th className="px-4 py-3">Approved</th>
                <th className="px-4 py-3">Failed</th>
                <th className="px-4 py-3">Critical</th>
                <th className="px-4 py-3">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-border">
              {(overview?.devices ?? []).filter((d) => !d.compliant || d.reboot_pending).slice(0, 25).map((d) => (
                <tr key={d.device_id} className="hover:bg-bg/50">
                  <td className="px-4 py-3">
                    <Link to={`/devices/${d.device_id}`} className="font-medium text-accent hover:underline">{d.name}</Link>
                    <span className="ml-2 text-xs text-text-muted">{d.os}</span>
                  </td>
                  <td className="px-4 py-3 text-text-secondary">{d.detected}</td>
                  <td className="px-4 py-3 text-text-secondary">{d.approved}</td>
                  <td className="px-4 py-3 text-text-secondary">{d.failed}</td>
                  <td className="px-4 py-3">{d.critical > 0 ? <span className="inline-flex items-center gap-1 text-status-error"><ShieldAlert className="h-3.5 w-3.5" />{d.critical}</span> : <span className="text-text-muted">0</span>}</td>
                  <td className="px-4 py-3 text-xs">
                    {d.reboot_pending && <span className="inline-flex items-center gap-1 text-status-warning"><RotateCw className="h-3.5 w-3.5" /> Reboot pending</span>}
                    {d.failed > 0 && <span className="inline-flex items-center gap-1 text-status-error"><AlertTriangle className="h-3.5 w-3.5" /> Failed installs</span>}
                  </td>
                </tr>
              ))}
              {(overview?.devices ?? []).every((d) => d.compliant && !d.reboot_pending) && (
                <tr><td colSpan={6} className="px-4 py-8 text-center text-sm text-text-secondary">Every device is compliant.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
