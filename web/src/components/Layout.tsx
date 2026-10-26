import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Outlet, useNavigate } from 'react-router-dom';
import { LogOut, Menu, Pause, Search, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../auth/context';
import { Sidebar } from './Sidebar';
import { LinkStamp, Stamp } from './Stamp';
import { useGlobalShortcuts } from '../hooks/useKeyboardShortcuts';
import { useWebSocket } from '../hooks/useWebSocket';
import { listPolicies } from '../api/patching';
import { getServerVersion, type ServerVersion } from '../api/system';
import type { PatchPolicy } from '../types/patch';
import { nextWindow, untilLabel } from '../utils/nextWindow';
import { formatWindow } from '../utils/patchWindow';
import { useRateLimit } from '../hooks/useRateLimit';

/**
 * The shift bar: what a technician on shift needs before they read a single
 * figure — is the link live, who is on the console, what time is it in the
 * shop's reference zone, and when the next maintenance window opens.
 */
export function Layout() {
  const { t } = useTranslation();
  const { user, logout, accessToken, roles } = useAuth();
  const { connected } = useWebSocket(accessToken);
  const navigate = useNavigate();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [clock, setClock] = useState(() => new Date());
  const [policies, setPolicies] = useState<PatchPolicy[]>([]);
  const [serverVersion, setServerVersion] = useState<ServerVersion | null>(null);
  // Telling the technician that polling is resting belongs in the shift bar:
  // it explains why figures stop moving, before they go hunting for a bug.
  const { limited, secondsLeft } = useRateLimit();
  useGlobalShortcuts();

  useEffect(() => {
    const id = setInterval(() => setClock(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    let cancelled = false;
    listPolicies()
      .then((p) => !cancelled && setPolicies(p))
      .catch(() => !cancelled && setPolicies([]));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    getServerVersion()
      .then((v) => !cancelled && setServerVersion(v))
      .catch(() => !cancelled && setServerVersion(null));
    return () => {
      cancelled = true;
    };
  }, []);

  const upcoming = useMemo(() => nextWindow(policies, clock), [policies, clock]);

  const utc = clock.toISOString().slice(11, 19);
  // "dev" is the unstamped local build: docs/versioning.md.
  const consoleVersion = import.meta.env.VITE_APP_VERSION || 'dev';

  function submitSearch(e: FormEvent) {
    e.preventDefault();
    const q = query.trim();
    navigate(q ? `/devices?q=${encodeURIComponent(q)}` : '/devices');
  }

  return (
    <div className="flex h-screen overflow-hidden">
      <a href="#main-content" className="skip-link">
        Skip to main content
      </a>

      {sidebarOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 lg:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <div
        className={`fixed inset-y-0 left-0 z-40 w-60 transform transition-transform duration-400 ease-[var(--ease-press)] lg:static lg:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'
        }`}
      >
        <Sidebar />
      </div>

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-16 shrink-0 items-center gap-3 border-b border-rule bg-bg-secondary px-3 md:px-5">
          <button
            className="rounded p-2 text-text-secondary lg:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="Open navigation menu"
          >
            <Menu className="h-5 w-5" />
          </button>

          <form onSubmit={submitSearch} className="relative w-full max-w-[22rem]" role="search">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" aria-hidden="true" />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search every unit — name, host, ip, tag"
              aria-label="Search all devices"
              className="input w-full pl-9"
            />
          </form>

          <div className="ml-auto flex items-center gap-3 md:gap-5">
            <span className="hidden md:flex items-center gap-2">
              <span className="cap-sm cap text-text-muted">Next window</span>
              {upcoming ? (
                <Stamp tone="due" title={`${upcoming.policy.name} · ${formatWindow(upcoming.policy)}`}>
                  {formatWindow(upcoming.policy)} · {untilLabel(upcoming.at, clock)}
                </Stamp>
              ) : (
                <Stamp tone="neutral" title="No policy maintenance window is scheduled">
                  Unscheduled
                </Stamp>
              )}
            </span>

            <span className="hidden sm:flex items-baseline gap-2">
              <span className="cap-sm cap text-text-muted">UTC</span>
              <time className="ow-num text-[15px] font-semibold text-text-primary" dateTime={clock.toISOString()}>
                {utc}
              </time>
            </span>

            <span
              className="hidden lg:flex items-baseline gap-2"
              title={t('layout.versionTitle', {
                server: serverVersion?.version || 'dev',
                commit: serverVersion?.git_commit || 'dev',
                console: consoleVersion,
              })}
            >
              <span className="cap-sm cap text-text-muted">srv</span>
              <span className="ow-num text-[13px] text-text-primary">{serverVersion?.version || 'dev'}</span>
            </span>

            {limited && (
              <Stamp tone="due" title="Server rate limit reached — polling rests until the minute budget refills">
                <span className="inline-flex items-center gap-1.5">
                  <Pause className="h-3 w-3" aria-hidden="true" />
                  Limited {secondsLeft}s
                </span>
              </Stamp>
            )}

            <span className="hidden lg:flex">
              <LinkStamp connected={connected} />
            </span>

            {user && (
              <span className="hidden flex-col items-end leading-tight sm:flex">
                <span className="text-[13px] font-semibold text-text-primary">{user.username}</span>
                <span className="cap-sm cap text-text-muted">{(roles ?? ['viewer'])[0]}</span>
              </span>
            )}

            <button
              onClick={logout}
              className="btn btn-quiet px-2.5 py-1.5"
              aria-label="Sign out"
              title={t('common.signOut', 'Sign out')}
            >
              <LogOut className="h-4 w-4" />
              <span className="hidden md:inline">{t('common.signOut', 'Sign out')}</span>
            </button>
          </div>
        </header>

        <main id="main-content" className="flex-1 overflow-y-auto p-4 md:p-6" tabIndex={-1}>
          <Outlet />
        </main>
      </div>

      {sidebarOpen && (
        <button
          className="fixed right-3 top-3 z-50 rounded p-2 lg:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-label="Close navigation menu"
        >
          <X className="h-5 w-5 text-text-primary" />
        </button>
      )}
    </div>
  );
}
