import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Check,
  Search,
  Bell,
  Activity,
  Cpu,
  MemoryStick,
  HardDrive,
  UserCheck,
  UserPlus,
} from 'lucide-react';
import { resolveAlert, acknowledgeAlert, assignAlert } from '../api/devices';
import { getUsers } from '../api/users';
import type { UserWithRoles } from '../auth/types';
import type { AlertSeverity } from '../types/alert';
import { useTranslation } from 'react-i18next';
import { Stamp, type StampTone } from '../components/Stamp';
import { agoLabel } from '../utils/policyScope';
import { filterAlerts, isAlertOpen } from '../utils/alerts';
import { useFleet } from '../hooks/useFleet';
import { useClientScope } from '../hooks/useClientScope';
import { ClientFacet } from '../components/ClientFacet';
import { invalidateFleet } from '../stores/fleet';

const severityTone: Record<AlertSeverity, StampTone> = {
  critical: 'alert',
  warning: 'due',
  info: 'service',
};

const severityLabel: Record<AlertSeverity, string> = {
  critical: 'Critical',
  warning: 'Warning',
  info: 'Info',
};

const metricIcons: Record<string, typeof Activity> = {
  cpu: Cpu,
  ram: MemoryStick,
  disk: HardDrive,
};

/**
 * The alert centre as a sign-off ledger: every row carries who acknowledged
 * it, who owns it, and the resolve stamp that closes it.
 */
