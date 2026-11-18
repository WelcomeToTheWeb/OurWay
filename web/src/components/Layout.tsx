import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { LogOut, Menu, Pause, Search, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../auth/context';
import { Sidebar } from './Sidebar';
import { LinkStamp, Stamp } from './Stamp';
import { useGlobalShortcuts } from '../hooks/useKeyboardShortcuts';
import { useWebSocket } from '../hooks/useWebSocket';
import { useFleet } from '../hooks/useFleet';
import { getServerVersion, type ServerVersion } from '../api/system';
import { onScopeChange, scopeHeader } from '../api/client';
import { scopeCount, scopeLabel } from '../utils/clientFilter';
import { nextWindow, untilLabel } from '../utils/nextWindow';
import { formatWindow } from '../utils/patchWindow';
import { useRateLimit } from '../hooks/useRateLimit';

/** Tailwind's `lg`: at or above this width the rail is static furniture; below
 *  it the same rail is a drawer, which changes what the keyboard owes it. */
const RAIL_BREAKPOINT = '(min-width: 1024px)';
const isWideViewport = () =>
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(RAIL_BREAKPOINT).matches
    : true;

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
  const [serverVersion, setServerVersion] = useState<ServerVersion | null>(null);
  // Telling the technician that polling is resting belongs in the shift bar:
  // it explains why figures stop moving, before they go hunting for a bug.
  const { limited, secondsLeft } = useRateLimit();
  useGlobalShortcuts();

  useEffect(() => {
    const id = setInterval(() => setClock(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  // The shift bar reads the policy list off the shared fleet model (2.2-2):
  // the bar is mounted on every page, so as its own poller it was the read
  // every navigation paid.
  const { data: policies } = useFleet('patchPolicies');
  // Tenant names, for the scope readout below. The shared collection means the
  // bar adds no read to a page that already shows a client facet.
  const { data: clients } = useFleet('clients');

  // The scope the server said it applied (2.3-1's header). Printed on the bar
  // because "the technician cannot find the unit" is the ticket this release is
  // measured by, and the answer is usually not "the unit is missing" but "you
  // are not granted that tenant". An operator should see that before they
  // search, not after they open the network tab.
  const [scope, setScope] = useState<string>(() => scopeHeader());
  useEffect(() => onScopeChange(() => setScope(scopeHeader())), []);

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

  /* Below `lg` the rail is a drawer. A drawer that survives the navigation it
     caused, that Escape does not close, or that keeps focus when it is slid off
     screen is a dead end for anyone not using a mouse, so all four are wired
     here rather than in the Sidebar that rides inside it. */
  const location = useLocation();
  const navRef = useRef<HTMLDivElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const [isWide, setIsWide] = useState(isWideViewport);

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(RAIL_BREAKPOINT);
    const onChange = (event: MediaQueryListEvent) => {
      setIsWide(event.matches);
      if (event.matches) setSidebarOpen(false);
    };
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  useEffect(() => {
    setSidebarOpen(false);
  }, [location.pathname]);

  const navIsModal = sidebarOpen && !isWide;

  useEffect(() => {
    if (!navIsModal) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      setSidebarOpen(false);
    };
    document.addEventListener('keydown', onKeyDown);
    navRef.current?.focus();
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      // Focus goes back where it came from, unless the rail became static first.
      if (!isWideViewport()) menuButtonRef.current?.focus();
    };
  }, [navIsModal]);

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
          className="fixed inset-0 z-30 lg:hidden"
          style={{ background: 'var(--scrim)' }}
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      <div
        id="ourway-nav"
        ref={navRef}
        tabIndex={-1}
        data-open={sidebarOpen ? 'true' : 'false'}
        className={`rail-shell fixed inset-y-0 left-0 z-40 w-60 transform lg:static lg:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'
        }`}
        {...(navIsModal ? { role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Navigation' } : {})}
      >
        <Sidebar />
      </div>

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-16 shrink-0 items-center gap-2 border-b border-rule bg-bg-secondary px-3 md:px-5">
          <button
            ref={menuButtonRef}
            className="rounded p-2 text-text-secondary lg:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="Open navigation menu"
            aria-expanded={sidebarOpen}
            aria-controls="ourway-nav"
          >
            <Menu className="h-5 w-5" />
          </button>

          <form onSubmit={submitSearch} className="relative w-full max-w-[12rem] shrink lg:max-w-[13rem] xl:max-w-[16rem] 2xl:max-w-[20rem]" role="search">
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

          <div className="ml-auto flex min-w-0 items-center gap-2.5 md:gap-3.5">
            {/* The instruments are one shrinkable box; the identity cluster and
                the way out are not. When the bar runs out of room the
                instruments give — in the order their breakpoints shed them —
                and the operator's own name and the sign-out never do. The order
                is load-bearing in a second way: at every width that carries
                more than the clock and the link, the scope chip is present, and
                it is the only child allowed to shrink. So the squeeze always
                lands on an announced ellipsis instead of on the instrument at
                the right-hand end of the box, which a clipped bar cannot show. */}
            <div className="hidden min-w-0 items-center gap-2.5 overflow-hidden md:flex md:gap-3.5">
            {/* The window instrument is the widest one on the bar, so it is the
                instrument that yields: it appears only at `2xl`, where it can
                say the whole window. It is never the only place the answer
                exists — the dashboard and the patch-policy pages both name the
                next window — but the server version and the request budget are
                promised to the operator wherever the bar can hold them, so
                those appear a whole breakpoint earlier. */}
            <span className="hidden min-w-0 items-center gap-2 2xl:flex">
              <span className="cap-sm cap shrink-0 whitespace-nowrap text-text-muted">Next window</span>
              {upcoming ? (
                <Stamp
                  tone="due"
                  className="max-w-[18rem] min-w-0"
                  title={`${upcoming.policy.name} · ${formatWindow(upcoming.policy)}`}
                >
                  <span className="block min-w-0 truncate">
                    {formatWindow(upcoming.policy)} · {untilLabel(upcoming.at, clock)}
                  </span>
                </Stamp>
              ) : (
                <Stamp tone="neutral" title="No policy maintenance window is scheduled">
                  Unscheduled
                </Stamp>
              )}
            </span>

            <span className="hidden sm:flex items-baseline gap-2">
              <span className="cap-sm cap shrink-0 whitespace-nowrap text-text-muted">UTC</span>
              <time className="ow-num text-[15px] font-semibold text-text-primary" dateTime={clock.toISOString()}>
                {utc}
              </time>
            </span>

            <span
              className="hidden xl:flex items-baseline gap-2"
              title={t('layout.versionTitle', {
                server: serverVersion?.version || 'dev',
                commit: serverVersion?.git_commit || 'dev',
                console: consoleVersion,
              })}
            >
              <span className="cap-sm cap shrink-0 whitespace-nowrap text-text-muted">srv</span>
              <span className="ow-num text-[13px] text-text-primary">{serverVersion?.version || 'dev'}</span>
            </span>

            {scope !== '' && (
              /* Scope is the declared trivia, so it is the instrument that
                 yields: below `xl` it is not on the bar at all, because from
                 `md` to `lg` the box holds exactly the clock and the link and
                 nothing else fits. Above `xl` it does more than sit here — it
                 is the bar's shock absorber. Every other instrument is a flex
                 item with `min-width: auto`, so it refuses to shrink below its
                 own text; this one carries `min-w-0` and truncates inside, so
                 whatever the bar runs short on lands here, as an ellipsis whose
                 full value is in the `title`, instead of pushing the link
                 indicator past the edge of the box and out of reach. The answer
                 is never lost either way: the sidebar names the scope and the
                 Clients page lists every tenant. */
              <span
                className="hidden xl:flex min-w-0 items-baseline gap-2"
                title={t('layout.scopeTitle', {
                  scope: scopeLabel(scope, clients),
                  tenants: scopeCount(scope),
                })}
              >
                <span className="cap-sm cap shrink-0 whitespace-nowrap text-text-muted">{t('layout.scopeCap')}</span>
                <Stamp
                  tone={scope === 'none' ? 'offline' : scope === 'all' ? 'neutral' : 'online'}
                  className="max-w-[9rem] min-w-0"
                  title={t('layout.scopeTitle', {
                    scope: scopeLabel(scope, clients),
                    tenants: scopeCount(scope),
                  })}
                >
                  {/* `truncate` belongs on a block container. `.stamp` is an
                      inline-flex pill, and `text-overflow` does not reach the
                      anonymous flex item holding the text — put it on the pill
                      and the value is painted straight through the pill's edge
                      and clipped by the bar, with no ellipsis to say so. Same
                      reason the window instrument truncates an inner span. */}
                  <span className="block min-w-0 truncate">{scopeLabel(scope, clients)}</span>
                </Stamp>
                {/* A `title` on a non-focusable span is a description, not a
                    name, and it only arrives on hover: an ellipsis read aloud is
                    still an ellipsis. Spell the scope out for assistive tech. */}
                <span className="sr-only">
                  {t('layout.scopeTitle', {
                    scope: scopeLabel(scope, clients),
                    tenants: scopeCount(scope),
                  })}
                </span>
              </span>
            )}

            {/* One rate instrument, two states. When the server is refusing us,
                the figure it replaces is the reason: "100/min" becomes
                "Limited 12s". They are never both on the bar, because a
                transient state must not be the thing that runs the bar out of
                room — from `md` to `lg` the box is exactly full with the clock
                and the link, so anything that appears only sometimes has to
                arrive at `xl`, where the scope chip is present to absorb it. */}
            {limited ? (
              <span
                className="hidden xl:flex min-w-0 items-baseline gap-2"
                title="Server rate limit reached — polling rests until the minute budget refills"
              >
                <Stamp
                  tone="due"
                  className="max-w-[9rem] min-w-0"
                  title="Server rate limit reached — polling rests until the minute budget refills"
                >
                  <Pause className="h-3 w-3 shrink-0" aria-hidden="true" />
                  <span className="block min-w-0 truncate">Limited {secondsLeft}s</span>
                </Stamp>
              </span>
            ) : (
              typeof serverVersion?.rate_limit_per_minute === 'number' && (
                <span
                  className="hidden xl:flex items-baseline gap-2"
                  title={t('layout.rateBudgetTitle', {
                    perMinute: serverVersion.rate_limit_per_minute,
                    burst: serverVersion.rate_limit_burst ?? serverVersion.rate_limit_per_minute,
                  })}
                >
                  {/* No cap label here, unlike its neighbours: "100/min" already
                      says what it is, and the 55px a "budget" caption costs is
                      the difference between the tenant scope showing a name and
                      showing an empty pill at 1280. */}
                  <span className="ow-num text-[13px] text-text-primary">
                    {serverVersion.rate_limit_per_minute}/min
                  </span>
                </span>
              )
            )}

            {/* The link indicator is the one instrument that is never shed while
                the bar can hold it: it is the truth about whether the figures on
                screen are live. Trivia goes first — version, budget, scope. */}
            <span className="hidden md:flex">
              <LinkStamp connected={connected} />
            </span>
            </div>

            {user && (
              <span className="hidden shrink-0 items-center gap-2 sm:flex">
                <span
                  className="flex h-7 w-7 items-center justify-center rounded-full text-[11px] font-semibold"
                  style={{ background: 'var(--field-alt)', color: 'var(--text-secondary)' }}
                  aria-hidden="true"
                >
                  {user.username.slice(0, 2).toUpperCase()}
                </span>
                <span className="flex flex-col items-end leading-tight">
                  <span className="text-[13px] font-semibold text-text-primary">{user.username}</span>
                  <span className="cap-sm cap shrink-0 whitespace-nowrap text-text-muted">{(roles ?? ['viewer'])[0]}</span>
                </span>
              </span>
            )}

            <button
              onClick={logout}
              className="btn btn-quiet shrink-0 px-2.5 py-1.5"
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
