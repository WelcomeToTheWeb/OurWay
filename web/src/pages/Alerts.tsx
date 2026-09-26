import { useEffect, useState, useRef } from 'react';
import {
  AlertTriangle,
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
import type { Alert, AlertSeverity } from '../types/alert';
import { useTranslation } from 'react-i18next';

const severityConfig: Record<AlertSeverity, { label: string; className: string; icon: typeof AlertTriangle }> = {
  critical: {
    label: 'Critical',
    className: 'bg-status-error/15 text-status-error ring-1 ring-inset ring-status-error/30',
    icon: AlertTriangle,
  },
  warning: {
    label: 'Warning',
    className: 'bg-status-warning/15 text-status-warning ring-1 ring-inset ring-status-warning/30',
    icon: AlertTriangle,
  },
  info: {
    label: 'Info',
    className: 'bg-accent/15 text-accent ring-1 ring-inset ring-accent/30',
    icon: Bell,
  },
};

const metricIcons: Record<string, typeof Activity> = {
  cpu: Cpu,
  ram: MemoryStick,
  disk: HardDrive,
};

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

export function Alerts() {
  const { t } = useTranslation();
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(true);
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [search, setSearch] = useState('');
  const [newAlerts, setNewAlerts] = useState(0);
  const previousCount = useRef(0);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        setLoading(true);
        const params: Record<string, string> = {};
        if (severityFilter !== 'all') params.severity = severityFilter;
        if (statusFilter === 'unresolved') params.resolved = 'false';
        if (statusFilter === 'resolved') params.resolved = 'true';

        const data = await getAlerts(params as any);
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
    const interval = setInterval(load, 30000);

    return () => {
      cancelled = true;
      clearInterval(interval);
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
      setAlerts((a) =>
        a.map((x) =>
          x.id === id ? { ...x, acknowledged: true } : x
        )
      );
    } catch {
      // ignore
    }
  }

  async function handleAssign(id: string) {
    try {
      // For now, assign to first available user (admin)
      await assignAlert(id, 'admin');
      setAlerts((a) =>
        a.map((x) =>
          x.id === id ? { ...x, assigned_to: 'admin' } : x
        )
      );
    } catch {
      // ignore
    }
  }

  const filtered = alerts.filter((a) => {
    if (search) {
      const q = search.toLowerCase();
      if (
        !a.device_name.toLowerCase().includes(q) &&
        !a.message.toLowerCase().includes(q)
      ) {
        return false;
      }
    }
    return true;
  });

  const unresolvedCount = alerts.filter((a) => !a.resolved).length;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('alerts.title')}</h1>
          <p className="text-sm text-text-secondary">
            {unresolvedCount} {t('common.active')} of {alerts.length} {t('common.all').toLowerCase()}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="flex items-center gap-1.5 rounded-full bg-status-online/10 px-2 py-0.5 text-xs text-status-online">
            <span className="h-2 w-2 rounded-full bg-status-online animate-pulse" />
            Auto-refreshing
          </span>
        </div>
      </div>

      {/* New alerts banner */}
      {newAlerts > 0 && (
        <div className="flex items-center justify-between rounded-xl border border-status-warning/30 bg-status-warning/10 px-4 py-3">
          <div className="flex items-center gap-3">
            <Bell className="h-5 w-5 text-status-warning" />
            <p className="text-sm text-status-warning">
              {newAlerts} new alert{newAlerts > 1 ? 's' : ''} since last check
            </p>
          </div>
          <button
            onClick={() => setNewAlerts(0)}
            className="rounded-lg bg-status-warning/20 px-3 py-1 text-xs text-status-warning transition-colors hover:bg-status-warning/30"
          >
            {t('common.close')}
          </button>
        </div>
      )}

      {/* Filters - responsive: stack on mobile, row on desktop */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('alerts.search')}
            className="w-full rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div className="flex flex-col gap-2 sm:flex-row sm:gap-3">
          <select
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
            className="w-full sm:w-auto rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
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
            className="w-full sm:w-auto rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
            aria-label="Filter by status"
          >
            <option value="all">{t('alerts.allStatuses')}</option>
            <option value="unresolved">{t('alerts.unresolved')}</option>
            <option value="resolved">{t('alerts.resolved')}</option>
          </select>
        </div>
      </div>

      {/* Alerts List */}
      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <Activity className="h-8 w-8 animate-spin text-accent" />
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
          <div className="relative">
            <div className="flex h-20 w-20 items-center justify-center rounded-full border border-bg-border bg-bg">
              <Bell className="h-8 w-8 text-text-muted" />
            </div>
            <div className="absolute -bottom-1 -right-1 flex h-6 w-6 items-center justify-center rounded-full bg-status-online">
              <Check className="h-3.5 w-3.5 text-text-primary" />
            </div>
          </div>
          <p className="mt-4 text-sm font-medium text-text-primary">{t('alerts.allClear')}</p>
          <p className="mt-1 text-xs text-text-muted">
            {t('alerts.noAlertsMatch')}
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {filtered.map((alert) => {
            const sev = severityConfig[alert.severity];
            const MetricIcon = metricIcons[alert.metric] || Activity;

            return (
              <div
                key={alert.id}
                className={`rounded-xl border p-4 transition-colors ${
                  alert.resolved
                    ? 'border-bg-border/50 bg-bg-card/50 opacity-60'
                    : 'border-bg-border bg-bg-card hover:border-accent/50'
                }`}
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="flex items-start gap-4">
                    <div
                      className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${
                        alert.resolved ? 'bg-bg' : sev.className
                      }`}
                    >
                      {alert.resolved ? (
                        <Check className="h-5 w-5 text-text-muted" />
                      ) : (
                        <MetricIcon className="h-5 w-5" />
                      )}
                    </div>
                    <div className="flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span
                          className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase ${sev.className}`}
                        >
                          {sev.label}
                        </span>
                        {alert.acknowledged && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-bg px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                            <UserCheck className="h-3 w-3" />
                            {t('alerts.acknowledged')}
                          </span>
                        )}
                        {alert.assigned_to && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-bg px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                            <UserPlus className="h-3 w-3" />
                            {alert.assigned_to}
                          </span>
                        )}
                        {alert.resolved && (
                          <span className="inline-flex items-center rounded-full bg-bg px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                            {t('alerts.resolved')}
                          </span>
                        )}
                        <span className="text-xs text-text-muted">
                          {timeAgo(alert.created_at)}
                        </span>
                      </div>
                      <p className="mt-1.5 text-sm font-medium text-text-primary">
                        {alert.message}
                      </p>
                      <p className="text-xs text-text-secondary">
                        {alert.device_name} • {alert.metric}
                      </p>
                    </div>
                  </div>
                  {!alert.resolved && (
                    <div className="flex shrink-0 gap-2">
                      {!alert.acknowledged && (
                        <button
                          onClick={() => handleAcknowledge(alert.id)}
                          className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-accent/80"
                          title={t('alerts.acknowledge')}
                        >
                          <UserCheck className="h-3.5 w-3.5" />
                          {t('alerts.ack')}
                        </button>
                      )}
                      {!alert.assigned_to && (
                        <button
                          onClick={() => handleAssign(alert.id)}
                          className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-bg"
                          title={t('alerts.assign')}
                        >
                          <UserPlus className="h-3.5 w-3.5" />
                          {t('alerts.assign')}
                        </button>
                      )}
                      <button
                        onClick={() => handleResolve(alert.id)}
                        className="flex items-center gap-1.5 rounded-lg bg-status-online px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-status-online/80"
                        title={t('alerts.resolve')}
                      >
                        <Check className="h-3.5 w-3.5" />
                        {t('alerts.resolve')}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
