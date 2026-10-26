import { useEffect, useState } from 'react';
import {
  Search,
  RefreshCw,
  Check,
  Package,
  Play,
} from 'lucide-react';
import {
  getDeviceUpdates,
  scanDeviceForUpdates,
  deployNow,
  approveUpdate,
} from '../api/patching';
import { getDevices } from '../api/devices';
import { useTranslation } from 'react-i18next';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import type { Device } from '../types/device';
import type { SoftwareUpdate } from '../types/patch';
import { isRateLimited } from '../api/client';


function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '—';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export function Patches() {
  const { t } = useTranslation();
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
    const interval = setInterval(() => {
      if (!isRateLimited()) void loadUpdates();
    }, 30000);

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
  const guide = useGuideWords(filtered.length);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('patches.managementTitle')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {t('patches.subtitle', { detected: detectedCount, failed: failedCount })}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-2">
            <span className="cap cap-sm text-text-muted">{t('patches.device')}</span>
            <select
              value={selectedDevice}
              onChange={(e) => setSelectedDevice(e.target.value)}
              className="select"
              aria-label={t('patches.device')}
            >
              <option value="">—</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name} · {d.hostname}
                </option>
              ))}
            </select>
          </label>
          <button onClick={handleScan} disabled={scanning || !selectedDevice} className="btn">
            <RefreshCw className={`h-3.5 w-3.5 ${scanning ? 'animate-spin' : ''}`} />
            {t('patches.scanShort')}
          </button>
          <button onClick={handleDeploy} disabled={deploying || !selectedDevice} className="btn-primary">
            <Play className={`h-3.5 w-3.5 ${deploying ? 'animate-spin' : ''}`} />
            {t('patches.deployNow')}
          </button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="relative block min-w-[16rem] flex-1">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" aria-hidden="true" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('patches.searchUpdates')}
            className="input w-full pl-8"
          />
        </label>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="select"
          aria-label="Filter by status"
        >
          <option value="all">{t('patches.allStatuses')}</option>
          <option value="detected">{t('patches.statuses.detected')}</option>
          <option value="approved">{t('patches.statuses.approved')}</option>
          <option value="downloading">{t('patches.statuses.downloading')}</option>
          <option value="installing">{t('patches.statuses.installing')}</option>
          <option value="installed">{t('patches.statuses.installed')}</option>
          <option value="failed">{t('patches.statuses.failed')}</option>
          <option value="skipped">{t('patches.statuses.skipped')}</option>
        </select>
      </div>

      {loading ? (
        <div className="flex h-40 items-center justify-center">
          <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        </div>
      ) : filtered.length === 0 ? (
        <div className="panel px-4 py-8 text-center">
          <Package className="mx-auto h-5 w-5 text-text-muted" aria-hidden="true" />
          <p className="cap cap-sm mt-2 text-text-secondary">
            {selectedDeviceName ? t('patches.noUpdates') : t('patches.selectDevice')}
          </p>
          <p className="mt-1 text-sm text-text-muted">
            {selectedDeviceName ? t('patches.upToDate') : t('patches.chooseDevice')}
          </p>
        </div>
      ) : (
        <section className="panel overflow-hidden">
          <GuideWordsHead
            total={filtered.length}
            unit={t('sheet.inView')}
            first={filtered[guide.first]?.title}
            last={filtered[guide.last]?.title}
          />
          <div ref={guide.ref} className="max-h-[32rem] overflow-y-auto">
            <table className="ledger">
              <thead>
                <tr>
                  <th>{t('patches.managementTitle')}</th>
                  <th className="w-28">Severity</th>
                  <th className="w-24">Size</th>
                  <th className="w-28">State</th>
                  <th className="w-32 text-right"> </th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((update) => (
                  <tr key={update.id} data-guide title={update.title}>
                    <td>
                      <span className="cap cap-sm text-text-primary">{update.title}</span>
                      <span className="cap cap-sm block truncate text-text-muted">
                        {update.source}
                        {update.version && ` · v${update.version}`}
                        {update.kb && ` · ${update.kb}`}
                      </span>
                      {update.status === 'failed' && update.error_message && (
                        <span className="cap cap-sm block truncate text-[var(--error)]" title={update.error_message}>
                          {update.error_message}
                        </span>
                      )}
                    </td>
                    <td>
                      {update.severity ? (
                        <Stamp
                          tone={
                            update.severity === 'critical'
                              ? 'alert'
                              : update.severity === 'important'
                                ? 'due'
                                : 'neutral'
                          }
                        >
                          {update.severity}
                        </Stamp>
                      ) : (
                        <span className="cap cap-sm text-text-muted">—</span>
                      )}
                    </td>
                    <td>
                      <span className="ow-num text-[12px]">{formatSize(update.size_bytes)}</span>
                    </td>
                    <td>
                      <span className="inline-flex items-center gap-1.5">
                        <Stamp
                          tone={
                            update.status === 'installed'
                              ? 'online'
                              : update.status === 'failed'
                                ? 'alert'
                                : update.status === 'approved'
                                  ? 'service'
                                  : update.status === 'detected'
                                    ? 'due'
                                    : 'neutral'
                          }
                        >
                          {t(`patches.statuses.${update.status}`)}
                        </Stamp>
                        {update.status === 'installing' && (
                          <RefreshCw className="h-3 w-3 animate-spin text-[var(--service)]" aria-hidden="true" />
                        )}
                      </span>
                    </td>
                    <td className="text-right">
                      {update.status === 'detected' && (
                        <button onClick={() => handleApprove(update.id)} className="btn-primary">
                          <Check className="h-3.5 w-3.5" />
                          {t('patches.approve')}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
