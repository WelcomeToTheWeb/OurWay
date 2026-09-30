import { useEffect, useState, useCallback } from 'react';
import {
  Upload,
  Download,
  FolderUp,
  FolderDown,
  Search,
  Activity,
  Check,
  X,
  Clock,
  File,
  Loader2,
} from 'lucide-react';
import { listTransfers, pushFile, pullFile, uploadFile, downloadFile } from '../api/files';
import { getDevices } from '../api/devices';
import { useTranslation } from 'react-i18next';
import type { Device } from '../types/device';
import type { FileTransfer } from '../types/file';

function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '—';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

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

export function FileTransfer() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<Device[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<string>('');
  const [transfers, setTransfers] = useState<FileTransfer[]>([]);
  const [loading, setLoading] = useState(true);
  const [dragOver, setDragOver] = useState(false);
  const [search, setSearch] = useState('');

  // Push state
  const [pushing, setPushing] = useState(false);
  const [pushDestination, setPushDestination] = useState('');
  const [uploadedFile, setUploadedFile] = useState<{ id: string; name: string; size: number } | null>(null);

  // Pull state
  const [pulling, setPulling] = useState(false);
  const [pullPath, setPullPath] = useState('');

  // Download state
  const [downloadingId, setDownloadingId] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState('');

  const handleDownload = async (transfer: FileTransfer) => {
    setDownloadingId(transfer.id);
    setDownloadError('');
    try {
      await downloadFile(transfer.id, transfer.filename);
    } catch {
      setDownloadError(t('fileTransfer.failedDownload', { file: transfer.filename }));
    } finally {
      setDownloadingId(null);
    }
  };

  useEffect(() => {
    let cancelled = false;

    async function loadDevices() {
      try {
        const data = await getDevices();
        if (!cancelled) {
          setDevices(data);
          if (data.length > 0) setSelectedDevice(data[0].id);
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
    let cancelled = false;

    async function loadTransfers() {
      try {
        const data = await listTransfers();
        if (!cancelled) {
          setTransfers(data);
          setLoading(false);
        }
      } catch {
        if (!cancelled) setLoading(false);
      }
    }

    loadTransfers();
    const interval = setInterval(loadTransfers, 10000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const handleFileDrop = useCallback(async (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);

    const file = e.dataTransfer.files[0];
    if (!file) return;

    try {
      setPushing(true);
      const result = await uploadFile(file);
      setUploadedFile({ id: result.transfer_id, name: result.filename, size: result.size_bytes });

      if (selectedDevice) {
        await pushFile(result.transfer_id, [selectedDevice], pushDestination || undefined);
      }
    } catch {
      // ignore
    } finally {
      setPushing(false);
    }
  }, [selectedDevice, pushDestination]);

  const handleFileInput = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    try {
      setPushing(true);
      const result = await uploadFile(file);
      setUploadedFile({ id: result.transfer_id, name: result.filename, size: result.size_bytes });

      if (selectedDevice) {
        await pushFile(result.transfer_id, [selectedDevice], pushDestination || undefined);
      }
    } catch {
      // ignore
    } finally {
      setPushing(false);
    }
  }, [selectedDevice, pushDestination]);

  const handlePull = useCallback(async () => {
    if (!selectedDevice || !pullPath) return;
    try {
      setPulling(true);
      await pullFile(selectedDevice, pullPath);
      setPullPath('');
    } catch {
      // ignore
    } finally {
      setPulling(false);
    }
  }, [selectedDevice, pullPath]);

  const filtered = transfers.filter((t) => {
    if (search) {
      const q = search.toLowerCase();
      if (!t.filename.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('fileTransfer.title')}</h1>
          <p className="text-sm text-text-secondary">
            {t('fileTransfer.totalTransfers', { count: transfers.length })}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="flex items-center gap-1.5 rounded-full bg-status-online/10 px-2 py-0.5 text-xs text-status-online">
            <span className="h-2 w-2 rounded-full bg-status-online animate-pulse" />
            {t('fileTransfer.autoRefreshing')}
          </span>
        </div>
      </div>

      {/* Device selector */}
      <div className="flex items-center gap-4">
        <label className="text-sm text-text-secondary">{t('fileTransfer.device')}:</label>
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

      {/* Push / Pull tabs */}
      <div className="grid gap-6 md:grid-cols-2">
        {/* Push */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-5">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-accent/15 text-accent">
              <FolderUp className="h-5 w-5" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-text-primary">{t('fileTransfer.pushTitle')}</h3>
              <p className="text-xs text-text-secondary">{t('fileTransfer.pushDesc')}</p>
            </div>
          </div>

          <div
            className={`mt-4 rounded-lg border-2 border-dashed p-6 text-center transition-colors ${
              dragOver ? 'border-accent bg-accent/5' : 'border-bg-border'
            }`}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={handleFileDrop}
          >
            <Upload className="mx-auto h-8 w-8 text-text-secondary" />
            <p className="mt-2 text-sm text-text-primary">
              {t('fileTransfer.dropHere')}
            </p>
            <input
              type="file"
              onChange={handleFileInput}
              className="hidden"
              id="push-file"
            />
            <label
              htmlFor="push-file"
              className="mt-2 inline-block cursor-pointer rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-accent/80"
            >
              {t('fileTransfer.browse')}
            </label>
          </div>

          {uploadedFile && (
            <div className="mt-3 flex items-center gap-2 rounded-lg bg-bg p-3">
              <File className="h-4 w-4 text-accent" />
              <div className="flex-1">
                <p className="text-xs font-medium text-text-primary">{uploadedFile.name}</p>
                <p className="text-xs text-text-secondary">{formatSize(uploadedFile.size)}</p>
              </div>
            </div>
          )}

          <div className="mt-4">
            <label className="block text-xs text-text-secondary">
              {t('fileTransfer.destinationLabel')}
            </label>
            <input
              type="text"
              value={pushDestination}
              onChange={(e) => setPushDestination(e.target.value)}
              placeholder="/home/user/Desktop"
              className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
          </div>

          {pushing && (
            <div className="mt-3 flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin text-accent" />
              <span className="text-xs text-accent">{t('fileTransfer.uploading')}</span>
            </div>
          )}
        </div>

        {/* Pull */}
        <div className="rounded-xl border border-bg-border bg-bg-card p-5">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-status-online/15 text-status-online">
              <FolderDown className="h-5 w-5" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-text-primary">{t('fileTransfer.pullTitle')}</h3>
              <p className="text-xs text-text-secondary">{t('fileTransfer.pullDesc')}</p>
            </div>
          </div>

          <div className="mt-4 space-y-3">
            <div>
              <label className="block text-xs text-text-secondary">
                {t('fileTransfer.pullPathLabel')}
              </label>
              <input
                type="text"
                value={pullPath}
                onChange={(e) => setPullPath(e.target.value)}
                placeholder="/etc/hosts or C:\\Windows\\System32\\drivers\\etc\\hosts"
                className="mt-1 w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            </div>
            <button
              onClick={handlePull}
              disabled={pulling || !pullPath}
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-status-online px-4 py-2 text-sm font-medium text-text-primary transition-colors hover:bg-status-online/80 disabled:opacity-50"
            >
              {pulling ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Download className="h-4 w-4" />
              )}
              {t('fileTransfer.pullTitle')}
            </button>
          </div>
        </div>
      </div>

      {/* Transfer history */}
      <div>
        {downloadError && (
          <p className="mb-2 text-sm text-status-error">{downloadError}</p>
        )}
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-text-primary">{t('fileTransfer.history')}</h2>
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('fileTransfer.searchFiles')}
              className="w-64 rounded-lg border border-bg-border bg-bg py-2 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
          </div>
        </div>

        {loading ? (
          <div className="mt-4 flex h-32 items-center justify-center">
            <Activity className="h-8 w-8 animate-spin text-accent" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="mt-4 flex flex-col items-center justify-center rounded-xl border border-bg-border bg-bg-card py-12 text-text-secondary">
            <File className="h-10 w-10 text-text-muted" />
            <p className="mt-3 text-sm font-medium text-text-primary">
              {t('fileTransfer.noTransfers')}
            </p>
            <p className="mt-1 text-xs text-text-muted">
              {t('fileTransfer.noTransfersDesc')}
            </p>
          </div>
        ) : (
          <div className="mt-4 space-y-3">
            {filtered.map((tr) => {
              const isPush = tr.direction === 'push';
              const Icon = tr.status === 'completed' ? Check : tr.status === 'failed' ? X : Clock;
              const statusColor =
                tr.status === 'completed'
                  ? 'bg-status-online/15 text-status-online'
                  : tr.status === 'failed'
                    ? 'bg-status-error/15 text-status-error'
                    : 'bg-accent/15 text-accent';

              return (
                <div
                  key={tr.id}
                  className="flex items-center justify-between rounded-xl border border-bg-border bg-bg-card p-4 transition-colors hover:border-accent/50"
                >
                  <div className="flex items-center gap-4">
                    <div
                      className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${
                        isPush ? 'bg-accent/15 text-accent' : 'bg-status-online/15 text-status-online'
                      }`}
                    >
                      {isPush ? <Upload className="h-5 w-5" /> : <Download className="h-5 w-5" />}
                    </div>
                    <div className="flex-1">
                      <p className="text-sm font-medium text-text-primary">{tr.filename}</p>
                      <p className="text-xs text-text-secondary">
                        {tr.size_bytes > 0 ? formatSize(tr.size_bytes) : t('fileTransfer.unknownSize')}
                        {tr.destination && ` • ${t('fileTransfer.to')}: ${tr.destination}`}
                        {tr.source_path && ` • ${t('fileTransfer.from')}: ${tr.source_path}`}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    {tr.status !== 'completed' && tr.status !== 'failed' && (
                      <span className="flex items-center gap-1.5 text-xs text-accent">
                        <Activity className="h-3.5 w-3.5 animate-spin" />
                        {tr.progress}%
                      </span>
                    )}
                    {tr.status === 'failed' && tr.error_message && (
                      <span className="max-w-xs truncate text-xs text-status-error" title={tr.error_message}>
                        {tr.error_message}
                      </span>
                    )}
                    {tr.status === 'completed' && (
                      <button
                        onClick={() => handleDownload(tr)}
                        disabled={downloadingId === tr.id}
                        className="text-sm text-accent hover:text-accent-dark transition-colors disabled:opacity-50"
                        title={t('fileTransfer.downloadFile')}
                      >
                        {downloadingId === tr.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                      </button>
                    )}
                    <span
                      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase ${statusColor}`}
                    >
                      <Icon className="h-3 w-3" />
                      {tr.status}
                    </span>
                    <span className="text-xs text-text-muted">{timeAgo(tr.created_at)}</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
