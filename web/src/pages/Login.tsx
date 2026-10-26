import { useState, useEffect, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { MonitorSmartphone } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../auth/context';
import { listSSOProviders, authorizeProvider, exchangeSSOCode, type SSOProvider } from '../api/sso';
import { getAuthStatus } from '../api/auth';
import { Stamp } from '../components/Stamp';

/**
 * The console door: a plate with the shop's own facts (this server, this
 * build, self-hosted) beside the sign-in fields. First run prints the
 * register form and says plainly that this account becomes the admin.
 */
export function Login() {
  const { t } = useTranslation();
  const { login, register, isAuthenticated, setToken } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [providers, setProviders] = useState<SSOProvider[]>([]);
  const [providersLoading, setProvidersLoading] = useState(true);
  const [hasUsers, setHasUsers] = useState<boolean | null>(null);

  // Handle SSO callback: the server redirects to /login?sso_code=<code>
  // (tokens never appear in the URL — C6). Redeem the one-time code via
  // POST /api/auth/sso/exchange to obtain the token pair.
  useEffect(() => {
    const ssoCode = searchParams.get('sso_code');
    if (!ssoCode) return;
    let cancelled = false;
    (async () => {
      try {
        const { access_token, refresh_token } = await exchangeSSOCode(ssoCode);
        if (cancelled) return;
        setToken(access_token, refresh_token);
        navigate('/');
      } catch {
        if (!cancelled) setError('SSO login failed. Sign in again, or use the password form.');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [searchParams, setToken, navigate]);

  if (isAuthenticated) {
    navigate('/');
    return null;
  }

  useEffect(() => {
    getAuthStatus()
      .then((status) => setHasUsers(status.has_users))
      .catch(() => setHasUsers(true));
  }, []);

  useEffect(() => {
    let mounted = true;
    listSSOProviders()
      .then((p) => {
        if (mounted) setProviders(p);
      })
      .catch(() => {})
      .finally(() => {
        if (mounted) setProvidersLoading(false);
      });
    return () => {
      mounted = false;
    };
  }, []);

  async function handleLogin(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      await login(username, password);
      navigate('/');
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  async function handleRegister(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      await register(username, email, password);
      navigate('/');
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  function handleSSO(provider: SSOProvider) {
    const url = authorizeProvider(provider.name);
    // The authorize endpoint is our own server path; only navigate to
    // that exact prefix (same-origin, handled by the router).
    if (url.startsWith('/api/auth/sso/')) {
      navigate(url);
    }
  }

  if (hasUsers === null) {
    return (
      <main className="plate flex min-h-screen items-center justify-center">
        <span className="cap cap-lg" style={{ color: 'var(--ink-on-plate-muted)' }}>
          Opening the console…
        </span>
      </main>
    );
  }

  const origin = typeof window !== 'undefined' ? window.location.host : '';

  return (
    <main className="grid min-h-screen grid-cols-1 lg:grid-cols-[22rem_minmax(0,1fr)]">
      {/* The plate */}
      <section className="plate flex flex-col justify-between px-6 py-8">
        <div>
          <div className="flex items-center gap-2.5">
            <span className="flex h-9 w-9 items-center justify-center border" style={{ borderColor: 'rgba(232,228,216,0.4)' }}>
              <MonitorSmartphone className="h-4 w-4" style={{ color: 'var(--ink-on-plate)' }} aria-hidden="true" />
            </span>
            <span>
              <span className="ow-display block text-[22px] leading-none" style={{ color: 'var(--ink-on-plate)' }}>
                {t('app.name')}
              </span>
              <span className="cap-sm cap mt-1 block" style={{ color: 'var(--ink-on-plate-muted)' }}>
                {t('app.tagline')}
              </span>
            </span>
          </div>

          <p className="mt-8 text-[13px] leading-relaxed" style={{ color: 'var(--ink-on-plate)' }}>
            Monitoring, patching, remote control and file transfer for Windows, Linux and macOS units — on a
            server you run.
          </p>
        </div>

        <dl className="mt-8 space-y-2">
          <div className="flex items-baseline justify-between gap-3 border-b pb-1.5" style={{ borderColor: 'rgba(232,228,216,0.16)' }}>
            <dt className="cap-sm cap" style={{ color: 'var(--ink-on-plate-muted)' }}>
              Server
            </dt>
            <dd className="ow-num text-[12px]" style={{ color: 'var(--ink-on-plate)' }}>
              {origin}
            </dd>
          </div>
          <div className="flex items-baseline justify-between gap-3 border-b pb-1.5" style={{ borderColor: 'rgba(232,228,216,0.16)' }}>
            <dt className="cap-sm cap" style={{ color: 'var(--ink-on-plate-muted)' }}>
              Hosting
            </dt>
            <dd>
              <span className="stamp" style={{ color: 'var(--ink-on-plate)' }}>
                Self-hosted
              </span>
            </dd>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <dt className="cap-sm cap" style={{ color: 'var(--ink-on-plate-muted)' }}>
              Licence
            </dt>
            <dd className="ow-num text-[12px]" style={{ color: 'var(--ink-on-plate)' }}>
              MIT
            </dd>
          </div>
        </dl>
      </section>

      {/* The door */}
      <section className="flex items-center justify-center px-5 py-10">
        <div className="w-full max-w-[26rem] panel p-5">
          <h1 className="text-[24px]">{hasUsers ? 'Sign in' : 'First run'}</h1>
          <p className="cap cap-sm mt-1 border-b border-rule pb-3 text-text-muted">
            {hasUsers ? 'Technicians, managers and admins on this console' : 'Create the first admin account'}
          </p>

          {hasUsers ? (
            <form onSubmit={handleLogin} className="mt-4 flex flex-col gap-3">
              {error && (
                <p role="alert" className="border border-[var(--error)] px-3 py-2 text-[12.5px] text-[var(--error)]">
                  {error}
                </p>
              )}

              <div className="flex flex-col gap-1">
                <label className="field-label" htmlFor="login-username">
                  {t('auth.username')}
                </label>
                <input
                  id="login-username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="input py-2.5 text-[13px]"
                  placeholder={t('auth.usernamePlaceholder')}
                  autoComplete="username"
                  required
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="field-label" htmlFor="login-password">
                  {t('auth.password')}
                </label>
                <input
                  id="login-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="input py-2.5 text-[13px]"
                  placeholder={t('auth.passwordPlaceholder')}
                  autoComplete="current-password"
                  required
                />
              </div>

              <button type="submit" disabled={loading} className="btn btn-primary mt-2 py-2.5 text-[13px]">
                {loading ? t('auth.signingIn') : t('auth.signIn')}
              </button>

              {!providersLoading && providers.length > 0 && (
                <div className="mt-2 flex flex-col gap-2">
                  <div className="flex items-center gap-3">
                    <span className="h-px flex-1 bg-[var(--rule)]" />
                    <span className="cap-sm cap text-text-muted">{t('auth.orSignInWith')}</span>
                    <span className="h-px flex-1 bg-[var(--rule)]" />
                  </div>
                  {providers.map((p) => (
                    <button
                      key={p.name}
                      type="button"
                      onClick={() => handleSSO(p)}
                      className="btn py-2.5 text-[13px]"
                    >
                      <ProviderIcon provider={p.name} />
                      {t('auth.signInWith', { provider: providerDisplayName(p.name) })}
                    </button>
                  ))}
                </div>
              )}
            </form>
          ) : (
            <form onSubmit={handleRegister} className="mt-5 flex flex-col gap-3">
              {error && (
                <p role="alert" className="border border-[var(--error)] px-3 py-2 text-[12.5px] text-[var(--error)]">
                  {error}
                </p>
              )}

              <p className="border border-rule bg-[var(--field)] px-3 py-2 text-[12.5px] text-text-secondary">
                This account becomes the console's admin: full access to units, users, policies and settings.
                Enrol agents after it exists — self-registration closes once an account is here.
              </p>

              <div className="flex flex-col gap-1">
                <label className="field-label" htmlFor="reg-username">
                  {t('auth.username')}
                </label>
                <input
                  id="reg-username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="input py-2.5 text-[13px]"
                  placeholder={t('auth.usernamePlaceholder')}
                  autoComplete="username"
                  required
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="field-label" htmlFor="reg-email">
                  {t('auth.email')}
                </label>
                <input
                  id="reg-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="input py-2.5 text-[13px]"
                  placeholder={t('auth.emailPlaceholder')}
                  autoComplete="email"
                  required
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="field-label" htmlFor="reg-password">
                  {t('auth.password')}
                </label>
                <input
                  id="reg-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="input py-2.5 text-[13px]"
                  placeholder={t('auth.passwordPlaceholder')}
                  autoComplete="new-password"
                  required
                />
              </div>

              <button type="submit" disabled={loading} className="btn btn-primary mt-2 py-2.5 text-[13px]">
                {loading ? t('auth.creatingAccount') : t('auth.getStarted')}
              </button>
            </form>
          )}

          <div className="mt-6 flex items-center gap-2">
            <Stamp tone="neutral" title="Sessions use short-lived access tokens refreshed over the API">
              JWT session
            </Stamp>
            <span className="text-[11px] text-text-muted">
              {hasUsers ? 'Access tokens expire and refresh automatically.' : 'No agents are enrolled on this server yet.'}
            </span>
          </div>
        </div>
      </section>
    </main>
  );
}

function providerDisplayName(name: string): string {
  switch (name) {
    case 'google':
      return 'Google';
    case 'microsoft':
      return 'Microsoft';
    case 'apple':
      return 'Apple';
    default:
      return name.charAt(0).toUpperCase() + name.slice(1);
  }
}

function ProviderIcon({ provider }: { provider: string }) {
  switch (provider) {
    case 'google':
      return (
        <svg width="16" height="16" viewBox="0 0 24 24" role="img" aria-label="Google">
          <title>Google</title>
          <path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" />
          <path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" />
          <path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.26 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" />
          <path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" />
        </svg>
      );
    case 'microsoft':
      return (
        <svg width="16" height="16" viewBox="0 0 24 24" role="img" aria-label="Microsoft">
          <title>Microsoft</title>
          <path fill="#F25022" d="M1 1h11v11H1z" />
          <path fill="#7FBA00" d="M14 1h11v11H14z" />
          <path fill="#00A4EF" d="M1 14h11v11H1z" />
          <path fill="#FFB900" d="M14 14h11v11H14z" />
        </svg>
      );
    case 'apple':
      return (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" role="img" aria-label="Apple">
          <title>Apple</title>
          <path d="M17.05 20.28c-.98.95-2.05.8-3.08.35-1.09-.46-2.09-.48-3.24 0-1.22.5-2.12.43-3.06-.35C2.43 15.25 3.04 8.51 7.12 7.36c1.88-.53 3.2.52 4.28.97 1.18-.48 2.43-1.35 4.08-.82 1.65.47 2.86 1.76 3.06 3.44-2.93 1.42-2.36 5.29.15 6.14-.45 1.61-1.2 3.19-1.64 3.99-.52.96-.98 1.89-1.93 2.85h-.07zM12.03 7.2c-.26-1.63.73-3.27 2.11-4.01.35-.18.73-.31 1.12-.39.27.55.34 1.19.15 1.82-.22.76-.73 1.45-1.37 1.94-.62.47-1.4.74-2.01.64z" />
        </svg>
      );
    default:
      return null;
  }
}
