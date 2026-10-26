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
import { useWebSocket } from '../hooks/useWebSocket';
import type { FleetUpdate, PatchOverview, Severity } from '../types/patch';
import { Stamp, type StampTone } from '../components/Stamp';
import { FieldBox } from '../components/FieldBox';

const severityTone: Record<Severity, StampTone> = {
  critical: 'alert',
  important: 'due',
  moderate: 'service',
  low: 'neutral',
  unspecified: 'neutral',
};

export function FleetPatches() {
  const { hasAnyRole, accessToken } = useAuth();
  const canApprove = hasAnyRole(['admin', 'manager']);
  const canScan = hasAnyRole(['admin', 'manager', 'technician']);
  useWebSocket(accessToken);

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

  const idsFor = (keys: Set<string>) => updates.filter((u) => keys.has(u.key)).flatMap((u) => u.detected_ids);

  const toggle = (key: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(key)) n.delete(key);
      else n.add(key);
      return n;
    });

  if (loading) {
    return (
      <div className="flex h-32 items-center justify-center">
        <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
      </div>
    );
  }

  const totals = overview?.totals;
  const pct = totals && totals.devices > 0 ? Math.round((totals.compliant / totals.devices) * 100) : 100;
  const approvedWaiting = overview?.devices.reduce((n, d) => n + d.approved, 0) ?? 0;
  const nonCompliant = (overview?.devices ?? []).filter((d) => !d.compliant || d.reboot_pending);

  return (
    <div className="space-y-4">
      {error && (
        <div role="alert" className="panel border-[var(--error)] flex items-center justify-between gap-3 px-3 py-2.5 text-[13px] text-[var(--error)]">
          {error}
          <button onClick={() => setError(null)} className="btn btn-quiet px-2 py-1 text-[11px]">
            Dismiss
          </button>
        </div>
      )}
      {notice && (
        <div role="status" className="panel border-[var(--online)] px-3 py-2.5 text-[13px] text-[var(--online)]">
          {notice}
        </div>
      )}

      <section aria-label="Compliance" className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <FieldBox
          label={`Compliant (${totals?.compliant ?? 0} of ${totals?.devices ?? 0})`}
          value={<span className="ow-num">{pct}%</span>}
          tone={pct === 100 ? 'ok' : pct >= 80 ? 'due' : 'alert'}
          hint={`${totals?.devices ?? 0} units in scope`}
        />
        <FieldBox
          label="Outstanding updates"
          value={<span className="ow-num">{totals?.pending ?? 0}</span>}
          tone={(totals?.pending ?? 0) > 0 ? 'due' : 'ok'}
          hint={`${approvedWaiting} approved, waiting for a window`}
        />
        <FieldBox
          label="Critical"
          value={<span className="ow-num">{totals?.critical ?? 0}</span>}
          tone={(totals?.critical ?? 0) > 0 ? 'alert' : 'ok'}
          hint="install now, not at the next window"
        />
        <FieldBox
          label="Reboot pending"
          value={<span className="ow-num">{totals?.reboot_pending ?? 0}</span>}
          tone={(totals?.reboot_pending ?? 0) > 0 ? 'due' : 'ok'}
          hint="installed, unit not yet restarted"
        />
      </section>

      <div className="panel flex flex-wrap items-center gap-2 px-3 py-2.5">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" aria-hidden="true" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search title or KB"
            aria-label="Search updates"
            className="input w-64 pl-9"
          />
        </div>
        <select
          value={severity}
          onChange={(e) => setSeverity(e.target.value as 'all' | Severity)}
          aria-label="Filter by severity"
          className="select"
        >
          <option value="all">All severities</option>
          {(['critical', 'important', 'moderate', 'low', 'unspecified'] as Severity[]).map((s) => (
            <option key={s} value={s} className="capitalize">
              {s}
            </option>
          ))}
        </select>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {canScan && (
            <button
              disabled={busy}
              onClick={() => act(async () => `Scan requested on ${(await scanDevices()).scans_sent} online device(s)`)}
              className="btn px-2.5 py-1.5 text-[11px]"
            >
              <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" /> Scan all
            </button>
          )}
          {canApprove && (
            <>
              <button
                disabled={busy || selected.size === 0}
                onClick={() => act(async () => `Approved ${(await bulkApproveUpdates(idsFor(selected))).changed} update(s)`)}
                className="btn px-2.5 py-1.5 text-[11px]"
              >
                <Check className="h-3.5 w-3.5" aria-hidden="true" /> Approve selected ({selected.size})
              </button>
              <button
                disabled={busy || approvedWaiting === 0}
                onClick={() =>
                  act(async () => {
                    await deployNow();
                    return 'Deployment started';
                  })
                }
                className="btn btn-primary px-2.5 py-1.5 text-[11px]"
              >
                <Play className="h-3.5 w-3.5" aria-hidden="true" /> Deploy approved ({approvedWaiting})
              </button>
            </>
          )}
        </div>
      </div>

      <section className="panel overflow-hidden" aria-label="Outstanding updates">
        <div className="panel-head">
          <h2 className="cap cap-lg text-text-muted">Outstanding updates</h2>
          <span className="cap cap-sm ml-auto text-text-muted">{visible.length} of {updates.length}</span>
        </div>
        {visible.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-4 py-12 text-center">
            <ShieldCheck className="h-7 w-7 text-[var(--online)]" aria-hidden="true" />
            <p className="text-[13px] text-text-secondary">
              {updates.length === 0 ? 'Nothing outstanding — every unit is up to date.' : 'No updates match the filters.'}
            </p>
          </div>
        ) : (
          <table className="ledger">
            <thead>
              <tr>
                {canApprove && (
                  <th className="w-10">
                    <span className="sr-only">Select</span>
                  </th>
                )}
                <th>Update</th>
                <th>Severity</th>
                <th>Units</th>
                <th>State</th>
                {canApprove && <th className="text-right">Actions</th>}
              </tr>
            </thead>
            <tbody>
              {visible.map((u) => (
                <tr key={u.key} className="text-[13px]">
                  {canApprove && (
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`Select ${u.title}`}
                        disabled={u.detected_ids.length === 0}
                        checked={selected.has(u.key)}
                        onChange={() => toggle(u.key)}
                      />
                    </td>
                  )}
                  <td>
                    <p className="font-semibold text-text-primary">{u.title}</p>
                    <p className="cap-sm cap text-text-muted">
                      {[u.kb && `KB${u.kb}`, u.category, u.source].filter(Boolean).join(' · ')}
                    </p>
                  </td>
                  <td>
                    <Stamp tone={severityTone[u.severity]}>{u.severity}</Stamp>
                  </td>
                  <td className="ow-num text-text-secondary">{u.devices}</td>
                  <td className="text-[11px] text-text-secondary">
                    {Object.entries(u.statuses)
                      .map(([k, v]) => `${v} ${k}`)
                      .join(', ')}
                  </td>
                  {canApprove && (
                    <td className="text-right">
                      <button
                        disabled={busy || u.detected_ids.length === 0}
                        onClick={() =>
                          act(async () => `Approved on ${(await bulkApproveUpdates(u.detected_ids)).changed} device(s)`)
                        }
                        className="btn btn-quiet px-2 py-1 text-[11px]"
                      >
                        Approve on all
                      </button>
                      <button
                        disabled={busy || u.detected_ids.length === 0}
                        onClick={() => act(async () => `Skipped on ${(await bulkSkipUpdates(u.detected_ids)).changed} device(s)`)}
                        className="btn btn-quiet ml-1 px-2 py-1 text-[11px]"
                        aria-label={`Skip ${u.title}`}
                      >
                        <EyeOff className="h-3.5 w-3.5" aria-hidden="true" /> Skip
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="panel overflow-hidden" aria-label="Device compliance">
        <div className="panel-head">
          <h2 className="cap cap-lg text-text-muted">Units not clear</h2>
          <Link to="/patch-policies" className="cap cap-sm ml-auto text-[var(--service)] hover:underline">
            Policies & windows
          </Link>
        </div>
        {nonCompliant.length === 0 ? (
          <div className="flex items-center gap-2 px-3 py-6">
            <ShieldCheck className="h-5 w-5 text-[var(--online)]" aria-hidden="true" />
            <p className="text-[13px] text-text-secondary">Every unit is compliant.</p>
          </div>
        ) : (
          <table className="ledger">
            <thead>
              <tr>
                <th>Unit</th>
                <th>Detected</th>
                <th>Approved</th>
                <th>Failed</th>
                <th>Critical</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {nonCompliant.slice(0, 40).map((d) => (
                <tr key={d.device_id} className="text-[13px]">
                  <td>
                    <Link to={`/devices/${d.device_id}`} className="font-semibold text-[var(--service)] hover:underline">
                      {d.name}
                    </Link>
                    <span className="cap-sm cap ml-2 text-text-muted">{d.os}</span>
                  </td>
                  <td className="ow-num text-text-secondary">{d.detected}</td>
                  <td className="ow-num text-text-secondary">{d.approved}</td>
                  <td className="ow-num text-text-secondary">{d.failed}</td>
                  <td>
                    {d.critical > 0 ? (
                      <span className="flex items-center gap-1 text-[var(--error)]">
                        <ShieldAlert className="h-3.5 w-3.5" aria-hidden="true" />
                        <span className="ow-num">{d.critical}</span>
                      </span>
                    ) : (
                      <span className="ow-num text-text-muted">0</span>
                    )}
                  </td>
                  <td className="text-[11px]">
                    {d.reboot_pending && (
                      <span className="flex items-center gap-1 text-[var(--due)]">
                        <RotateCw className="h-3.5 w-3.5" aria-hidden="true" /> Reboot pending
                      </span>
                    )}
                    {d.failed > 0 && (
                      <span className="flex items-center gap-1 text-[var(--error)]">
                        <AlertTriangle className="h-3.5 w-3.5" aria-hidden="true" /> Failed installs
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
