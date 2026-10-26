import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { RefreshCw, RotateCw, ShieldCheck, ShieldAlert, Server, Radio } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDevices } from '../hooks/useDevices';
import { useWebSocket } from '../hooks/useWebSocket';
import { useAuth } from '../auth/context';
import { DeviceCard } from '../components/DeviceCard';
import { FieldBox } from '../components/FieldBox';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { getPatchOverview } from '../api/patching';
import { useRateLimit } from '../hooks/useRateLimit';
import { listSessions, type Session } from '../api/sessions';
import type { DeviceCompliance, PatchOverview, PatchPolicy } from '../types/patch';
import { buildAttentionList, osBreakdown } from '../utils/attention';
import { listPolicies } from '../api/patching';
import { useFleetHistory } from '../stores/fleetHistory';

/**
 * The console's front page: what the fleet is doing right now, which units
 * need somebody, and where each unit sits in its service interval.
 */
export function Dashboard() {
  const { t } = useTranslation();
  const { devices, loading, error, refresh } = useDevices();
  const { accessToken, hasAnyRole } = useAuth();
  const { connected } = useWebSocket(accessToken);
  const canSeePatches = hasAnyRole(['admin', 'manager', 'technician']);

  const [overview, setOverview] = useState<PatchOverview | null>(null);
  const [policies, setPolicies] = useState<PatchPolicy[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const { limited, secondsLeft } = useRateLimit();
  const samples = useFleetHistory((s) => s.samples);
  const push = useFleetHistory((s) => s.push);

  const load = useCallback(async () => {
    try {
      const [o, p, s] = await Promise.all([
        canSeePatches ? getPatchOverview() : Promise.resolve(null),
        listPolicies().catch(() => [] as PatchPolicy[]),
        listSessions().catch(() => [] as Session[]),
      ]);
      setOverview(o);
      setPolicies(p);
      setSessions(s);
    } catch {
      setOverview(null);
    }
  }, [canSeePatches]);

  useEffect(() => {
    void load();
  }, [load]);

  const online = devices.filter((d) => d.status === 'online').length;
  const alerts = devices.filter((d) => d.status === 'alert').length;
  const complianceById = useMemo(
    () => new Map((overview?.devices ?? []).map((d) => [d.device_id, d])),
    [overview],
  );

  // Keep the session's own history of the headline figures, so every
  // number on this page can show the recent past it came from.
  useEffect(() => {
    const totals = overview?.totals;
    const sample = {
      t: Date.now(),
      online,
      offline: devices.length - online,
      alerts,
      critical: totals?.critical ?? 0,
      rebootPending: totals?.reboot_pending ?? 0,
      compliant: totals?.compliant ?? 0,
      pending: totals?.pending ?? 0,
    };
    push(sample);
    const id = setInterval(() => push({ ...sample, t: Date.now() }), 15_000);
    return () => clearInterval(id);
  }, [online, alerts, devices.length, overview, push]);

  const series = useMemo(() => {
    const empty: number[] = [];
    return {
      online: samples.length > 1 ? samples.map((s) => s.online) : empty,
      alerts: samples.length > 1 ? samples.map((s) => s.alerts) : empty,
      critical: samples.length > 1 ? samples.map((s) => s.critical) : empty,
      reboot: samples.length > 1 ? samples.map((s) => s.rebootPending) : empty,
      compliance:
        samples.length > 1
          ? samples.map((s) => (s.online + s.offline > 0 ? Math.round((s.compliant / Math.max(s.compliant + s.pending, 1)) * 100) : 100))
          : empty,
    };
  }, [samples]);

  const attention = useMemo(() => buildAttentionList(devices, overview, 40), [devices, overview]);
  const osCounts = useMemo(() => osBreakdown(devices), [devices]);
  const guide = useGuideWords(attention.length);

  const totals = overview?.totals;
  const compliancePct = totals && totals.devices > 0 ? Math.round((totals.compliant / totals.devices) * 100) : 100;
  const rebootList = (overview?.devices ?? []).filter((d) => d.reboot_pending);
  const criticalList = (overview?.devices ?? []).filter((d) => d.critical > 0).slice(0, 6);

  if (loading) {
    return (
      <div className="flex h-64 flex-col items-center justify-center gap-3">
        <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
      </div>
    );
  }

  if (error) {
    // A 429 is not an outage: the server is honouring its per-user minute
    // budget and the console is resting. Say that, with the countdown, so
    // nobody restarts anything.
    const rateLimited = /429/.test(error) || limited;
    return (
      <div role="alert" className="panel border-[var(--error)] p-4">
        <p className="cap cap-sm text-[var(--error)]">
          {rateLimited ? t('sheet.rateLimited') : t('sheet.consoleUnavailable')}
        </p>
        <p className="mt-1 text-sm text-text-secondary">
          {rateLimited
            ? t('sheet.rateLimitedHelp', { seconds: secondsLeft })
            : error}
        </p>
        <button onClick={refresh} className="btn mt-3">
          <RefreshCw className="h-3.5 w-3.5" /> {t('common.refresh')}
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('dashboard.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {devices.length} unit{devices.length === 1 ? '' : 's'} ·{' '}
            {connected ? 'link live' : 'link down — figures from the last refresh'} ·{' '}
            {new Date().toISOString().slice(11, 16)} UTC
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={() => void load()} className="btn btn-quiet">
            <RefreshCw className="h-3.5 w-3.5" /> {t('sheet.reload')}
          </button>
          <button onClick={refresh} className="btn">
            <RefreshCw className="h-3.5 w-3.5" /> {t('common.refresh')}
          </button>
        </div>
      </div>

      {/* Compliance rail: five stamped numerals, each with its recent past */}
      <section aria-label="Fleet headlines" className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
        <FieldBox
          label={t('dashboard.onlineDevices')}
          value={
            <span className="ow-num">
              {online}
              <span className="text-[13px] font-medium text-text-muted">/{devices.length}</span>
            </span>
          }
          tone={online === devices.length ? 'ok' : 'default'}
          hint={`${devices.length - online} offline`}
          history={series.online}
        />
        <FieldBox
          label={t('dashboard.activeAlerts')}
          value={<span className="ow-num">{alerts}</span>}
          tone={alerts > 0 ? 'alert' : 'ok'}
          hint={alerts > 0 ? 'thresholds breached' : 'no thresholds breached'}
          history={series.alerts}
        />
        {canSeePatches && (
          <FieldBox
            label="Critical updates"
            value={<span className="ow-num">{totals?.critical ?? 0}</span>}
            tone={(totals?.critical ?? 0) > 0 ? 'alert' : 'ok'}
            hint={`${totals?.pending ?? 0} outstanding`}
            history={series.critical}
          />
        )}
        {canSeePatches && (
          <FieldBox
            label="Reboot pending"
            value={<span className="ow-num">{totals?.reboot_pending ?? 0}</span>}
            tone={(totals?.reboot_pending ?? 0) > 0 ? 'due' : 'ok'}
            hint="awaiting a window"
            history={series.reboot}
          />
        )}
        {canSeePatches && (
          <FieldBox
            label="Patch compliance"
            value={<span className="ow-num">{compliancePct}%</span>}
            tone={compliancePct === 100 ? 'ok' : compliancePct >= 80 ? 'due' : 'alert'}
            hint={`${totals?.compliant ?? 0} of ${totals?.devices ?? 0} units clear`}
            history={series.compliance}
            historyMax={100}
          />
        )}
      </section>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_17rem]">
        <div className="min-w-0 space-y-5">
          {/* Needs-attention ledger, with guide words */}
          <section className="panel overflow-hidden" aria-label="Needs attention">
            <div className="flex items-baseline justify-between gap-3 px-3 py-2">
              <h2 className="cap cap-lg text-text-muted">{t('sheet.needsAttention')}</h2>
              <Link to="/alerts" className="cap-sm cap text-[var(--service)] hover:underline">
                {t('sheet.alertCentre')}
              </Link>
            </div>
            {attention.length === 0 ? (
              <div className="flex items-center gap-3 px-3 py-8">
                <ShieldCheck className="h-6 w-6 text-[var(--online)]" aria-hidden="true" />
                <p className="text-sm text-text-secondary">
                  Nothing is asking for anyone right now — {online} online, no alerts, no critical updates.
                </p>
              </div>
            ) : (
              <>
                <GuideWordsHead
                  total={attention.length}
                  unit={t('sheet.flagged')}
                  first={attention[guide.first]?.device.name}
                  last={attention[guide.last]?.device.name}
                />
                <div ref={guide.ref} className="max-h-[19rem] overflow-y-auto">
                  <table className="ledger">
                    <thead>
                      <tr>
                        <th>Unit</th>
                        <th>Why</th>
                        <th>State</th>
                        <th className="text-right">Last seen</th>
                      </tr>
                    </thead>
                    <tbody>
                      {attention.map(({ device, reasons }) => (
                        <tr key={device.id} data-guide className="text-[13px]">
                          <td>
                            <Link
                              to={`/devices/${device.id}`}
                              className="font-semibold text-[var(--service)] hover:underline"
                            >
                              {device.name}
                            </Link>
                            <span className="cap-sm cap ml-2 text-text-muted">{device.os}</span>
                          </td>
                          <td className="text-text-secondary">{reasons.join(' · ')}</td>
                          <td>
                            <Stamp tone={device.status === 'alert' ? 'alert' : device.status === 'offline' ? 'offline' : 'due'}>
                              {device.status}
                            </Stamp>
                          </td>
                          <td className="ow-num text-right text-[11px] text-text-muted">{device.last_seen.slice(11, 16)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </section>

          {/* The decal grid: every unit's record on one sheet */}
          <section aria-label={t('devices.title')}>
            <div className="mb-2 flex items-baseline justify-between gap-3">
              <h2 className="cap cap-lg text-text-muted">{t('devices.title')}</h2>
              <Link to="/devices" className="cap-sm cap text-[var(--service)] hover:underline">
                {t('sheet.allUnits', { count: devices.length })}
              </Link>
            </div>
            {devices.length === 0 ? (
              <div className="panel flex flex-col items-center gap-3 px-6 py-14 text-center">
                <Server className="h-8 w-8 text-text-muted" aria-hidden="true" />
                <p className="text-sm font-semibold text-text-primary">{t('dashboard.noDevices')}</p>
                <p className="max-w-md text-sm text-text-secondary">
                  Enrol the first unit with the one-command installer — it registers with this server and starts
                  reporting within a heartbeat.
                </p>
                <Link to="/devices" className="btn btn-primary mt-1">
                  {t('sheet.installAgent')}
                </Link>
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {devices.slice(0, 12).map((device) => (
                  <DeviceCard
                    key={device.id}
                    device={device}
                    compliance={complianceById.get(device.id)}
                    policies={policies}
                  />
                ))}
              </div>
            )}
          </section>
        </div>

        {/* Due board */}
        <div className="space-y-4">
          {canSeePatches && (
            <section className="panel" aria-label="Due now">
              <div className="panel-head">
                <h2 className="cap cap-lg text-text-muted">{t('sheet.dueNow')}</h2>
                <Link to="/patches" className="cap cap-sm ml-auto text-[var(--service)] hover:underline">
                  Patches
                </Link>
              </div>
              {criticalList.length === 0 ? (
                <div className="flex items-center gap-2 px-3 py-4">
                  <ShieldCheck className="h-5 w-5 text-[var(--online)]" aria-hidden="true" />
                  <p className="text-[13px] text-text-secondary">No critical updates outstanding.</p>
                </div>
              ) : (
                <ul>
                  {criticalList.map((d: DeviceCompliance) => (
                    <li key={d.device_id} className="flex items-center justify-between gap-2 border-b border-rule px-3 py-2 last:border-b-0">
                      <Link
                        to={`/devices/${d.device_id}`}
                        className="min-w-0 truncate text-[13px] font-semibold text-text-primary hover:text-[var(--service)]"
                      >
                        {d.name}
                      </Link>
                      <span className="flex shrink-0 items-center gap-1 text-[var(--error)]">
                        <ShieldAlert className="h-3.5 w-3.5" aria-hidden="true" />
                        <span className="ow-num text-[12px]">{d.critical}</span>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}

          {canSeePatches && rebootList.length > 0 && (
            <section className="panel" aria-label="Reboot pending">
              <div className="panel-head">
                <h2 className="cap cap-lg text-text-muted">{t('sheet.rebootPending')}</h2>
                <span className="ow-num ml-auto text-[12px] text-[var(--due)]">{rebootList.length}</span>
              </div>
              <ul>
                {rebootList.slice(0, 6).map((d) => (
                  <li key={d.device_id} className="flex items-center gap-2 border-b border-rule px-3 py-2 last:border-b-0">
                    <RotateCw className="h-3.5 w-3.5 shrink-0 text-[var(--due)]" aria-hidden="true" />
                    <Link
                      to={`/devices/${d.device_id}`}
                      className="min-w-0 truncate text-[13px] text-text-primary hover:text-[var(--service)]"
                    >
                      {d.name}
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}

          <section className="panel" aria-label="Live sessions">
            <div className="panel-head">
              <h2 className="cap cap-lg text-text-muted">{t('sheet.liveSessions')}</h2>
              <Link to="/sessions" className="cap cap-sm ml-auto text-[var(--service)] hover:underline">
                Sessions
              </Link>
            </div>
            {sessions.length === 0 ? (
              <p className="px-3 py-4 text-[13px] text-text-secondary">Nobody is remote-controlled right now.</p>
            ) : (
              <ul>
                {sessions.slice(0, 5).map((s) => (
                  <li key={s.id} className="flex items-center gap-2 border-b border-rule px-3 py-2 text-[12px] last:border-b-0">
                    <Radio className="h-3.5 w-3.5 shrink-0 text-[var(--service)]" aria-hidden="true" />
                    <Link to={`/devices/${s.device_id}`} className="ow-num truncate text-text-primary hover:text-[var(--service)]">
                      {s.device_id.slice(0, 8)}
                    </Link>
                    <Stamp tone={s.status === 'active' ? 'online' : 'neutral'}>{s.status}</Stamp>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="panel px-3 py-2.5" aria-label="Systems">
            <h2 className="cap cap-lg text-text-muted">{t('sheet.systems')}</h2>
            {osCounts.length === 0 ? (
              <p className="mt-2 text-[13px] text-text-secondary">No units enrolled.</p>
            ) : (
              <ul className="mt-2 space-y-2">
                {osCounts.map(({ os, count }) => (
                  <li key={os}>
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="cap-sm cap text-text-secondary">
                        {os === 'darwin' ? 'macOS' : os === 'windows' ? 'Windows' : 'Linux'}
                      </span>
                      <span className="ow-num text-[12px] text-text-primary">{count}</span>
                    </div>
                    <div className="mt-1 h-[6px] border border-rule bg-[var(--field)]">
                      <div
                        className="h-full bg-[var(--service)] transition-[width] duration-400 ease-[var(--ease-press)]"
                        style={{ width: `${(count / devices.length) * 100}%` }}
                      />
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>

          {samples.length > 1 && (
            <p className="text-[11px] leading-snug text-text-muted">
              History strips cover this console session ({samples.length} samples), not server-side archives.
            </p>
          )}
        </div>
      </div>

    </div>
  );
}
