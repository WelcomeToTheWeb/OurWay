import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Search, Server, RefreshCw, LayoutGrid, List, X, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDevices } from '../hooks/useDevices';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';
import { DeviceCard } from '../components/DeviceCard';
import { InstallerPanel } from '../components/InstallerPanel';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { Grid } from 'react-window';
import { applyDeviceFilters, collectTags, defaultFilters, devicesViewFromSearch, devicesViewToParams, isFiltered, type DeviceFilters, type SortKey } from '../utils/deviceList';
import { clientNameForSlug, clientSlugOfDevice } from '../utils/clientFilter';
import { ClientFacet } from '../components/ClientFacet';
import { bulkTags } from '../api/tags';
import { deployNow, scanDevices } from '../api/patching';
import { useFleet } from '../hooks/useFleet';
import type { DeviceCompliance } from '../types/patch';

/** Ledger time: 24-hour and unambiguous, with the ISO date beyond a day. */
function seenLabel(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return 'never';
  const mins = Math.round((Date.now() - d.getTime()) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  if (mins < 1440) return `${Math.round(mins / 60)}h ago`;
  return `${d.toISOString().slice(0, 10)} ${d.toISOString().slice(11, 16)} UTC`;
}

type View = 'cards' | 'table';

function readView(): View {
  try {
    return localStorage.getItem('devices.view') === 'table' ? 'table' : 'cards';
  } catch {
    return 'cards';
  }
}

/**
 * The fleet sheet: every unit as a decal with its record, plus the ledger
 * view for working a thousand units in one column.
 */
export function Devices() {
  const { t } = useTranslation();
  const { devices, loading, error, refresh } = useDevices();
  const { accessToken, hasAnyRole } = useAuth();
  const canAct = hasAnyRole(['admin', 'manager', 'technician']);
  const canDeploy = hasAnyRole(['admin', 'manager']);
  // Realtime presence: WS status/heartbeat frames update the shared device
  // store, so the sheet reflects online/offline changes without a refresh.
  useWebSocket(accessToken);

  const [params, setParams] = useSearchParams();
  // Every control below writes the URL and reads the URL. See
  // `devicesViewFromSearch`: a sheet a technician reached through a link must
  // be the sheet they see, after a reload and after a back.
  const view$ = devicesViewFromSearch(params);
  const filters = view$;
  const sort = view$.sort;
  const desc = view$.desc;
  // The view is URL state, not just a preference: a sheet opened from a
  // dashboard link, a bookmark, or a browser back must come back the same
  // shape. localStorage only supplies the initial default.
  const view: View = params.get('view') === 'table' ? 'table' : params.get('view') === 'cards' ? 'cards' : readView();
  function writeView(next: Partial<DeviceFilters & { sort: SortKey; desc: boolean }>, nextView?: View) {
    setParams(devicesViewToParams({ ...view$, ...next }, params, nextView ?? view), { replace: true });
  }
  const setFilters = (next: DeviceFilters) => writeView(next);
  const setSort = (next: SortKey) => writeView({ sort: next });
  const setDesc = (fn: (d: boolean) => boolean) => writeView({ desc: fn(desc) });
  // The view is URL state, not just a preference: a sheet opened from a
  // dashboard link, a bookmark, or a browser back must come back the same
  // shape. localStorage only supplies the initial default.
  const [selected, setSelected] = useState<Set<string>>(new Set());
  function setView(next: View) {
    try {
      localStorage.setItem('devices.view', next);
    } catch {
      // Private mode: the URL still carries it.
    }
    setParams(devicesViewToParams(view$, params, next), { replace: true });
  }

  const [tagDraft, setTagDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  // Shared collections (2.2-2): this page used to fetch the compliance view and
  // the policy list itself, on top of the list read useDevices already made.
  const { data: overview } = useFleet('patchOverview', { enabled: canAct });
  const { data: policies } = useFleet('patchPolicies');
  // The tenant list behind the client facet and the ledger's Client column.
  // A facet that needed its own read would be a read on every ledger that has
  // a facet; the shared fleet model is why it costs nothing.
  const { data: clients } = useFleet('clients');

  const complianceById = useMemo(
    () => new Map((overview?.devices ?? []).map((d) => [d.device_id, d]) as [string, DeviceCompliance][]),
    [overview],
  );

  useEffect(() => {
    try {
      localStorage.setItem('devices.view', view);
    } catch {
      /* storage unavailable */
    }
  }, [view]);

  const filtered = useMemo(
    () => applyDeviceFilters(devices, filters, sort, desc, clients),
    [devices, filters, sort, desc, clients],
  );
  const allTags = useMemo(() => collectTags(devices), [devices]);
  const filtersActive = isFiltered(filters);
  const guide = useGuideWords(filtered.length);

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
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('devices.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {filtersActive
              ? `${filtered.length} of ${devices.length} units in view`
              : t('devices.count', { count: devices.length })}
          </p>
        </div>
        <button onClick={refresh} className="btn">
          <RefreshCw className="h-3.5 w-3.5" />
          {t('common.refresh')}
        </button>
      </div>

      <InstallerPanel />

      {/* Filter band */}
      <div className="panel flex flex-wrap items-center gap-2 px-3 py-2.5">
        <div className="relative min-w-[14rem] flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" aria-hidden="true" />
          <input
            type="text"
            value={filters.search}
            onChange={(e) => setFilters({ ...filters, search: e.target.value })}
            placeholder={t('devices.searchDevices')}
            aria-label="Search devices"
            className="input w-full pl-9"
          />
        </div>
        <ClientFacet
          clients={clients}
          value={filters.client}
          onChange={(slug) => setFilters({ ...filters, client: slug })}
          includeUnassigned={hasAnyRole(['admin'])}
        />
        <select
          aria-label="Filter by status"
          className="select"
          value={filters.status}
          onChange={(e) => setFilters({ ...filters, status: e.target.value as DeviceFilters['status'] })}
        >
          <option value="all">All statuses</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
          <option value="alert">Alert</option>
        </select>
        <select
          aria-label="Filter by OS"
          className="select"
          value={filters.os}
          onChange={(e) => setFilters({ ...filters, os: e.target.value as DeviceFilters['os'] })}
        >
          <option value="all">All systems</option>
          <option value="windows">Windows</option>
          <option value="linux">Linux</option>
          <option value="darwin">macOS</option>
        </select>
        {allTags.length > 0 && (
          <select
            aria-label="Filter by tag"
            className="select"
            value={filters.tag}
            onChange={(e) => setFilters({ ...filters, tag: e.target.value })}
          >
            <option value="">All tags</option>
            {allTags.map((tg) => (
              <option key={tg} value={tg}>
                {tg}
              </option>
            ))}
          </select>
        )}
        <select
          aria-label="Sort devices"
          className="select"
          value={sort === 'last_seen' ? 'last-seen' : sort}
          onChange={(e) => setSort((e.target.value === 'last-seen' ? 'last_seen' : e.target.value) as SortKey)}
        >
          <option value="name">Sort: name</option>
          <option value="status">Sort: status</option>
          <option value="last-seen">Sort: last seen</option>
          <option value="os">Sort: system</option>
        </select>
        <button
          onClick={() => setDesc((d) => !d)}
          aria-label={desc ? 'Descending order' : 'Ascending order'}
          aria-pressed={desc}
          className="btn px-2.5 py-[9px]"
        >
          <span className="ow-num">{desc ? '↓' : '↑'}</span>
        </button>
        {filtersActive && (
          <button onClick={() => setFilters(defaultFilters)} className="cap-sm cap text-[var(--service)] hover:underline">
            Clear filters
          </button>
        )}
        <div role="group" aria-label="View" className="ml-auto flex">
          {([['cards', LayoutGrid, 'Card view'], ['table', List, 'Table view']] as const).map(([v, Icon, label]) => (
            <button
              key={v}
              onClick={() => setView(v)}
              aria-label={label}
              aria-pressed={view === v}
              className="btn border-rule-strong px-2.5 py-[9px]"
              style={{
                marginLeft: v === 'table' ? -1 : 0,
                background: view === v ? 'var(--ink)' : 'var(--bg-card)',
                color: view === v ? 'var(--bg-card)' : 'var(--text-secondary)',
              }}
            >
              <Icon className="h-4 w-4" />
            </button>
          ))}
        </div>
      </div>

      {actionError && (
        <div role="alert" className="panel border-[var(--error)] px-3 py-2.5 text-[13px] text-[var(--error)]">
          {actionError}
        </div>
      )}
      {notice && (
        <div role="status" className="panel border-[var(--online)] px-3 py-2.5 text-[13px] text-[var(--online)]">
          {notice}
        </div>
      )}

      {canAct && selectedVisible.length > 0 && (
        <div
          role="toolbar"
          aria-label="Bulk actions"
          className="flex flex-wrap items-center gap-2 border border-rule-strong bg-[var(--field-alt)] px-3 py-2"
        >
          <span className="cap cap-sm text-text-primary">{selectedVisible.length} selected</span>
          <button
            disabled={busy}
            onClick={() =>
              bulk(async () => `Scan requested on ${(await scanDevices(selectedVisible)).scans_sent} online device(s)`)
            }
            className="btn px-2.5 py-1.5 text-[11px]"
          >
            Scan for updates
          </button>
          {canDeploy && (
            <button
              disabled={busy}
              onClick={() =>
                bulk(async () => {
                  await deployNow(selectedVisible);
                  return 'Deployment started';
                })
              }
              className="btn px-2.5 py-1.5 text-[11px]"
            >
              Deploy approved
            </button>
          )}
          <input
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            list="bulk-tag-suggestions"
            placeholder="tag"
            aria-label="Tag to add or remove"
            maxLength={32}
            className="input w-28 px-2 py-1.5 text-[11px]"
          />
          <datalist id="bulk-tag-suggestions">
            {allTags.map((tg) => (
              <option key={tg} value={tg} />
            ))}
          </datalist>
          <button
            disabled={busy || !tagInput}
            onClick={() =>
              bulk(async () => {
                const r = await bulkTags(selectedVisible, [tagInput], []);
                await refresh();
                return `Tagged ${r.updated} device(s)`;
              })
            }
            className="btn px-2.5 py-1.5 text-[11px]"
          >
            Add tag
          </button>
          <button
            disabled={busy || !tagInput}
            onClick={() =>
              bulk(async () => {
                const r = await bulkTags(selectedVisible, [], [tagInput]);
                await refresh();
                return `Updated ${r.updated} device(s)`;
              })
            }
            className="btn px-2.5 py-1.5 text-[11px]"
          >
            Remove tag
          </button>
          <button
            onClick={() => setSelected(new Set())}
            className="cap-sm cap ml-auto flex items-center gap-1 text-text-secondary hover:text-text-primary"
          >
            <X className="h-3.5 w-3.5" /> Clear
          </button>
        </div>
      )}

      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-[13px] text-[var(--error)]">
          {error}
        </p>
      ) : filtered.length === 0 ? (
        <div className="panel flex flex-col items-center gap-3 px-6 py-16 text-center text-text-secondary">
          <Server className="h-8 w-8 text-text-muted" aria-hidden="true" />
          <p className="text-sm">{filtersActive ? t('devices.noMatch') : t('devices.none')}</p>
          {filtersActive && (
            <button onClick={() => setFilters(defaultFilters)} className="btn">
              Clear filters
            </button>
          )}
        </div>
      ) : view === 'table' ? (
        <div className="panel overflow-hidden">
          <GuideWordsHead
            total={filtered.length}
            unit={t('sheet.inView')}
            first={filtered[guide.first]?.name}
            last={filtered[guide.last]?.name}
          />
          <div ref={guide.ref} className="max-h-[36rem] overflow-y-auto">
            <table className="ledger">
              <thead>
                <tr>
                  {canAct && (
                    <th className="w-10">
                      <input
                        type="checkbox"
                        aria-label="Select all devices"
                        checked={allSelected}
                        onChange={() => setSelected(allSelected ? new Set() : new Set(filtered.map((d) => d.id)))}
                      />
                    </th>
                  )}
                  <th>Unit</th>
                  <th>State</th>
                  <th>{t('devices.colClient')}</th>
                  <th>System</th>
                  <th>Record</th>
                  <th>Tags</th>
                  <th className="text-right">Last seen</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((d) => {
                  const compliance = complianceById.get(d.id);
                  return (
                    <tr key={d.id} data-guide className="text-[13px]">
                      {canAct && (
                        <td>
                          <input
                            type="checkbox"
                            aria-label={`Select ${d.name}`}
                            checked={selected.has(d.id)}
                            onChange={() => toggle(d.id)}
                          />
                        </td>
                      )}
                      <td>
                        <Link
                          to={`/devices/${d.id}`}
                          className="font-semibold text-[var(--service)] hover:underline"
                        >
                          {d.name}
                        </Link>
                        <p className="cap-sm cap text-text-muted">{d.hostname}</p>
                      </td>
                      <td>
                        <Stamp tone={d.status === 'alert' ? 'alert' : d.status === 'offline' ? 'offline' : 'online'}>
                          {d.status}
                        </Stamp>
                        {d.reboot_pending && (
                          <RotateCw className="ml-2 inline h-3.5 w-3.5 text-[var(--due)]" aria-label="Reboot pending" />
                        )}
                      </td>
                      <td>
                        {/* The tenant, printed as a link into its own filtered
                            sheet: "show me the rest of this fleet" is the next
                            question after any row. */}
                        <Link
                          to={`/devices?client=${clientSlugOfDevice(d, clients)}`}
                          className="text-[11px] text-[var(--service)] hover:underline"
                        >
                          {clientNameForSlug(clients, clientSlugOfDevice(d, clients))}
                        </Link>
                      </td>
                      <td className="text-text-secondary">
                        {d.os} · <span className="ow-num text-[11px]">{d.arch}</span>
                      </td>
                      <td className="text-[11px] text-text-secondary">
                        {compliance ? (
                          compliance.critical > 0 ? (
                            <span className="text-[var(--error)]">{compliance.critical} critical</span>
                          ) : compliance.detected > 0 ? (
                            <span className="text-[var(--due)]">{compliance.detected} outstanding</span>
                          ) : (
                            <span className="text-[var(--online)]">clear</span>
                          )
                        ) : (
                          'not scanned'
                        )}
                      </td>
                      <td className="text-[11px] text-text-secondary">{(d.tags ?? []).join(', ') || '—'}</td>
                      <td className="ow-num text-right text-[11px] text-text-muted">
                        {seenLabel(d.last_seen)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      ) : filtered.length > 20 ? (
        <div className="panel overflow-hidden">
          <Grid
            className="overflow-y-auto outline-none"
            style={{ height: 640, width: '100%' }}
            rowHeight={152}
            columnWidth={300}
            rowCount={Math.ceil(filtered.length / 4)}
            columnCount={4}
            overscanCount={10}
            cellProps={{}}
            cellComponent={(props) => {
              const index = props.rowIndex * 4 + props.columnIndex;
              const device = filtered[index];
              if (!device) return null;
              return (
                <div style={{ ...props.style, padding: '6px' }}>
                  <DeviceCard
                    device={device}
                    compliance={complianceById.get(device.id)}
                    policies={policies}
                    clientName={clientNameForSlug(clients, clientSlugOfDevice(device, clients))}
                  />
                </div>
              );
            }}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {filtered.map((device) => (
            <DeviceCard
              key={device.id}
              device={device}
              compliance={complianceById.get(device.id)}
              policies={policies}
              clientName={clientNameForSlug(clients, clientSlugOfDevice(device, clients))}
            />
          ))}
        </div>
      )}
    </div>
  );
}
