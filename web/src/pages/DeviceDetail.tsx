import { useEffect, useState, useMemo, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  ArrowLeft,
  Trash2,
  Wifi,
  Server,
  Key,
  Cpu,
  MemoryStick,
  HardDrive,
  Network,
  Radio,
  Monitor,
  Apple,
  MousePointer2,
  ExternalLink,
  RefreshCw,
  Download,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  getDevice,
  deleteDevice,
  rebootDevice,
  startDeviceStream,
  stopDeviceStream,
  getMetricsHistory,
} from '../api/devices';
import { startSession } from '../api/sessions';
import { downloadInstaller, fetchInstallers } from '../api/installers';
import { listPolicies, getPatchOverview } from '../api/patching';
import type { PatchOverview } from '../types/patch';
import type { Session } from '../api/sessions';
import type { PatchPolicy } from '../types/patch';
import { Gauge } from '../components/Gauge';
import { LineChart } from '../components/LineChart';
import { InfoRow } from '../components/InfoRow';
import { TagEditor } from '../components/TagEditor';
import { CopyButton } from '../components/CopyButton';
import { ServiceRecord } from '../components/ServiceRecord';
import { Stamp } from '../components/Stamp';
import { SessionView } from '../components/SessionView';
import { useMetricsStore } from '../stores/metrics';
import { useAuth } from '../auth/context';
import { deviceRecord, policyForDevice } from '../utils/policyScope';
import { isRateLimited } from '../api/client';
import type { Device, Metrics } from '../types/device';

const osIcons: Record<string, typeof Monitor> = {
  linux: Server,
  windows: Monitor,
  darwin: Apple,
};

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function osLabel(os: string): string {
  switch (os) {
    case 'linux': return 'Linux';
    case 'windows': return 'Windows';
    case 'darwin': return 'macOS';
    default: return os;
  }
}

/** Record-stamp time: ISO date, 24-hour, with the zone it is stated in. */
function utcStamp(iso: string, withClock = true): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  const date = d.toISOString().slice(0, 10);
  return withClock ? `${date} ${d.toISOString().slice(11, 16)} UTC` : date;
}

/**
 * One unit's record: the header is the decal (name, state, identity,
 * actions), the ruled fields below are what the sheet knows about it, and
 * the charts carry the day's recorded shape — not just whatever arrives
 * while the tab happens to be open.
 */
