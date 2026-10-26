import { useCallback, useEffect, useState } from 'react';
import { Apple, Download, Loader2, Monitor, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CopyButton } from './CopyButton';
import {
  downloadInstaller,
  fetchInstallers,
  powershellInstallCommand,
  serverOrigin,
  shellInstallCommand,
  type Installer,
} from '../api/installers';

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

function platformLabel(installer: Installer): string {
  if (installer.kind === 'viewer') return `${osName(installer.os, installer.name) ?? ''} Viewer`.trim();
  const os = osName(installer.os, installer.name);
  if (os && installer.arch) return `${os} ${archLabel(installer.arch)}`;
  if (os) return os;
  return installer.name;
}

interface Command {
  id: string;
  icon: typeof Monitor;
  label: string;
  command: string;
}

function buildCommands(origin: string, installers: Installer[]): Command[] {
  const commands: Command[] = [];
  const has = (os: string) => installers.some((i) => osName(i.os, i.name) === os);
  if (has('Linux') || has('macOS')) {
    commands.push({ id: 'shell', icon: Server, label: 'Linux / macOS', command: shellInstallCommand(origin) });
  }
  if (has('Windows')) {
    commands.push({ id: 'powershell', icon: Monitor, label: 'Windows (PowerShell)', command: powershellInstallCommand(origin) });
  }
  return commands;
}

/** Enrol a unit: the one command, or the binary for the machine type. */
export function InstallerPanel() {
  const { t } = useTranslation();
  const [installers, setInstallers] = useState<Installer[] | null>(null);
  const [error, setError] = useState('');
  const [downloading, setDownloading] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const origin = serverOrigin();

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

  const commands = installers ? buildCommands(origin, installers) : [];

  return (
    <section className="panel overflow-hidden" aria-label={t('devices.addDevice')}>
      <div className="panel-head">
        <div className="flex items-baseline gap-3">
          <h2 className="cap cap-lg text-text-primary">{t('devices.addDevice')}</h2>
          <p className="hidden text-[12px] text-text-secondary md:block">{t('devices.installerDesc')}</p>
        </div>
        <button
          onClick={() => setOpen((o) => !o)}
          className="cap-sm cap text-[var(--service)] hover:underline"
          aria-expanded={open}
        >
          {open ? 'Hide' : 'Show'} install
        </button>
      </div>

      {open && (
        <div className="space-y-4 px-3 py-3">
          {error && (
            <div className="flex items-center gap-3">
              <p className="text-[13px] text-[var(--error)]">{error}</p>
              <button onClick={() => void load()} className="btn px-2 py-1 text-[11px]">
                {t('common.refresh')}
              </button>
            </div>
          )}

          {!error && installers === null ? (
            <div className="flex items-center gap-2 text-[13px] text-text-secondary">
              <Loader2 className="h-4 w-4 animate-spin text-[var(--service)]" aria-hidden="true" />
              {t('common.loading')}
            </div>
          ) : !error && installers && installers.length === 0 ? (
            <p className="text-[13px] text-text-muted">{t('devices.installerNone')}</p>
          ) : (
            <>
              {commands.length > 0 && (
                <div className="space-y-2">
                  <p className="field-label">{t('devices.installerOneCommand')}</p>
                  {commands.map((cmd) => {
                    const CmdIcon = cmd.icon;
                    return (
                      <div key={cmd.id} className="field flex items-center gap-2">
                        <CmdIcon className="h-4 w-4 shrink-0 text-[var(--service)]" aria-hidden="true" />
                        <span className="min-w-0 flex-1">
                          <span className="cap-sm cap block text-text-muted">{cmd.label}</span>
                          <code className="block truncate font-mono text-[11.5px] text-text-primary">
                            {cmd.command}
                          </code>
                        </span>
                        <CopyButton text={cmd.command} className="shrink-0" />
                      </div>
                    );
                  })}
                </div>
              )}

              <div>
                <p className="field-label mb-2">{t('devices.installerManual')}</p>
                <div className="flex flex-wrap gap-2">
                  {installers!.filter((i) => i.kind !== 'remote').map((installer) => {
                    const Icon = osIcons[installer.os.toLowerCase()] || Monitor;
                    const isDownloading = downloading === installer.url;
                    return (
                      <button
                        key={installer.url}
                        onClick={() => void handleDownload(installer)}
                        disabled={isDownloading}
                        title={installer.name}
                        className="btn px-2.5 py-1.5 text-[12px]"
                      >
                        {isDownloading ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                        ) : (
                          <>
                            <Icon className="h-3.5 w-3.5" aria-hidden="true" />
                            <Download className="h-3.5 w-3.5" aria-hidden="true" />
                          </>
                        )}
                        {platformLabel(installer)}
                      </button>
                    );
                  })}
                </div>
              </div>
            </>
          )}
        </div>
      )}
    </section>
  );
}
