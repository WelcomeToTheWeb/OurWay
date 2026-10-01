import { useCallback, useEffect, useState } from 'react';
import { Apple, Download, Loader2, Monitor, Plus, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { downloadInstaller, fetchInstallers, type Installer } from '../api/installers';

// osIcons mirrors the OS icon convention used by DeviceCard.
const osIcons: Record<string, typeof Monitor> = {
  linux: Server,
  windows: Monitor,
  darwin: Apple,
};

// osName resolves the human-facing OS name from the API's `os` field,
// falling back to recognizable markers in the file name.
function osName(os: string, name: string): 'Windows' | 'Linux' | 'macOS' | null {
  const o = os.toLowerCase();
  const n = name.toLowerCase();
  if (o === 'windows' || n.includes('windows')) return 'Windows';
  if (o === 'linux' || n.includes('linux')) return 'Linux';
  if (o === 'darwin' || o === 'macos' || o === 'osx' || /darwin|macos|osx/.test(n)) return 'macOS';
  return null;
}

function archLabel(arch: string): string {
  const a = arch.toLowerCase();
  if (a.includes('arm')) return 'ARM64';
  if (a === 'amd64' || a === 'x86_64' || a.endsWith('64')) return 'x64';
  return arch;
}

// installerLabel derives a button label from the API's os/arch fields
// (never hardcoded per file): Windows binaries get an arch suffix
// ("Windows x64", "Windows ARM64"); script-based installers get a
// "script" suffix ("Linux (script)", "macOS (script)"). Unknown
// os/arch values fall back to a raw "os arch" pair, then the file name.
function installerLabel(installer: Installer, script: string): string {
  const os = osName(installer.os, installer.name);
  if (os === 'Windows') return `Windows ${archLabel(installer.arch)}`;
  if (os === 'macOS') return `macOS (${script})`;
  if (os === 'Linux') return `Linux (${script})`;
  if (installer.os && installer.arch) return `${installer.os} ${installer.arch}`;
  return installer.os || installer.name;
}

export function InstallerPanel() {
  const { t } = useTranslation();
  const [installers, setInstallers] = useState<Installer[] | null>(null);
  const [error, setError] = useState('');
  const [downloading, setDownloading] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError('');
    setInstallers(null);
    try {
      setInstallers(await fetchInstallers());
    } catch {
      setError(t('devices.installerError'));
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleDownload = async (installer: Installer) => {
    setDownloading(installer.url);
    try {
      await downloadInstaller(installer);
    } catch {
      setError(t('devices.installerDownloadFailed', { file: installer.name }));
    } finally {
      setDownloading(null);
    }
  };

  return (
    <div className="rounded-xl border border-bg-border bg-bg-card p-5">
      <div className="flex items-center gap-2">
        <Plus className="h-4 w-4 text-accent" />
        <h2 className="text-sm font-semibold text-text-primary">{t('devices.addDevice')}</h2>
      </div>
      <p className="mt-1 text-sm text-text-secondary">{t('devices.installerDesc')}</p>

      {error && (
        <div className="mt-3 flex items-center gap-3">
          <p className="text-sm text-status-error">{error}</p>
          <button
            onClick={() => void load()}
            className="rounded-lg bg-bg-secondary px-3 py-1.5 text-xs text-text-primary transition-colors hover:bg-bg"
          >
            {t('common.refresh')}
          </button>
        </div>
      )}

      {!error && installers === null ? (
        <div className="mt-3 flex items-center gap-2 text-sm text-text-secondary">
          <Loader2 className="h-4 w-4 animate-spin text-accent" />
          {t('common.loading')}
        </div>
      ) : !error && installers && installers.length === 0 ? (
        <p className="mt-3 text-sm text-text-muted">{t('devices.installerNone')}</p>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2">
          {installers!.map((installer) => {
            const Icon = osIcons[installer.os.toLowerCase()] || Monitor;
            const isDownloading = downloading === installer.url;
            return (
              <button
                key={installer.url}
                onClick={() => void handleDownload(installer)}
                disabled={isDownloading}
                title={installer.name}
                className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg disabled:opacity-50"
              >
                {isDownloading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <>
                    <Icon className="h-4 w-4" />
                    <Download className="h-4 w-4" />
                  </>
                )}
                {installerLabel(installer, t('devices.installerScript'))}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
