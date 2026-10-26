import { useEffect, useState, useCallback } from 'react';
import {
  Upload,
  Download,
  FolderUp,
  FolderDown,
  Search,
  File,
  Loader2,
  RefreshCw,
} from 'lucide-react';
import { listTransfers, pushFile, pullFile, uploadFile, downloadFile } from '../api/files';
import { getDevices } from '../api/devices';
import { useTranslation } from 'react-i18next';
import type { Device } from '../types/device';
import type { FileTransfer } from '../types/file';
import { isRateLimited } from '../api/client';
import { Stamp } from '../components/Stamp';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';

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
    return d.toISOString().slice(0, 10);
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
    const interval = setInterval(() => {
      if (!isRateLimited()) void loadTransfers();
    }, 10000);
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

  const guide = useGuideWords(transfers.length);

  const filtered = transfers.filter((t) => {
    if (search) {
      const q = search.toLowerCase();
      if (!t.filename.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('fileTransfer.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {t('fileTransfer.totalTransfers', { count: transfers.length })} ·{' '}
            {isRateLimited() ? 'polling rests — rate limit' : 'polling every 10 s'}
          </p>
        </div>
        <label className="flex items-center gap-2">
          <span className="cap cap-sm text-text-muted">{t('fileTransfer.device')}</span>
          <select
            value={selectedDevice}
            onChange={(e) => setSelectedDevice(e.target.value)}
            className="select"
          >
            <option value="">—</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name} · {d.hostname}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        {/* Push */}
        <section className="panel p-4">
          <h2 className="panel-head">
            <FolderUp className="h-3.5 w-3.5" aria-hidden="true" /> {t('fileTransfer.pushTitle')}
          </h2>
          <p className="cap cap-sm mt-1 text-text-muted">{t('fileTransfer.pushDesc')}</p>

          <div
            className={`mt-3 border bg-[var(--field)] px-4 py-6 text-center transition-colors ${
              dragOver ? 'border-[var(--service)] bg-bg-secondary' : 'border-rule'
            }`}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={handleFileDrop}
          >
            <Upload className="mx-auto h-5 w-5 text-text-secondary" aria-hidden="true" />
            <p className="cap cap-sm mt-2 text-text-primary">{t('fileTransfer.dropHere')}</p>
            <input type="file" onChange={handleFileInput} className="hidden" id="push-file" />
            <label htmlFor="push-file" className="btn btn-primary mt-2 inline-block">
              {t('fileTransfer.browse')}
            </label>
          </div>

          {uploadedFile && (
            <div className="mt-3 flex items-center justify-between gap-3 border border-rule bg-[var(--field)] px-3 py-2">
              <span className="cap cap-sm truncate text-text-primary">{uploadedFile.name}</span>
              <span className="ow-num text-[12px] text-text-secondary">{formatSize(uploadedFile.size)}</span>
            </div>
          )}

          <label className="mt-3 block">
            <span className="field-label">{t('fileTransfer.destinationLabel')}</span>
            <input
              type="text"
              value={pushDestination}
              onChange={(e) => setPushDestination(e.target.value)}
              placeholder="/home/user/Desktop"
              className="input mt-1 w-full"
            />
          </label>

          {pushing && (
            <p className="cap cap-sm mt-2 inline-flex items-center gap-2 text-[var(--service)]">
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" /> {t('fileTransfer.uploading')}
            </p>
          )}
        </section>

        {/* Pull */}
        <section className="panel p-4">
          <h2 className="panel-head">
            <FolderDown className="h-3.5 w-3.5" aria-hidden="true" /> {t('fileTransfer.pullTitle')}
          </h2>
          <p className="cap cap-sm mt-1 text-text-muted">{t('fileTransfer.pullDesc')}</p>

          <label className="mt-3 block">
            <span className="field-label">{t('fileTransfer.pullPathLabel')}</span>
            <input
              type="text"
              value={pullPath}
              onChange={(e) => setPullPath(e.target.value)}
              placeholder="/etc/hosts"
              className="input mt-1 w-full"
            />
          </label>

          <button
            onClick={handlePull}
            disabled={pulling || !pullPath || !selectedDevice}
            className="btn-primary mt-3 w-full justify-center"
          >
            {pulling ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            {t('fileTransfer.pullTitle')}
          </button>
          {!selectedDevice && (
            <p className="cap cap-sm mt-2 text-text-muted">Pick a unit first — a pull needs a machine to pull from.</p>
          )}
        </section>
      </div>

      {/* Transfer ledger */}
      <section className="panel overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
          <h2 className="panel-head border-0 bg-transparent p-0">
            <File className="h-3.5 w-3.5" aria-hidden="true" /> {t('fileTransfer.history')}
          </h2>
          <label className="relative block">
            <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" aria-hidden="true" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('fileTransfer.searchFiles')}
              className="input w-64 pl-8"
            />
          </label>
        </div>
        <GuideWordsHead
          total={filtered.length}
          unit={t('sheet.inView')}
          first={filtered[guide.first]?.filename}
          last={filtered[guide.last]?.filename}
        />

        {downloadError && <p className="cap cap-sm px-3 text-[var(--error)]">{downloadError}</p>}

        {loading ? (
          <div className="flex h-32 items-center justify-center">
            <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="px-4 py-8 text-center">
            <p className="cap cap-sm text-text-secondary">{t('fileTransfer.noTransfers')}</p>
            <p className="mt-1 text-sm text-text-muted">{t('fileTransfer.noTransfersDesc')}</p>
          </div>
        ) : (
          <div ref={guide.ref} className="max-h-[28rem] overflow-y-auto">
            <table className="ledger">
              <thead>
                <tr>
                  <th>{t('fileTransfer.history')}</th>
                  <th className="w-16">Dir</th>
                  <th className="w-24">Size</th>
                  <th className="w-40">Path</th>
                  <th className="w-24">State</th>
                  <th className="w-20">Stated</th>
                  <th className="w-14 text-right"> </th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((tr) => {
                  const isPush = tr.direction === 'push';
                  return (
                    <tr key={tr.id} data-guide title={tr.filename}>
                      <td>
                        <span className="cap cap-sm text-text-primary">{tr.filename}</span>
                        {tr.status === 'failed' && tr.error_message && (
                          <span className="cap cap-sm block truncate text-[var(--error)]" title={tr.error_message}>
                            {tr.error_message}
                          </span>
                        )}
                      </td>
                      <td>
                        <Stamp tone={isPush ? 'service' : 'neutral'}>{isPush ? 'push' : 'pull'}</Stamp>
                      </td>
                      <td>
                        <span className="ow-num text-[12px]">
                          {tr.size_bytes > 0 ? formatSize(tr.size_bytes) : t('fileTransfer.unknownSize')}
                        </span>
                      </td>
                      <td>
                        <span
                          className="ow-num block truncate text-[11.5px] text-text-secondary"
                          title={tr.destination || tr.source_path || ''}
                        >
                          {tr.destination || tr.source_path || '—'}
                        </span>
                      </td>
                      <td>
                        <span className="inline-flex items-center gap-1.5">
                          <Stamp
                            tone={
                              tr.status === 'completed'
                                ? 'online'
                                : tr.status === 'failed'
                                  ? 'alert'
                                  : 'due'
                            }
                          >
                            {tr.status}
                          </Stamp>
                          {tr.status !== 'completed' && tr.status !== 'failed' && (
                            <span className="ow-num text-[11px] text-text-secondary">{tr.progress}%</span>
                          )}
                        </span>
                      </td>
                      <td>
                        <span className="ow-num text-[11.5px] text-text-secondary">{timeAgo(tr.created_at)}</span>
                      </td>
                      <td className="text-right">
                        {tr.status === 'completed' && (
                          <button
                            onClick={() => handleDownload(tr)}
                            disabled={downloadingId === tr.id}
                            className="btn-quiet btn"
                            title={t('fileTransfer.downloadFile')}
                          >
                            {downloadingId === tr.id ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <Download className="h-3.5 w-3.5" />
                            )}
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