export function Alerts() {
  const { t } = useTranslation();
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [search, setSearch] = useState('');
  const [newAlerts, setNewAlerts] = useState(0);
  const [users, setUsers] = useState<UserWithRoles[]>([]);
  const [assignFor, setAssignFor] = useState<string | null>(null);
  const [assignUser, setAssignUser] = useState('');

  // The ledger is the owner of the alert collection: it reads on arrival and
  // polls. The dashboard's alert tile rides along on the same data, and an
  // alert frame over the WebSocket invalidates it, so the numbers on the two
  // surfaces move together (2.2-4).
  const { data: alerts, loading } = useFleet('alerts', { owner: true });
  // The tenant facet (2.3-5): an alert names a device, so the tenant filter is
  // a lookup on that tenant's unit set, over the collection already on screen.
  const scope = useClientScope();
  const openNow = useMemo(() => alerts.filter(isAlertOpen).length, [alerts]);

  // "N new since the last look" compares two samples of the same count, and
  // only counts upward — an alert resolved elsewhere is not a new alert.
  const previousOpen = useRef<number | null>(null);
  useEffect(() => {
    if (loading || alerts.length === 0) return;
    const prev = previousOpen.current;
    if (prev !== null && openNow > prev) setNewAlerts(openNow - prev);
    previousOpen.current = openNow;
  }, [alerts, loading, openNow]);

  // Load the user list for the Assign picker.
  useEffect(() => {
    let cancelled = false;
    getUsers()
      .then((u) => {
        if (!cancelled) setUsers(u);
      })
      .catch(() => {
        // non-fatal: assign picker will be empty
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function mutate<T>(fn: () => Promise<T>, onDone: (result: T) => void) {
    try {
      const result = await fn();
      onDone(result);
      // The counts every surface prints come from this collection, so the
      // mutation has to re-read it, not just patch the row on screen.
      invalidateFleet('alerts');
    } catch {
      // The poll re-reads; a failed sign-off is not swallowed silently.
    }
  }

  const handleResolve = (id: string) =>
    mutate(
      () => resolveAlert(id),
      () => {
        /* the re-read carries the truth */
      }
    );

  const handleAcknowledge = (id: string) =>
    mutate(
      () => acknowledgeAlert(id),
      () => {
        /* the re-read carries the truth */
      }
    );

  const handleAssign = (id: string, username: string) =>
    mutate(
      () => assignAlert(id, username),
      () => {
        setAssignFor(null);
        setAssignUser('');
      }
    );

  // Filters are applied here, over the whole collection. The server can filter
  // too, and when it did the chips counted the filtered page while the tile
  // counted the fleet. One array, one set of predicates: a chip and the rows
  // it summarises can no longer disagree.
  const filtered = useMemo(
    () =>
      filterAlerts(alerts, {
        severity: severityFilter,
        status: statusFilter,
        search,
        deviceIds: scope.slug === '' ? undefined : scope.scopedDeviceIds,
      }),
    [alerts, severityFilter, statusFilter, search, scope.slug, scope.scopedDeviceIds]
  );
  const openCount = useMemo(() => filtered.filter(isAlertOpen).length, [filtered]);

  // The chips summarise the searched population, before the status chip's own
  // filter, so switching a chip changes the rows without changing what the
  // chips are measuring.
  const searchFiltered = useMemo(
    () => filterAlerts(alerts, { search, deviceIds: scope.slug === '' ? undefined : scope.scopedDeviceIds }),
    [alerts, search, scope.slug, scope.scopedDeviceIds]
  );
  const countChips = [
    { key: 'all', label: 'All', status: 'all', count: searchFiltered.length, title: 'Every alert the search matches' },
    {
      key: 'open',
      label: 'Open',
      status: 'unresolved',
      count: searchFiltered.filter(isAlertOpen).length,
      title: 'Unresolved — acknowledged ones are included, they are not closed',
    },
    {
      key: 'ack',
      // "Signed off", not "Acknowledged": the chip sits on the same screen as
      // the row badges that say Acknowledged, and a count chip must not
      // impersonate a sign-off.
      label: 'Signed off',
      status: 'all',
      count: searchFiltered.filter((a) => a.acknowledged && !a.resolved).length,
      title: 'Open and signed off by somebody — a display count, not a filter',
    },
    {
      key: 'resolved',
      label: 'Resolved',
      status: 'resolved',
      count: searchFiltered.filter((a) => a.resolved).length,
      title: 'Closed',
    },
  ];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('alerts.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {openCount} open · {filtered.length} on the sheet · {alerts.length} in the ledger
          </p>
        </div>
        <span className="flex items-center gap-2">
          <span className="cap-sm cap text-text-muted">Polling</span>
          <Stamp tone="online" title="The list reloads every 30 seconds and on every alert frame">
            Auto
          </Stamp>
        </span>
      </div>

      {newAlerts > 0 && (
        <div className="flex items-center justify-between gap-3 border border-rule-strong bg-[var(--field-alt)] px-3 py-2">
          <div className="flex items-center gap-2">
            <Bell className="h-4 w-4 text-[var(--due)]" aria-hidden="true" />
            <p className="text-[13px] text-text-primary">
              {newAlerts} new alert{newAlerts > 1 ? 's' : ''} since the last look
            </p>
          </div>
          <button type="button" onClick={() => setNewAlerts(0)} className="btn btn-quiet px-2 py-1 text-[11px]">
            {t('common.close')}
          </button>
        </div>
      )}

      {/* Count chips: counts of the population on screen, and the filter they
          stand for. They count the searched set, so a chip and the rows under
          it can never disagree (2.2-4). */}
      <div className="flex flex-wrap items-center gap-2">
        {countChips.map((chip) => (
          <button
            key={chip.key}
            type="button"
            onClick={() => setStatusFilter(chip.status)}
            aria-pressed={statusFilter === chip.status && chip.status !== 'all'}
            className={`btn ${statusFilter === chip.status && chip.status !== 'all' ? 'btn-primary' : 'btn-quiet'} px-2 py-1 text-[12px]`}
            title={chip.title}
          >
            <span className="ow-num">{chip.count}</span> {chip.label}
          </button>
        ))}
        <span className="cap cap-sm text-text-muted">
          of {searchFiltered.length} matching · {alerts.length} in the ledger
        </span>
      </div>

      <div className="panel flex flex-col gap-2 px-3 py-2.5 md:flex-row md:items-center">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" aria-hidden="true" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('alerts.search')}
            aria-label="Search alerts"
            className="input w-full pl-9"
          />
        </div>
        <div className="flex flex-col gap-2 sm:flex-row sm:gap-2">
          <ClientFacet
            clients={scope.clients}
            value={scope.slug}
            onChange={scope.setSlug}
            includeUnassigned={scope.includeUnassigned}
          />
          <select
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
            className="select w-full sm:w-auto"
            aria-label="Filter by severity"
          >
            <option value="all">{t('alerts.allSeverities')}</option>
            <option value="critical">{t('common.critical')}</option>
            <option value="warning">{t('common.warning')}</option>
            <option value="info">{t('common.info')}</option>
          </select>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="select w-full sm:w-auto"
            aria-label="Filter by status"
          >
            <option value="all">{t('alerts.allStatuses')}</option>
            <option value="unresolved">{t('alerts.unresolved')}</option>
            <option value="resolved">{t('alerts.resolved')}</option>
          </select>
        </div>
      </div>

      {loading && (
        <div className="flex h-48 items-center justify-center">
          <Activity className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        </div>
      )}

      {!loading && filtered.length === 0 && (
        <div className="panel flex flex-col items-center gap-2 px-6 py-14 text-center">
          <span className="flex h-14 w-14 items-center justify-center border border-rule">
            <Bell className="h-6 w-6 text-text-muted" aria-hidden="true" />
          </span>
          <p className="text-sm font-semibold text-text-primary">{t('alerts.allClear')}</p>
          <p className="text-[12px] text-text-secondary">{t('alerts.noAlertsMatch')}</p>
        </div>
      )}

      {!loading && filtered.length > 0 && (
        <div className="panel overflow-hidden">
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b border-rule-strong bg-bg-card px-3 py-1.5 md:grid-cols-[minmax(0,1fr)_14rem_auto]">
            <span className="cap cap-sm text-text-muted">Signal</span>
            <span className="cap cap-sm hidden text-text-muted md:block">Sign-off</span>
            <span className="cap cap-sm text-text-muted">Actions</span>
          </div>
          <ul>
            {filtered.map((alert) => {
              const MetricIcon = metricIcons[alert.metric] || Activity;
              const tone = severityTone[alert.severity];
              return (
                <li
                  key={alert.id}
                  className={`border-b border-rule px-3 py-2.5 last:border-b-0 ${
                    alert.resolved ? 'opacity-60' : ''
                  }`}
                >
                  <div className="grid grid-cols-1 items-start gap-3 md:grid-cols-[minmax(0,1fr)_14rem_auto]">
                    <div className="flex min-w-0 items-start gap-3">
                      <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center border border-rule" aria-hidden="true">
                        {alert.resolved ? (
                          <Check className="h-4 w-4 text-text-muted" />
                        ) : (
                          <MetricIcon className="h-4 w-4 text-text-secondary" />
                        )}
                      </span>
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Stamp tone={alert.resolved ? 'neutral' : tone}>{alert.resolved ? 'Resolved' : severityLabel[alert.severity]}</Stamp>
                          <span className="text-[13px] font-semibold text-text-primary">{alert.message}</span>
                        </div>
                        <p className="cap-sm cap mt-1 text-text-muted">
                          {alert.device_name} · {alert.metric} ·{' '}
                          <span className="ow-num">{alert.value}</span> vs{' '}
                          <span className="ow-num">{alert.threshold}</span> · {agoLabel(alert.created_at)}
                        </p>
                      </div>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5">
                      {alert.acknowledged && (
                        <Stamp tone="service" title="Acknowledged by somebody">
                          {t('alerts.acknowledged')}
                        </Stamp>
                      )}
                      {alert.assigned_to && (
                        <Stamp tone="neutral" title="Assigned owner">
                          {alert.assigned_to}
                        </Stamp>
                      )}
                      {alert.resolved && (
                        <Stamp tone="online" title="Closed">
                          {t('alerts.resolved')}
                        </Stamp>
                      )}
                      {!alert.acknowledged && !alert.assigned_to && !alert.resolved && (
                        <span className="cap-sm cap text-text-muted">Nobody on it</span>
                      )}
                    </div>

                    {!alert.resolved && (
                      <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                        {!alert.acknowledged && (
                          <button
                            type="button"
                            onClick={() => handleAcknowledge(alert.id)}
                            className="btn px-2 py-1 text-[11px]"
                            title={t('alerts.acknowledge')}
                          >
                            <UserCheck className="h-3.5 w-3.5" aria-hidden="true" />
                            {t('alerts.ack')}
                          </button>
                        )}
                        {!alert.assigned_to &&
                          (assignFor === alert.id ? (
                            <span className="flex items-center gap-1.5">
                              <select
                                value={assignUser}
                                onChange={(e) => setAssignUser(e.target.value)}
                                className="select px-2 py-1 text-[11px]"
                                aria-label="Assign to user"
                              >
                                <option value="">Select user…</option>
                                {users.map((u) => (
                                  <option key={u.id} value={u.username}>
                                    {u.username}
                                  </option>
                                ))}
                              </select>
                              <button
                                type="button"
                                onClick={() => handleAssign(alert.id, assignUser)}
                                disabled={!assignUser}
                                className="btn btn-primary px-2 py-1 text-[11px]"
                                title={t('alerts.assign')}
                              >
                                {t('alerts.assign')}
                              </button>
                            </span>
                          ) : (
                            <button
                              type="button"
                              onClick={() => setAssignFor(alert.id)}
                              className="btn px-2 py-1 text-[11px]"
                              title={t('alerts.assign')}
                            >
                              <UserPlus className="h-3.5 w-3.5" aria-hidden="true" />
                              {t('alerts.assign')}
                            </button>
                          ))}
                        <button
                          type="button"
                          onClick={() => handleResolve(alert.id)}
                          className="btn btn-primary px-2 py-1 text-[11px]"
                          title={t('alerts.resolve')}
                        >
                          <Check className="h-3.5 w-3.5" aria-hidden="true" />
                          {t('alerts.resolve')}
                        </button>
                      </div>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}
