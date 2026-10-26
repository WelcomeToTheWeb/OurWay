import { useEffect, useState, useRef } from 'react';
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
import { getAlerts, resolveAlert, acknowledgeAlert, assignAlert } from '../api/devices';
import { getUsers } from '../api/users';
import type { UserWithRoles } from '../auth/types';
import type { Alert, AlertSeverity } from '../types/alert';
import { useTranslation } from 'react-i18next';
import { Stamp, type StampTone } from '../components/Stamp';
import { agoLabel } from '../utils/policyScope';
import { isRateLimited } from '../api/client';

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
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(true);
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [search, setSearch] = useState('');
  const [newAlerts, setNewAlerts] = useState(0);
  const [users, setUsers] = useState<UserWithRoles[]>([]);
  const [assignFor, setAssignFor] = useState<string | null>(null);
  const [assignUser, setAssignUser] = useState('');
  const previousCount = useRef(0);

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

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        setLoading(true);
        const params: Record<string, string> = {};
        if (severityFilter !== 'all') params.severity = severityFilter;
        if (statusFilter === 'unresolved') params.resolved = 'false';
        if (statusFilter === 'resolved') params.resolved = 'true';

        const data = await getAlerts(params as never);
        if (!cancelled) {
          const count = data.filter((a) => !a.resolved).length;
          if (previousCount.current > 0 && count > previousCount.current) {
            setNewAlerts(count - previousCount.current);
          }
          previousCount.current = count;
          setAlerts(data);
        }
      } catch {
        // ignore
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    // Polls are what spend the server's 100 requests per minute per user,
    // so during a rate-limit back-off the tick rests instead of firing.
    const interval = setInterval(() => {
      if (!isRateLimited()) void load();
    }, 30000);

    // Refresh immediately when a new alert arrives over the WebSocket.
    const onAlert = () => load();
    window.addEventListener('ourway:alert', onAlert);

    return () => {
      cancelled = true;
      clearInterval(interval);
      window.removeEventListener('ourway:alert', onAlert);
    };
  }, [severityFilter, statusFilter]);

  async function handleResolve(id: string) {
    try {
      await resolveAlert(id);
      setAlerts((a) => a.map((x) => (x.id === id ? { ...x, resolved: true } : x)));
    } catch {
      // ignore
    }
  }

  async function handleAcknowledge(id: string) {
    try {
      await acknowledgeAlert(id);
      setAlerts((a) => a.map((x) => (x.id === id ? { ...x, acknowledged: true } : x)));
    } catch {
      // ignore
    }
  }

  async function handleAssign(id: string, username: string) {
    try {
      await assignAlert(id, username);
      setAlerts((a) => a.map((x) => (x.id === id ? { ...x, assigned_to: username } : x)));
      setAssignFor(null);
      setAssignUser('');
    } catch {
      // ignore
    }
  }

  const filtered = alerts.filter((a) => {
    if (search) {
      const q = search.toLowerCase();
      if (!a.device_name.toLowerCase().includes(q) && !a.message.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const unresolvedCount = alerts.filter((a) => !a.resolved).length;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('alerts.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {unresolvedCount} open · {alerts.length} on the sheet
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
