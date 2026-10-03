import { useState, useEffect, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { MonitorSmartphone } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../auth/context';
import { listSSOProviders, authorizeProvider, exchangeSSOCode, type SSOProvider } from '../api/sso';
import { getAuthStatus } from '../api/auth';

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
        if (!cancelled) setError('SSO login failed. Please try again.');
      }
    })();
    return () => { cancelled = true; };
  }, [searchParams, setToken, navigate]);

  if (isAuthenticated) {
    navigate('/');
    return null;
  }

  // Check if users exist (first-run detection)
  useEffect(() => {
    getAuthStatus()
      .then((status) => setHasUsers(status.has_users))
      .catch(() => setHasUsers(true));
  }, []);

  // Load SSO providers
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

  // Loading state while checking for existing users
  if (hasUsers === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-bg">
        <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-accent">
          <MonitorSmartphone className="h-7 w-7 text-text-primary" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-accent">
            <MonitorSmartphone className="h-7 w-7 text-text-primary" />
          </div>
          <div className="text-center">
            <h1 className="text-2xl font-bold text-text-primary">{t('app.name')}</h1>
            <p className="text-sm text-text-secondary">{t('app.tagline')}</p>
          </div>
        </div>

        {hasUsers ? (
          /* Login form */
          <form
            onSubmit={handleLogin}
            className="flex flex-col gap-4 rounded-2xl border border-bg-border bg-bg-card p-6"
          >
            {error && (
              <p className="rounded-lg bg-status-error/15 px-3 py-2 text-sm text-status-error">
                {error}
              </p>
            )}

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('auth.username')}</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                placeholder={t('auth.usernamePlaceholder')}
                required
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('auth.password')}</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                placeholder={t('auth.passwordPlaceholder')}
                required
              />
            </div>

            <button
              type="submit"
              disabled={loading}
              className="mt-2 w-full rounded-lg bg-accent py-2.5 text-sm font-semibold text-white transition-colors hover:bg-accent-dark disabled:opacity-60"
            >
              {loading ? t('auth.signingIn') : t('auth.signIn')}
            </button>

            {/* SSO Buttons */}
            {!providersLoading && providers.length > 0 && (
              <div className="flex flex-col gap-3 pt-2">
                <div className="relative flex items-center">
                  <div className="flex-1 border-t border-bg-border" />
                  <span className="px-3 text-xs text-text-muted">{t('auth.orSignInWith')}</span>
                  <div className="flex-1 border-t border-bg-border" />
                </div>

                {providers.map((p) => (
                  <button
                    key={p.name}
                    type="button"
                    onClick={() => handleSSO(p)}
                    className="flex w-full items-center justify-center gap-2 rounded-lg border border-bg-border bg-bg px-4 py-2.5 text-sm text-text-primary transition-colors hover:bg-bg"
                  >
                    <ProviderIcon provider={p.name} />
                    {t('auth.signInWith', { provider: providerDisplayName(p.name) })}
                  </button>
                ))}
              </div>
            )}
          </form>
        ) : (
          /* Registration form (first run) */
          <form
            onSubmit={handleRegister}
            className="flex flex-col gap-4 rounded-2xl border border-bg-border bg-bg-card p-6"
          >
            {error && (
              <p className="rounded-lg bg-status-error/15 px-3 py-2 text-sm text-status-error">
                {error}
              </p>
            )}

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('auth.username')}</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                placeholder={t('auth.usernamePlaceholder')}
                required
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('auth.email')}</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                placeholder={t('auth.emailPlaceholder')}
                required
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('auth.password')}</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                placeholder={t('auth.passwordPlaceholder')}
                required
              />
            </div>

            <button
              type="submit"
              disabled={loading}
              className="mt-2 w-full rounded-lg bg-accent py-2.5 text-sm font-semibold text-white transition-colors hover:bg-accent-dark disabled:opacity-60"
            >
              {loading ? t('auth.creatingAccount') : t('auth.getStarted')}
            </button>
          </form>
        )}
      </div>
    </div>
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