export function DeviceDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { accessToken, hasAnyRole } = useAuth();
  const canEditTags = hasAnyRole(['admin', 'manager', 'technician']);
  const [device, setDevice] = useState<Device | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [session, setSession] = useState<Session | null>(null);
  const [startingSession, setStartingSession] = useState(false);
  const [viewerLaunched, setViewerLaunched] = useState(false);
  const [rebooting, setRebooting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [policies, setPolicies] = useState<PatchPolicy[]>([]);
  const [overview, setOverview] = useState<PatchOverview | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  const history = useMetricsStore((s) => (id ? s.history[id] : null));
  const addMetric = useMetricsStore((s) => s.addMetric);
  const seedHistory = useMetricsStore((s) => s.seedHistory);

  const deviceId = id!;
  const canSeePatches = hasAnyRole(['admin', 'manager', 'technician']);

  const load = useCallback(async () => {
    try {
      const data = await getDevice(deviceId);
      setDevice(data);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [deviceId]);

  useEffect(() => {
    if (!id) return;
    void load();
    listPolicies().then(setPolicies).catch(() => setPolicies([]));
    if (canSeePatches) getPatchOverview().then(setOverview).catch(() => setOverview(null));

    // Recorded history first: a page opened on a unit shows its day, and
    // the live stream then extends it.
    const from = new Date(Date.now() - 24 * 3600_000).toISOString();
    getMetricsHistory(deviceId, from)
      .then((rows) => seedHistory(deviceId, rows))
      .catch(() => {});

    const interval = setInterval(() => {
      if (isRateLimited()) return;
      void load();
    }, 15000);

    return () => clearInterval(interval);
  }, [id, load, seedHistory, deviceId, canSeePatches]);

  // WebSocket connection for real-time metrics
  useEffect(() => {
    if (!id || !accessToken) return;

    const connect = () => {
      if (wsRef.current?.readyState === WebSocket.OPEN) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.host;
      const url = `${protocol}//${host}/ws`;
      const ws = new WebSocket(url, ['ourway-auth', accessToken]);
      wsRef.current = ws;

      ws.onopen = () => setConnected(true);

      ws.onmessage = (event) => {
        try {
          // Server messages are {type, payload} envelopes; the device id and
          // metrics live in the payload, not at the top level.
          const raw = JSON.parse(event.data) as {
            type: string;
            payload?: { device_id?: string; metrics?: Metrics };
          };
          if (raw.type === 'metrics' && raw.payload?.device_id === id && raw.payload.metrics) {
            setConnected(true);
            addMetric(raw.payload.device_id, raw.payload.metrics);
          }
        } catch {
          // ignore
        }
      };

      ws.onclose = () => {
        setConnected(false);
        reconnectTimeout.current = setTimeout(connect, 3000);
      };

      ws.onerror = () => ws.close();
    };

    connect();

    return () => {
      wsRef.current?.close();
      if (reconnectTimeout.current) clearTimeout(reconnectTimeout.current);
    };
  }, [id, accessToken, addMetric]);

  // H5: while the detail page is open, switch the device to 2 s
  // streaming metrics; revert to the normal interval when leaving.
  // 503s (offline device) are harmless: the interval resets on the
  // agent's next connection.
  useEffect(() => {
    if (!id) return;
    startDeviceStream(id, 2).catch(() => {});
    return () => {
      stopDeviceStream(id).catch(() => {});
    };
  }, [id]);

  const latestMetrics: Metrics | null = useMemo(() => {
    if (!history || history.cpu.length === 0) return null;
    const last = history.cpu.length - 1;
    return {
      cpu: history.cpu[last],
      ram: history.ram[last],
      ram_used: 0,
      ram_total: 0,
      disk_usage: history.disk[last],
      disk_used: 0,
      disk_total: 0,
      net_in: history.netIn[last],
      net_out: history.netOut[last],
      uptime: 0,
      processes: 0,
    };
  }, [history]);

  const chartData = useMemo(() => {
    if (!history) return [];
    return history.timestamps.map((ts, i) => ({
      time: ts,
      cpu: history.cpu[i],
      ram: history.ram[i],
      disk: history.disk[i],
      netIn: history.netIn[i],
      netOut: history.netOut[i],
    }));
  }, [history]);

  async function handleReboot() {
    if (!device) return;
    setActionError(null);
    setRebooting(true);
    try {
      await rebootDevice(device.id);
    } catch (err) {
      setActionError(`Failed to reboot device: ${(err as Error).message}`);
    } finally {
      setRebooting(false);
    }
  }

  async function handleDelete() {
    if (!device) return;
    if (!confirm(`Delete device "${device.name}"? This cannot be undone.`)) return;
    try {
      await deleteDevice(device.id);
      navigate('/devices');
    } catch (err) {
      setError((err as Error).message);
    }
  }

  async function handleStartSession() {
    if (!device) return;
    setStartingSession(true);
    try {
      const { session: started } = await startSession(device.id);
      setSession(started);
    } catch (err) {
      setError(`Failed to start session: ${(err as Error).message}`);
    } finally {
      setStartingSession(false);
    }
  }

  // Starts a session and hands it to the native viewer through the
  // ourway:// protocol; the viewer owns the session from then on.
  async function handleOpenInViewer() {
    if (!device) return;
    setStartingSession(true);
    try {
      const { viewer_url } = await startSession(device.id);
      setViewerLaunched(true);
      window.location.assign(viewer_url);
    } catch (err) {
      setError(`Failed to start session: ${(err as Error).message}`);
    } finally {
      setStartingSession(false);
    }
  }

  async function handleDownloadViewer() {
    try {
      const viewer = (await fetchInstallers()).find((i) => i.kind === 'viewer');
      if (!viewer) {
        setError('No viewer build is available on this server.');
        return;
      }
      await downloadInstaller(viewer);
    } catch (err) {
      setError(`Failed to download viewer: ${(err as Error).message}`);
    }
  }

  if (loading) {
    return (
      <div className="flex h-64 flex-col items-center justify-center gap-3">
        <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        <p className="cap cap-sm text-text-muted">{t('common.loading')}</p>
      </div>
    );
  }

  if (error || !device) {
    return (
      <div role="alert" className="panel border-[var(--error)] p-4">
        <p className="cap cap-sm text-[var(--error)]">Unit record unavailable</p>
        <p className="mt-1 text-sm text-text-secondary">{error || 'No device matches this record.'}</p>
        <button onClick={() => navigate('/devices')} className="btn mt-3">
          <ArrowLeft className="h-3.5 w-3.5" /> {t('common.back')}
        </button>
      </div>
    );
  }

  const OsIcon = osIcons[device.os] || Monitor;
  const hasData = chartData.length > 0;
  const compliance = overview?.devices.find((d) => d.device_id === device.id);
  const record = deviceRecord(device, compliance, policyForDevice(policies, device), {
    lastSeen: t('record.lastSeen'),
    nextDue: t('record.nextDue'),
    interval: t('record.interval'),
    noPolicy: t('record.noPolicy'),
  });

  return (
    <div className="space-y-4">
      {actionError && (
        <div className="flex flex-wrap items-center justify-between gap-3 panel border-[var(--error)] px-4 py-3">
          <p className="text-sm text-[var(--error)]">{actionError}</p>
          <button onClick={() => setActionError(null)} className="btn-quiet btn">
            {t('common.dismiss')}
          </button>
        </div>
      )}

      {/* Header: the decal */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <button onClick={() => navigate(-1)} className="btn mt-1">
            <ArrowLeft className="h-3.5 w-3.5" /> {t('common.back')}
          </button>
          <div className="flex h-11 w-11 items-center justify-center border border-rule bg-[var(--field)]">
            <OsIcon className="h-5 w-5 text-text-secondary" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2.5">
              <h1 className="text-[26px]">{device.name}</h1>
              <Stamp tone={device.status === 'online' ? 'online' : device.status === 'alert' ? 'alert' : 'offline'}>
                {device.status}
              </Stamp>
              {connected && hasData && <Stamp tone="service">streaming 2s</Stamp>}
            </div>
            <p className="cap cap-sm mt-1 text-text-muted">
              {device.hostname} · {osLabel(device.os)} · {device.arch}
              {device.agent_version && ` · v${device.agent_version}`} · {device.public_ip}
            </p>
            <div className="mt-2">
              <TagEditor
                deviceId={device.id}
                tags={device.tags ?? []}
                readOnly={!canEditTags}
                onChange={(tags) => setDevice((d) => (d ? { ...d, tags } : d))}
              />
            </div>
            {device.reboot_pending && (
              <p className="cap cap-sm mt-2 text-[var(--due)]">
                Reboot pending — updates installed, restart required
              </p>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={handleStartSession}
            disabled={startingSession}
            className="btn-primary"
          >
            <MousePointer2 className="h-3.5 w-3.5" />
            {startingSession ? 'Connecting…' : 'Remote session'}
          </button>
          <button
            onClick={handleOpenInViewer}
            disabled={startingSession}
            title="Open in the OurWay Viewer app (Ctrl+Alt+Del, multiple monitors, clipboard)"
            className="btn"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Open in viewer
          </button>
          <button onClick={handleReboot} disabled={rebooting} className="btn">
            <RefreshCw className={`h-3.5 w-3.5 ${rebooting ? 'animate-spin' : ''}`} />
            {t('devices.reboot', 'Reboot')}
          </button>
          <button onClick={handleDelete} className="btn text-[var(--stamp)]">
            <Trash2 className="h-3.5 w-3.5" />
            {t('common.delete')}
          </button>
        </div>
      </div>

      <ServiceRecord {...record} />

      {/* Ruled record fields */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="min-w-0 panel p-4">
          <h2 className="panel-head">
            <Wifi className="h-3.5 w-3.5" aria-hidden="true" /> Network
          </h2>
          <InfoRow
            label="Public IP"
            value={
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate" title={device.public_ip}>
                  {device.public_ip}
                </span>
                <CopyButton text={device.public_ip} />
              </div>
            }
          />
          <InfoRow
            label="Private IP"
            icon={Network}
            value={
              device.private_ip ? (
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate" title={device.private_ip}>
                    {device.private_ip}
                  </span>
                  <CopyButton text={device.private_ip} />
                </div>
              ) : (
                <span className="text-text-muted">—</span>
              )
            }
          />
        </div>

        <div className="min-w-0 panel p-4">
          <h2 className="panel-head">
            <Server className="h-3.5 w-3.5" aria-hidden="true" /> System
          </h2>
          <InfoRow label="Agent version" icon={Cpu} value={device.agent_version || '—'} />
          <InfoRow label="Architecture" value={device.arch} />
          {device.device_key && (
            <InfoRow
              label="Device key"
              icon={Key}
              value={
                <div className="flex min-w-0 items-center gap-2">
                  <span className="ow-num truncate text-[12px]" title={device.device_key}>
                    {device.device_key}
                  </span>
                  <CopyButton text={device.device_key} />
                </div>
              }
            />
          )}
        </div>

        <div className="min-w-0 panel p-4">
          <h2 className="panel-head">
            <Radio className="h-3.5 w-3.5" aria-hidden="true" /> Stated times
          </h2>
          <InfoRow label="Last seen" value={utcStamp(device.last_seen)} />
          <InfoRow label="Enrolled" value={utcStamp(device.created_at, false)} />
          <InfoRow
            label="Metric samples"
            value={
              <span className="inline-flex items-center gap-2">
                <span className="ow-num">{chartData.length}</span>
                <span className="cap cap-sm text-text-muted">last 24 h</span>
              </span>
            }
          />
        </div>
      </div>

      {/* Ruled measures */}
      <section className="panel p-4">
        <h2 className="panel-head">
          <Radio className="h-3.5 w-3.5" aria-hidden="true" /> Measured now
          <span className="cap-sm cap ml-auto font-normal normal-case tracking-normal text-text-muted">
            {hasData
              ? connected
                ? 'live · 2 s stream'
                : 'last recorded sample'
              : 'no sample recorded yet'}
          </span>
        </h2>
        <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Gauge
            value={latestMetrics?.cpu ?? 0}
            label="CPU usage"
            unit="%"
            history={history?.cpu.slice(-40)}
          />
          <Gauge
            value={latestMetrics?.ram ?? 0}
            label="Memory"
            unit="%"
            history={history?.ram.slice(-40)}
          />
          <Gauge
            value={latestMetrics?.disk_usage ?? 0}
            label="Disk"
            unit="%"
            history={history?.disk.slice(-40)}
          />
        </div>
        <div className="mt-3 grid grid-cols-2 gap-3 md:grid-cols-4">
          <div className="field">
            <span className="field-label">Network in</span>
            <span className="ow-num mt-1 block text-[17px] font-semibold">
              {hasData ? `${formatBytes(latestMetrics!.net_in)}/s` : '—'}
            </span>
            <span className="cap cap-sm mt-0.5 block text-text-muted">since last sample</span>
          </div>
          <div className="field">
            <span className="field-label">Network out</span>
            <span className="ow-num mt-1 block text-[17px] font-semibold">
              {hasData ? `${formatBytes(latestMetrics!.net_out)}/s` : '—'}
            </span>
            <span className="cap cap-sm mt-0.5 block text-text-muted">since last sample</span>
          </div>
          <div className="field">
            <span className="field-label">Samples in view</span>
            <span className="ow-num mt-1 block text-[17px] font-semibold">{chartData.length}</span>
            <span className="cap cap-sm mt-0.5 block text-text-muted">
              {hasData ? `${chartData[0].time} → ${chartData[chartData.length - 1].time} UTC` : 'nothing recorded'}
            </span>
          </div>
          <div className="field">
            <span className="field-label">Live link</span>
            <span className="mt-1 block">
              <Stamp tone={connected ? 'online' : 'offline'}>{connected ? 'open' : 'closed'}</Stamp>
            </span>
            <span className="cap cap-sm mt-0.5 block text-text-muted">
              {connected ? 'metrics arriving' : 'recording stopped — showing the last day'}
            </span>
          </div>
        </div>
      </section>

      {/* Recorded day */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <div className="panel p-4">
          <h2 className="panel-head">
            <Cpu className="h-3.5 w-3.5" aria-hidden="true" /> CPU %
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'cpu', name: 'CPU %', color: 'var(--service)' }]}
            height={190}
            yDomain={[0, 100]}
          />
        </div>
        <div className="panel p-4">
          <h2 className="panel-head">
            <MemoryStick className="h-3.5 w-3.5" aria-hidden="true" /> Memory %
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'ram', name: 'RAM %', color: 'var(--online)' }]}
            height={190}
            yDomain={[0, 100]}
          />
        </div>
        <div className="panel p-4">
          <h2 className="panel-head">
            <HardDrive className="h-3.5 w-3.5" aria-hidden="true" /> Disk %
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'disk', name: 'Disk %', color: 'var(--due)' }]}
            height={190}
            yDomain={[0, 100]}
          />
        </div>
        <div className="panel p-4">
          <h2 className="panel-head">
            <Network className="h-3.5 w-3.5" aria-hidden="true" /> Network bytes / s
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[
              { key: 'netIn', name: 'In', color: 'var(--service)' },
              { key: 'netOut', name: 'Out', color: 'var(--stamp)' },
            ]}
            height={190}
            yLabel="bytes/s"
          />
        </div>
      </div>

      {viewerLaunched && (
        <div className="flex flex-wrap items-center justify-between gap-3 panel px-4 py-3">
          <p className="text-sm text-text-secondary">
            Launching the OurWay Viewer… If nothing opened, the viewer is not installed on this computer.
          </p>
          <div className="flex gap-2">
            <button onClick={() => void handleDownloadViewer()} className="btn-primary">
              <Download className="h-3.5 w-3.5" /> Download viewer (Windows)
            </button>
            <button onClick={() => setViewerLaunched(false)} className="btn">
              {t('common.dismiss')}
            </button>
          </div>
        </div>
      )}

      {/* Remote Session View */}
      {session && <SessionView session={session} onClose={() => setSession(null)} />}
    </div>
  );
}
