import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Server, CheckCircle, XCircle, Bell, Activity, ShieldCheck, ShieldAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDevices } from '../hooks/useDevices';
import { DeviceCard } from '../components/DeviceCard';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';
import { getPatchOverview } from '../api/patching';
import type { PatchOverview } from '../types/patch';
import { buildAttentionList, osBreakdown } from '../utils/attention';

export function Dashboard() {
  const { t } = useTranslation();
  const { devices, loading, error } = useDevices();
  const { accessToken, hasAnyRole } = useAuth();
  const { connected } = useWebSocket(accessToken);
  const canSeePatches = hasAnyRole(['admin', 'manager', 'technician']);
  const [overview, setOverview] = useState<PatchOverview | null>(null);

  useEffect(() => {
    if (!canSeePatches) return;
    getPatchOverview().then(setOverview).catch(() => setOverview(null));
  }, [canSeePatches]);

  const attention = useMemo(() => buildAttentionList(devices, overview), [devices, overview]);
  const osCounts = useMemo(() => osBreakdown(devices), [devices]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Activity className="h-8 w-8 animate-spin text-accent" />
      </div>
    );
  }

  if (error) {
    return <p className="text-status-error">Error: {error}</p>;
  }

  const online = devices.filter((d) => d.status === 'online').length;
  const offline = devices.filter((d) => d.status === 'offline').length;
  const alerts = devices.filter((d) => d.status === 'alert').length;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('dashboard.title')}</h1>
          <p className="text-sm text-text-secondary">{t('dashboard.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${connected ? 'bg-status-online animate-pulse' : 'bg-status-offline'}`}
          />
          <span className="text-xs text-text-secondary">
            {connected ? t('dashboard.live') : t('common.offline')}
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={Server} label={t('dashboard.totalDevices')} value={devices.length} color="text-accent" />
        <StatCard icon={CheckCircle} label={t('common.online')} value={online} color="text-status-online" />
        <StatCard icon={XCircle} label={t('common.offline')} value={offline} color="text-status-offline" />
        <StatCard icon={Bell} label={t('dashboard.activeAlerts')} value={alerts} color="text-status-error" />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <section aria-label="Needs attention" className="rounded-xl border border-bg-border bg-bg-card p-4 lg:col-span-2">
          <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-text-muted">Needs attention</h2>
          {attention.length === 0 ? (
            <div className="flex items-center gap-2 py-6 text-sm text-text-secondary">
              <ShieldCheck className="h-5 w-5 text-status-online" /> Nothing needs attention right now.
            </div>
          ) : (
            <ul className="divide-y divide-bg-border">
              {attention.map(({ device, reasons }) => (
                <li key={device.id}>
                  <Link to={`/devices/${device.id}`} className="flex items-center justify-between gap-3 py-2.5 hover:text-accent">
                    <span className="text-sm font-medium text-text-primary">{device.name}</span>
                    <span className="text-xs text-text-secondary">{reasons.join(' · ')}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>

        <div className="space-y-4">
          {canSeePatches && overview && (
            <Link to="/patches" aria-label="Patch compliance" className="block rounded-xl border border-bg-border bg-bg-card p-4 transition-colors hover:border-accent">
              <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-text-muted">Patch compliance</h2>
              <div className="flex items-center gap-3">
                {overview.totals.critical > 0 ? (
                  <ShieldAlert className="h-8 w-8 text-status-error" />
                ) : (
                  <ShieldCheck className="h-8 w-8 text-status-online" />
                )}
                <div>
                  <p className="text-2xl font-semibold text-text-primary">
                    {overview.totals.devices > 0 ? Math.round((overview.totals.compliant / overview.totals.devices) * 100) : 100}%
                  </p>
                  <p className="text-xs text-text-secondary">
                    {overview.totals.pending} outstanding · {overview.totals.critical} critical
                  </p>
                </div>
              </div>
            </Link>
          )}
          <section aria-label="Operating systems" className="rounded-xl border border-bg-border bg-bg-card p-4">
            <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-text-muted">Systems</h2>
            {osCounts.length === 0 ? (
              <p className="text-sm text-text-secondary">No devices yet.</p>
            ) : (
              <ul className="space-y-2">
                {osCounts.map(({ os, count }) => (
                  <li key={os} className="text-xs text-text-secondary">
                    <div className="mb-1 flex justify-between">
                      <span>{os === 'darwin' ? 'macOS' : os === 'windows' ? 'Windows' : 'Linux'}</span>
                      <span>{count}</span>
                    </div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-bg">
                      <div className="h-full rounded-full bg-accent" style={{ width: `${(count / devices.length) * 100}%` }} />
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      </div>

      <div>
        <h2 className="text-sm font-medium text-text-muted uppercase tracking-wide mb-4">
          {t('devices.title')}
        </h2>
        {devices.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
            <Server className="h-10 w-10 text-text-muted" />
            <p className="mt-3 text-sm">{t('dashboard.noDevices')}</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {devices.map((device) => (
              <DeviceCard key={device.id} device={device} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  color,
}: {
  icon: typeof Server;
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div className="flex items-center gap-4 rounded-xl border border-bg-border bg-bg-card p-4">
      <div className={`flex h-10 w-10 items-center justify-center rounded-lg bg-bg ${color}`}>
        <Icon className="h-5 w-5" />
      </div>
      <div>
        <p className="text-2xl font-semibold text-text-primary">{value}</p>
        <p className="text-xs text-text-secondary">{label}</p>
      </div>
    </div>
  );
}
