import { useEffect, useState, useMemo, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  ArrowLeft,
  Trash2,
  Clock,
  Activity,
  Server,
  Wifi,
  ArrowUpRight,
  ArrowDownRight,
  Cpu,
  HardDrive,
  MemoryStick,
  Radio,
  Monitor,
  Apple,
  Globe,
  Calendar,
  Network,
  MousePointer2,
  RefreshCw,
} from 'lucide-react';
import { getDevice, deleteDevice, rebootDevice } from '../api/devices';
import { startSession } from '../api/sessions';
import type { Session } from '../api/sessions';
import { Gauge } from '../components/Gauge';
import { LineChart } from '../components/LineChart';
import { InfoRow } from '../components/InfoRow';
import { CopyButton } from '../components/CopyButton';
import { StatusBadge } from '../components/StatusBadge';
import { SessionView } from '../components/SessionView';
import { useMetricsStore } from '../stores/metrics';
import { useAuth } from '../auth/context';
import type { Device, Metrics } from '../types/device';

const osIcons: Record<string, typeof Monitor> = {
  linux: Server,
  windows: Monitor,
  darwin: Apple,
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
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

export function DeviceDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { accessToken } = useAuth();
  const [device, setDevice] = useState<Device | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [session, setSession] = useState<Session | null>(null);
  const [sessionOffer, setSessionOffer] = useState<string | null>(null);
  const [startingSession, setStartingSession] = useState(false);
  const [rebooting, setRebooting] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  const history = useMetricsStore((s) => (id ? s.history[id] : null));
  const addMetric = useMetricsStore((s) => s.addMetric);

  // Ensure id is always defined (it comes from URL params)
  const deviceId = id!;

  // Load device data
  useEffect(() => {
    if (!id) return;
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const data = await getDevice(deviceId);
        if (!cancelled) {
          setDevice(data);
        }
      } catch (err) {
        if (!cancelled) {
          setError((err as Error).message);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    load();

    // Poll for device status updates every 15s
    const interval = setInterval(async () => {
      try {
        const data = await getDevice(deviceId);
        setDevice(data);
      } catch {
        // ignore
      }
    }, 15000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [id]);

  // WebSocket connection for real-time metrics
  useEffect(() => {
    if (!id || !accessToken) return;

    const connect = () => {
      if (wsRef.current?.readyState === WebSocket.OPEN) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.host;
      const url = `${protocol}//${host}/ws?token=${encodeURIComponent(accessToken)}`;
      const ws = new WebSocket(url);
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
  }, [id, accessToken]);

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
    try {
      setRebooting(true);
      await rebootDevice(device.id);
    } catch {
      // ignore
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
      const { session, offer } = await startSession(device.id);
      setSession(session);
      setSessionOffer(offer);
    } catch (err) {
      setError(`Failed to start session: ${(err as Error).message}`);
    } finally {
      setStartingSession(false);
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Activity className="h-8 w-8 animate-spin text-accent" />
      </div>
    );
  }

  if (error || !device) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-4">
        <p className="text-status-error">{error || 'Device not found'}</p>
        <button
          onClick={() => navigate(-1)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-text-primary transition-colors hover:bg-accent-dark"
        >
          <ArrowLeft className="h-4 w-4" />
          Go Back
        </button>
      </div>
    );
  }

  const OsIcon = osIcons[device.os] || Monitor;
  const hasData = chartData.length > 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-4">
          <button
            onClick={() => navigate(-1)}
            className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-text-secondary transition-colors hover:bg-bg hover:text-text-primary"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </button>
          <div className="flex items-center gap-3">
            <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-bg-card border border-bg-border">
              <OsIcon className="h-6 w-6 text-accent" />
            </div>
            <div>
              <div className="flex items-center gap-3">
                <h1 className="text-2xl font-semibold text-text-primary">{device.name}</h1>
                <StatusBadge status={device.status} />
                {connected && hasData && (
                  <span className="flex items-center gap-1.5 rounded-full bg-status-online/10 px-2 py-0.5 text-xs text-status-online">
                    <span className="relative flex h-2 w-2">
                      <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-online opacity-75" />
                      <span className="relative inline-flex h-2 w-2 rounded-full bg-status-online" />
                    </span>
                    Streaming
                  </span>
                )}
              </div>
              <p className="text-sm text-text-secondary">
                {device.hostname} • {osLabel(device.os)} • {device.arch}
                {device.agent_version && ` • v${device.agent_version}`}
              </p>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleStartSession}
            disabled={startingSession}
            className="flex items-center gap-2 rounded-lg bg-accent px-3 py-2 text-sm text-text-primary transition-colors hover:bg-accent-dark disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <MousePointer2 className="h-4 w-4" />
            {startingSession ? 'Connecting...' : 'Remote Session'}
          </button>
          <button
            onClick={handleReboot}
            disabled={rebooting}
            className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg hover:text-text-primary disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${rebooting ? 'animate-spin' : ''}`} />
            Reboot
          </button>
          <button
            onClick={handleDelete}
            className="flex items-center gap-2 rounded-lg bg-status-error px-3 py-2 text-sm text-text-primary transition-colors hover:bg-status-error/80"
          >
            <Trash2 className="h-4 w-4" />
            Delete
          </button>
        </div>
      </div>

      {/* Remote Session View */}
      {session && sessionOffer && (
        <SessionView
          session={session}
          offer={sessionOffer}
          onClose={() => {
            setSession(null);
            setSessionOffer(null);
          }}
        />
      )}

      {/* Info Grid */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Network Info */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-3 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <Wifi className="h-4 w-4" />
            Network
          </h2>
          <InfoRow
            label="Public IP"
            icon={Globe}
            value={
              device.public_ip ? (
                <div className="flex items-center gap-2">
                  <span>{device.public_ip}</span>
                  <CopyButton text={device.public_ip} />
                </div>
              ) : (
                <span className="text-text-muted">—</span>
              )
            }
          />
          <InfoRow
            label="Private IP"
            icon={Network}
            value={
              device.private_ip ? (
                <div className="flex items-center gap-2">
                  <span>{device.private_ip}</span>
                  <CopyButton text={device.private_ip} />
                </div>
              ) : (
                <span className="text-text-muted">—</span>
              )
            }
          />
        </div>

        {/* System Info */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-3 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <Server className="h-4 w-4" />
            System
          </h2>
          <InfoRow
            label="Agent Version"
            icon={Activity}
            value={device.agent_version || '—'}
          />
          <InfoRow
            label="Architecture"
            value={device.arch}
          />
        </div>

        {/* Timestamps */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-3 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <Clock className="h-4 w-4" />
            Timestamps
          </h2>
          <InfoRow
            label="Last Seen"
            icon={Clock}
            value={new Date(device.last_seen).toLocaleString()}
          />
          <InfoRow
            label="Created"
            icon={Calendar}
            value={new Date(device.created_at).toLocaleDateString()}
          />
        </div>
      </div>

      {/* Live Metrics Gauges */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <h2 className="mb-6 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
          <Radio className="h-4 w-4" />
          Live Metrics
          {!hasData && <span className="text-text-muted">(waiting for data...)</span>}
        </h2>
        <div className="flex flex-wrap justify-around gap-6">
          <Gauge
            value={latestMetrics?.cpu ?? 0}
            label="CPU Usage"
            unit="%"
            size={140}
          />
          <Gauge
            value={latestMetrics?.ram ?? 0}
            label="Memory"
            unit="%"
            size={140}
          />
          <Gauge
            value={latestMetrics?.disk_usage ?? 0}
            label="Disk"
            unit="%"
            size={140}
          />
        </div>
      </div>

      {/* Additional Metrics */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <InfoCard
          icon={ArrowDownRight}
          label="Network In"
          value={hasData ? `${formatBytes(latestMetrics!.net_in)}/s` : '—'}
          sublabel="current rate"
        />
        <InfoCard
          icon={ArrowUpRight}
          label="Network Out"
          value={hasData ? `${formatBytes(latestMetrics!.net_out)}/s` : '—'}
          sublabel="current rate"
        />
        <InfoCard
          icon={Activity}
          label="Data Points"
          value={String(chartData.length)}
          sublabel="collected so far"
        />
        <InfoCard
          icon={Radio}
          label="Connection"
          value={connected ? 'Connected' : 'Connecting...'}
          sublabel={connected ? 'WebSocket active' : 'waiting...'}
        />
      </div>

      {/* Charts */}
      <div className="space-y-4">
        {/* CPU Chart */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-4 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <Cpu className="h-4 w-4" />
            CPU Usage Over Time
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'cpu', name: 'CPU %', color: '#3b82f6' }]}
            height={200}
            yDomain={[0, 100]}
          />
        </div>

        {/* RAM Chart */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-4 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <MemoryStick className="h-4 w-4" />
            Memory Usage Over Time
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'ram', name: 'RAM %', color: '#22c55e' }]}
            height={200}
            yDomain={[0, 100]}
          />
        </div>

        {/* Disk Chart */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-4 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <HardDrive className="h-4 w-4" />
            Disk Usage Over Time
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[{ key: 'disk', name: 'Disk %', color: '#eab308' }]}
            height={200}
            yDomain={[0, 100]}
          />
        </div>

        {/* Network Chart */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-4">
          <h2 className="mb-4 flex items-center gap-2 text-sm font-medium text-text-secondary uppercase tracking-wider">
            <Network className="h-4 w-4" />
            Network Traffic Over Time
          </h2>
          <LineChart
            data={chartData}
            xKey="time"
            yKeys={[
              { key: 'netIn', name: 'Download', color: '#3b82f6' },
              { key: 'netOut', name: 'Upload', color: '#a855f7' },
            ]}
            height={200}
            yLabel="bytes/s"
          />
        </div>
      </div>
    </div>
  );
}

function InfoCard({
  icon: Icon,
  label,
  value,
  sublabel,
}: {
  icon: typeof Clock;
  label: string;
  value: string;
  sublabel?: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-bg-border bg-bg-card p-4">
      <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-bg">
        <Icon className="h-5 w-5 text-text-secondary" />
      </div>
      <div>
        <p className="text-xs text-text-secondary">{label}</p>
        <p className="text-sm font-medium text-text-primary">{value}</p>
        {sublabel && <p className="text-[10px] text-text-muted">{sublabel}</p>}
      </div>
    </div>
  );
}
