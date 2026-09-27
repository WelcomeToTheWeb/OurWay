import { useEffect, useState } from 'react';
import {
  Search,
  RefreshCw,
  Check,
  X,
  Clock,
  Download,
  Package,
  Activity,
  Play,
} from 'lucide-react';
import {
  getDeviceUpdates,
  scanDeviceForUpdates,
  deployNow,
  approveUpdate,
} from '../api/patching';
import { getDevices } from '../api/devices';
import type { Device } from '../types/device';
import type { SoftwareUpdate } from '../types/patch';

const statusConfig: Record<
  SoftwareUpdate['status'],
  { label: string; className: string; icon: typeof Check }
> = {
  detected: {
    label: 'Detected',
    className: 'bg-status-warning/15 text-status-warning ring-1 ring-inset ring-status-warning/30',
    icon: Clock,
  },
  approved: {
    label: 'Approved',
    className: 'bg-accent/15 text-accent ring-1 ring-inset ring-accent/30',
    icon: Check,
  },
  downloading: {
    label: 'Downloading',
    className: 'bg-accent/15 text-accent ring-1 ring-inset ring-accent/30',
    icon: Download,
  },
  installing: {
    label: 'Installing',
    className: 'bg-accent/15 text-accent ring-1 ring-inset ring-accent/30',
    icon: Activity,
  },
  installed: {
    label: 'Installed',
    className: 'bg-status-online/15 text-status-online ring-1 ring-inset ring-status-online/30',
    icon: Check,
  },
  failed: {
    label: 'Failed',
    className: 'bg-status-error/15 text-status-error ring-1 ring-inset ring-status-error/30',
    icon: X,
  },
  skipped: {
    label: 'Skipped',
    className: 'bg-bg text-text-secondary ring-1 ring-inset ring-bg-border',
    icon: Clock,
  },
};

function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '—';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export function Patches() {
  const [devices, setDevices] = useState<Device[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<string>('');
  const [updates, setUpdates] = useState<SoftwareUpdate[]>([]);
  const [loading, setLoading] = useState(true);
  const [scanning, setScanning] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  useEffect(() => {
    let cancelled = false;

    async function loadDevices() {
      try {
        const data = await getDevices();
        if (!cancelled) {
          setDevices(data);
          if (data.length > 0) {
            setSelectedDevice(data[0].id);
          }
        }
      } catch {
        // ignore
      }
    }

    loadDevices();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!selectedDevice) return;

    let cancelled = false;

    async function loadUpdates() {
      try {
        setLoading(true);
        const data = await getDeviceUpdates(selectedDevice);
        if (!cancelled) setUpdates(data);
      } catch {
        // ignore
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    loadUpdates();
    const interval = setInterval(loadUpdates, 30000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [selectedDevice]);

  async function handleScan() {
    if (!selectedDevice) return;
    try {
      setScanning(true);
      await scanDeviceForUpdates(selectedDevice);
    } catch {
      // ignore
    } finally {
      setScanning(false);
    }
  }

  async function handleDeploy() {
    if (!selectedDevice) return;
    try {
      setDeploying(true);
      await deployNow([selectedDevice]);
    } catch {
      // ignore
    } finally {
      setDeploying(false);
    }
  }

  async function handleApprove(updateId: string) {
    if (!selectedDevice) return;
    try {
      await approveUpdate(updateId);
      const data = await getDeviceUpdates(selectedDevice);
      setUpdates(data);
    } catch {
      // ignore
    }
  }

  const filtered = updates.filter((u) => {
    if (statusFilter !== 'all' && u.status !== statusFilter) return false;
    if (search) {
      const q = search.toLowerCase();
      if (!u.title.toLowerCase().includes(q) && !u.source.toLowerCase().includes(q)) {
        return false;
      }
    }
    return true;
  });

  const detectedCount = updates.filter((u) => u.status === 'detected').length;
  const failedCount = updates.filter((u) => u.status === 'failed').length;

  const selectedDeviceName = devices.find((d) => d.id === selectedDevice)?.hostname || '';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Patch Management</h1>
          <p className="text-sm text-text-secondary">
            {detectedCount} updates detected • {failedCount} failed
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleScan}
            disabled={scanning || !selectedDevice}
            className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-bg disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${scanning ? 'animate-spin' : ''}`} />
            Scan
          </button>
          <button
            onClick={handleDeploy}
            disabled={deploying || !selectedDevice}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-accent/80 disabled:opacity-50"
          >
            <Play className={`h-3.5 w-3.5 ${deploying ? 'animate-spin' : ''}`} />
            Deploy Now
          </button>
        </div>
      </div>

      {/* Device selector */}
      <div className="flex items-center gap-4">
        <label className="text-sm text-text-secondary">Device:</label>
        <select
          value={selectedDevice}
          onChange={(e) => setSelectedDevice(e.target.value)}
          className="rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
        >
          {devices.map((d) => (
            <option key={d.id} value={d.id}>
              {d.hostname}
            </option>
          ))}
        </select>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-4">
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search updates..."
            className="w-full rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary"
        >
          <option value="all">All statuses</option>
          <option value="detected">Detected</option>
          <option value="approved">Approved</option>
          <option value="downloading">Downloading</option>
          <option value="installing">Installing</option>
          <option value="installed">Installed</option>
          <option value="failed">Failed</option>
          <option value="skipped">Skipped</option>
        </select>
      </div>

      {/* Updates list */}
      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <Activity className="h-8 w-8 animate-spin text-accent" />
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-16 text-text-secondary">
          <div className="h-20 w-20 rounded-full bg-bg border border-bg-border flex items-center justify-center">
            <Package className="h-8 w-8 text-text-muted" />
          </div>
          <p className="mt-4 text-sm font-medium text-text-primary">
            {selectedDeviceName ? 'No updates found' : 'Select a device to view updates'}
          </p>
          <p className="mt-1 text-xs text-text-muted">
            {selectedDeviceName
              ? 'All systems appear to be up to date'
              : 'Choose a device from the dropdown above'}
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {filtered.map((update) => {
            const cfg = statusConfig[update.status];
            const Icon = cfg.icon;
            return (
              <div
                key={update.id}
                className="flex items-center justify-between rounded-xl border border-bg-border bg-bg-card p-4 transition-colors hover:border-accent/50"
              >
                <div className="flex items-center gap-4">
                  <div
                    className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${cfg.className}`}
                  >
                    <Icon className="h-5 w-5" />
                  </div>
                  <div className="flex-1">
                    <p className="text-sm font-medium text-text-primary">{update.title}</p>
                    <div className="mt-0.5 flex items-center gap-3 text-xs text-text-secondary">
                      <span>{update.source}</span>
                      {update.version && <span>v{update.version}</span>}
                      <span>{formatSize(update.size_bytes)}</span>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  {update.status === 'detected' && (
                    <button
                      onClick={() => handleApprove(update.id)}
                      className="flex items-center gap-1.5 rounded-lg bg-accent px-2.5 py-1 text-xs font-medium text-text-primary transition-colors hover:bg-accent/80"
                    >
                      <Check className="h-3.5 w-3.5" />
                      Approve
                    </button>
                  )}
                  {update.status === 'installing' && (
                    <span className="flex items-center gap-1.5 text-xs text-accent">
                      <Activity className="h-3.5 w-3.5 animate-spin" />
                      Installing...
                    </span>
                  )}
                  {update.status === 'failed' && update.error_message && (
                    <span className="max-w-xs truncate text-xs text-status-error" title={update.error_message}>
                      {update.error_message}
                    </span>
                  )}
                  <span
                    className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase ${cfg.className}`}
                  >
                    {cfg.label}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
