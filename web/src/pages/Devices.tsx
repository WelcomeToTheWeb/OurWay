import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Search, Server, RefreshCw, LayoutGrid, List, X, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDevices } from '../hooks/useDevices';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';
import { DeviceCard } from '../components/DeviceCard';
import { InstallerPanel } from '../components/InstallerPanel';
import { StatusBadge } from '../components/StatusBadge';
import { Grid } from 'react-window';
import { applyDeviceFilters, collectTags, defaultFilters, type DeviceFilters, type SortKey } from '../utils/deviceList';
import { bulkTags } from '../api/tags';
import { deployNow, scanDevices } from '../api/patching';

type View = 'cards' | 'table';

function readView(): View {
  try {
    return localStorage.getItem('devices.view') === 'table' ? 'table' : 'cards';
  } catch {
    return 'cards';
  }
}

const selectCls =
  'rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none';

export function Devices() {
  const { t } = useTranslation();
  const { devices, loading, error, refresh } = useDevices();
  const { accessToken, hasAnyRole } = useAuth();
  const canAct = hasAnyRole(['admin', 'manager', 'technician']);
  const canDeploy = hasAnyRole(['admin', 'manager']);
  // Realtime presence: WS status/heartbeat frames update the shared device
  // store, so the grid reflects online/offline changes without a refresh.
  useWebSocket(accessToken);

  const [filters, setFilters] = useState<DeviceFilters>(defaultFilters);
  const [sort, setSort] = useState<SortKey>('name');
  const [desc, setDesc] = useState(false);
  const [view, setView] = useState<View>(readView);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [tagDraft, setTagDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    try {
      localStorage.setItem('devices.view', view);
    } catch {
      /* storage unavailable */
    }
  }, [view]);

  const filtered = useMemo(() => applyDeviceFilters(devices, filters, sort, desc), [devices, filters, sort, desc]);
  const allTags = useMemo(() => collectTags(devices), [devices]);
  const filtersActive = JSON.stringify(filters) !== JSON.stringify(defaultFilters);

  // Drop selections that are no longer visible so a bulk action never
  // touches devices the user can't see.
  const visibleIds = useMemo(() => new Set(filtered.map((d) => d.id)), [filtered]);
  const selectedVisible = [...selected].filter((id) => visibleIds.has(id));
  const allSelected = filtered.length > 0 && selectedVisible.length === filtered.length;

  function toggle(id: string) {
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  }

  async function bulk(fn: () => Promise<string>) {
    setBusy(true);
    setNotice(null);
    setActionError(null);
    try {
      setNotice(await fn());
    } catch (e) {
      const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error;
      setActionError(msg ?? 'Action failed');
    } finally {
      setBusy(false);
    }
  }

  const tagInput = tagDraft.trim().toLowerCase();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('devices.title')}</h1>
          <p className="text-sm text-text-secondary">
            {filtersActive
              ? `${filtered.length} of ${devices.length} devices`
              : t('devices.count', { count: devices.length })}
          </p>
        </div>
        <button
          onClick={refresh}
          className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg"
        >
          <RefreshCw className="h-4 w-4" />
          {t('common.refresh')}
        </button>
      </div>

      <InstallerPanel />

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-[14rem] flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary" />
          <input
            type="text"
            value={filters.search}
            onChange={(e) => setFilters({ ...filters, search: e.target.value })}
            placeholder={t('devices.searchDevices')}
            aria-label="Search devices"
            className="w-full rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <select aria-label="Filter by status" className={selectCls} value={filters.status}
          onChange={(e) => setFilters({ ...filters, status: e.target.value as DeviceFilters['status'] })}>
          <option value="all">All statuses</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
          <option value="alert">Alert</option>
        </select>
        <select aria-label="Filter by OS" className={selectCls} value={filters.os}
          onChange={(e) => setFilters({ ...filters, os: e.target.value as DeviceFilters['os'] })}>
          <option value="all">All systems</option>
          <option value="windows">Windows</option>
          <option value="linux">Linux</option>
          <option value="darwin">macOS</option>
        </select>
        {allTags.length > 0 && (
          <select aria-label="Filter by tag" className={selectCls} value={filters.tag}
            onChange={(e) => setFilters({ ...filters, tag: e.target.value })}>
            <option value="">All tags</option>
            {allTags.map((tg) => <option key={tg} value={tg}>{tg}</option>)}
          </select>
        )}
        <select aria-label="Sort devices" className={selectCls} value={sort}
          onChange={(e) => setSort(e.target.value as SortKey)}>
          <option value="name">Sort: name</option>
          <option value="status">Sort: status</option>
          <option value="last_seen">Sort: last seen</option>
          <option value="os">Sort: system</option>
        </select>
        <button
          onClick={() => setDesc((d) => !d)}
          aria-label={desc ? 'Descending order' : 'Ascending order'}
          aria-pressed={desc}
          className={`${selectCls} hover:bg-bg-secondary`}
        >
          {desc ? '↓' : '↑'}
        </button>
        {filtersActive && (
          <button onClick={() => setFilters(defaultFilters)} className="text-sm text-accent hover:underline">
            Clear filters
          </button>
        )}
        <div role="group" aria-label="View" className="ml-auto flex overflow-hidden rounded-lg border border-bg-border">
          {([['cards', LayoutGrid, 'Card view'], ['table', List, 'Table view']] as const).map(([v, Icon, label]) => (
            <button
              key={v}
              onClick={() => setView(v)}
              aria-label={label}
              aria-pressed={view === v}
              className={`px-3 py-2 ${view === v ? 'bg-accent text-white' : 'bg-bg text-text-secondary hover:bg-bg-secondary'}`}
            >
              <Icon className="h-4 w-4" />
            </button>
          ))}
        </div>
      </div>

      {actionError && <div role="alert" className="rounded-lg border border-status-error/40 bg-status-error/10 px-4 py-3 text-sm text-status-error">{actionError}</div>}
      {notice && <div role="status" className="rounded-lg border border-status-online/30 bg-status-online/10 px-4 py-3 text-sm text-status-online">{notice}</div>}

      {canAct && selectedVisible.length > 0 && (
        <div role="toolbar" aria-label="Bulk actions" className="flex flex-wrap items-center gap-3 rounded-xl border border-accent/40 bg-accent/10 px-4 py-3">
          <span className="text-sm font-medium text-text-primary">{selectedVisible.length} selected</span>
          <button disabled={busy} onClick={() => bulk(async () => `Scan requested on ${(await scanDevices(selectedVisible)).scans_sent} online device(s)`)}
            className="rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50">
            Scan for updates
          </button>
          {canDeploy && (
            <button disabled={busy} onClick={() => bulk(async () => { await deployNow(selectedVisible); return 'Deployment started'; })}
              className="rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50">
              Deploy approved
            </button>
          )}
          <input value={tagDraft} onChange={(e) => setTagDraft(e.target.value)} list="bulk-tag-suggestions" placeholder="tag" aria-label="Tag to add or remove" maxLength={32}
            className="w-28 rounded-lg border border-bg-border bg-bg px-2 py-1.5 text-xs text-text-primary" />
          <datalist id="bulk-tag-suggestions">{allTags.map((tg) => <option key={tg} value={tg} />)}</datalist>
          <button disabled={busy || !tagInput} onClick={() => bulk(async () => { const r = await bulkTags(selectedVisible, [tagInput], []); await refresh(); return `Tagged ${r.updated} device(s)`; })}
            className="rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50">
            Add tag
          </button>
          <button disabled={busy || !tagInput} onClick={() => bulk(async () => { const r = await bulkTags(selectedVisible, [], [tagInput]); await refresh(); return `Updated ${r.updated} device(s)`; })}
            className="rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg disabled:opacity-50">
            Remove tag
          </button>
          <button onClick={() => setSelected(new Set())} className="ml-auto flex items-center gap-1 text-xs text-text-secondary hover:text-text-primary">
            <X className="h-3.5 w-3.5" /> Clear
          </button>
        </div>
      )}

      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <Server className="h-8 w-8 animate-spin text-accent" />
        </div>
      ) : error ? (
        <p className="text-status-error">{error}</p>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
          <Server className="h-10 w-10 text-text-muted" />
          <p className="mt-3 text-sm">{filtersActive ? t('devices.noMatch') : t('devices.none')}</p>
        </div>
      ) : view === 'table' ? (
        <div className="overflow-x-auto rounded-xl border border-bg-border bg-bg-card">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-bg-border text-xs uppercase tracking-wide text-text-muted">
              <tr>
                {canAct && (
                  <th className="w-10 px-4 py-3">
                    <input type="checkbox" aria-label="Select all devices" checked={allSelected}
                      onChange={() => setSelected(allSelected ? new Set() : new Set(filtered.map((d) => d.id)))} />
                  </th>
                )}
                <th className="px-4 py-3">Device</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">System</th>
                <th className="px-4 py-3">Tags</th>
                <th className="px-4 py-3">Last seen</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-border">
              {filtered.map((d) => (
                <tr key={d.id} className="hover:bg-bg/50">
                  {canAct && (
                    <td className="px-4 py-3">
                      <input type="checkbox" aria-label={`Select ${d.name}`} checked={selected.has(d.id)} onChange={() => toggle(d.id)} />
                    </td>
                  )}
                  <td className="px-4 py-3">
                    <Link to={`/devices/${d.id}`} className="font-medium text-accent hover:underline">{d.name}</Link>
                    <p className="text-xs text-text-muted">{d.hostname}</p>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={d.status} />
                    {d.reboot_pending && <RotateCw className="ml-2 inline h-3.5 w-3.5 text-status-warning" aria-label="Reboot pending" />}
                  </td>
                  <td className="px-4 py-3 text-text-secondary">{d.os} · {d.arch}</td>
                  <td className="px-4 py-3 text-xs text-text-secondary">{(d.tags ?? []).join(', ') || '—'}</td>
                  <td className="px-4 py-3 text-xs text-text-muted">{new Date(d.last_seen).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : filtered.length > 20 ? (
        <div className="rounded-xl border border-bg-border bg-bg-card">
          <Grid
            className="overflow-y-auto"
            style={{ height: 600, width: '100%' }}
            rowHeight={140}
            columnWidth={280}
            rowCount={Math.ceil(filtered.length / 4)}
            columnCount={4}
            overscanCount={10}
            cellProps={{}}
            cellComponent={(props) => {
              const index = props.rowIndex * 4 + props.columnIndex;
              const device = filtered[index];
              if (!device) return null;
              return (
                <div style={{ ...props.style, padding: '8px' }}>
                  <DeviceCard device={device} />
                </div>
              );
            }}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {filtered.map((device) => (
            <DeviceCard key={device.id} device={device} />
          ))}
        </div>
      )}
    </div>
  );
}
