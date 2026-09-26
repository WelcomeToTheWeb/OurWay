import { useState, useEffect } from 'react';
import {
  User,
  Bell,
  Palette,
  Key,
  Trash2,
  Save,
  Eye,
  EyeOff,
  Check,
  Database,
  Plus,
  RotateCw,
  XCircle,
} from 'lucide-react';
import { useAuth } from '../auth/context';
import { useThemeStore, Theme } from '../stores/themeStore';
import { useTranslation } from 'react-i18next';
import i18n from '../i18n';
import {
  listAPIKeys,
  createAPIKey,
  deleteAPIKey,
  revokeAPIKey,
  rotateAPIKey,
  type APIKey as APIKeyType,
} from '../api/api-keys';

export function Settings() {
  const { user } = useAuth();
  const { theme, setTheme } = useThemeStore();
  const { t } = useTranslation();
  const [username, setUsername] = useState(user?.username || '');
  const [email, setEmail] = useState(user?.email || '');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [saved, setSaved] = useState(false);
  const [refreshInterval, setRefreshInterval] = useState('30');
  const [emailAlerts, setEmailAlerts] = useState(true);
  const [criticalOnly, setCriticalOnly] = useState(false);

  // API Keys
  const [apiKeys, setApiKeys] = useState<APIKeyType[]>([]);
  const [loadingKeys, setLoadingKeys] = useState(true);
  const [showKeyForm, setShowKeyForm] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyScopes, setNewKeyScopes] = useState<string[]>(['read', 'write']);
  const [newKeyExpires, setNewKeyExpires] = useState('never');
  const [createdKey, setCreatedKey] = useState<APIKeyType | null>(null);

  async function loadAPIKeys() {
    try {
      const keys = await listAPIKeys();
      setApiKeys(keys);
    } catch (err) {
      console.error('Failed to load API keys', err);
    } finally {
      setLoadingKeys(false);
    }
  }

  useEffect(() => {
    loadAPIKeys();
  }, []);

  async function handleCreateKey() {
    if (!newKeyName) return;
    try {
      const key = await createAPIKey({
        name: newKeyName,
        scopes: newKeyScopes,
        expires: newKeyExpires,
      });
      setCreatedKey(key);
      setNewKeyName('');
      setShowKeyForm(false);
      loadAPIKeys();
    } catch (err) {
      console.error('Failed to create API key', err);
    }
  }

  async function handleDeleteKey(id: string) {
    if (!confirm('Delete this API key?')) return;
    try {
      await deleteAPIKey(id);
      loadAPIKeys();
    } catch (err) {
      console.error('Failed to delete API key', err);
    }
  }

  async function handleRevokeKey(id: string) {
    if (!confirm('Revoke this API key?')) return;
    try {
      await revokeAPIKey(id);
      loadAPIKeys();
    } catch (err) {
      console.error('Failed to revoke API key', err);
    }
  }

  async function handleRotateKey(id: string) {
    try {
      const key = await rotateAPIKey(id);
      setCreatedKey(key);
      loadAPIKeys();
    } catch (err) {
      console.error('Failed to rotate API key', err);
    }
  }

  async function handleSaveProfile(e: React.FormEvent) {
    e.preventDefault();
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  }

  function handleSavePassword(e: React.FormEvent) {
    e.preventDefault();
    setSaved(true);
    setPassword('');
    setTimeout(() => setSaved(false), 2000);
  }



  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h1 className="text-2xl font-semibold text-text-primary">{t('settings.title')}</h1>
        <p className="text-sm text-text-secondary">{t('settings.subtitle')}</p>
      </div>

      {saved && (
        <div className="flex items-center gap-2 rounded-xl border border-status-online/30 bg-status-online/10 px-4 py-3">
          <Check className="h-4 w-4 text-status-online" />
          <span className="text-sm text-status-online">Settings saved successfully</span>
        </div>
      )}

      {/* Profile Section */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
            <User className="h-5 w-5 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">{t('settings.profile.title')}</h2>
            <p className="text-xs text-text-secondary">{t('settings.profile.subtitle')}</p>
          </div>
        </div>

        <form onSubmit={handleSaveProfile} className="mt-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">{t('settings.profile.username')}</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-text-secondary">Email</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
            </div>
          </div>
          <button
            type="submit"
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary transition-colors hover:bg-accent-dark"
          >
            <Save className="h-4 w-4" />
            Save Profile
          </button>
        </form>
      </div>

      {/* Change Password */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
            <Key className="h-5 w-5 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">Change Password</h2>
            <p className="text-xs text-text-secondary">Update your account password</p>
          </div>
        </div>

        <form onSubmit={handleSavePassword} className="mt-4 space-y-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-text-secondary">New Password</label>
            <div className="relative">
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 pr-10 text-sm text-text-primary focus:border-accent focus:outline-none"
                placeholder="Enter new password"
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-text-secondary hover:text-text-primary"
              >
                {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </div>
          <button
            type="submit"
            className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-text-primary transition-colors hover:bg-accent-dark"
          >
            <Save className="h-4 w-4" />
            Update Password
          </button>
        </form>
      </div>

      {/* Notifications */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
            <Bell className="h-5 w-5 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">Notifications</h2>
            <p className="text-xs text-text-secondary">How you want to be alerted</p>
          </div>
        </div>

        <div className="mt-4 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">Email alerts</p>
              <p className="text-xs text-text-muted">Receive alerts via email</p>
            </div>
            <button
              type="button"
              onClick={() => setEmailAlerts(!emailAlerts)}
              className={`relative h-6 w-11 rounded-full transition-colors ${emailAlerts ? 'bg-accent' : 'bg-bg-border'}`}
            >
              <span
                className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${emailAlerts ? 'translate-x-5' : ''}`}
              />
            </button>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">In-app notifications</p>
              <p className="text-xs text-text-muted">Show toast notifications in the browser</p>
            </div>
            <button
              type="button"
              className="relative h-6 w-11 rounded-full bg-accent"
            >
              <span className="absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white transition-transform translate-x-5" />
            </button>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">Critical alerts only</p>
              <p className="text-xs text-text-muted">Only notify for critical severity alerts</p>
            </div>
            <button
              type="button"
              onClick={() => setCriticalOnly(!criticalOnly)}
              className={`relative h-6 w-11 rounded-full transition-colors ${criticalOnly ? 'bg-accent' : 'bg-bg-border'}`}
            >
              <span
                className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${criticalOnly ? 'translate-x-5' : ''}`}
              />
            </button>
          </div>
        </div>
      </div>

      {/* Appearance */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
            <Palette className="h-5 w-5 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">Appearance</h2>
            <p className="text-xs text-text-secondary">Customize the look and feel</p>
          </div>
        </div>

        <div className="mt-4 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">Theme</p>
              <p className="text-xs text-text-muted">Choose your preferred color scheme</p>
            </div>
            <select
              value={theme}
              onChange={(e) => setTheme(e.target.value as Theme)}
              className="rounded-lg border border-bg-border bg-bg px-3 py-1.5 text-sm text-text-primary"
              aria-label="Theme selection"
            >
              <option value="dark">Dark</option>
              <option value="light">Light</option>
              <option value="system">System</option>
            </select>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">{t('settings.appearance.theme')}</p>
              <p className="text-xs text-text-muted">{t('settings.appearance.themeDesc')}</p>
            </div>
            <select
              value={i18n.language.split('-')[0]}
              onChange={(e) => i18n.changeLanguage(e.target.value)}
              className="rounded-lg border border-bg-border bg-bg px-3 py-1.5 text-sm text-text-primary"
              aria-label="Language selection"
            >
              <option value="en">English</option>
              <option value="de">Deutsch</option>
              <option value="fr">Français</option>
            </select>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-text-primary">{t('settings.appearance.refresh')}</p>
              <p className="text-xs text-text-muted">{t('settings.appearance.refreshDesc')}</p>
            </div>
            <select
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(e.target.value)}
              className="rounded-lg border border-bg-border bg-bg px-3 py-1.5 text-sm text-text-primary"
            >
              <option value="10">{t('settings.appearance.every10s')}</option>
              <option value="30">{t('settings.appearance.every30s')}</option>
              <option value="60">{t('settings.appearance.everyMinute')}</option>
              <option value="300">{t('settings.appearance.every5Minutes')}</option>
            </select>
          </div>
        </div>
      </div>

      {/* Keyboard Shortcuts */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
            <Key className="h-5 w-5 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">Keyboard Shortcuts</h2>
            <p className="text-xs text-text-secondary">Quick navigation and actions</p>
          </div>
        </div>

        <div className="mt-4 space-y-2">
          {[
            { keys: ['g', 'd'], desc: 'Go to Devices' },
            { keys: ['g', 'a'], desc: 'Go to Alerts' },
            { keys: ['g', 'f'], desc: 'Go to Files' },
            { keys: ['g', 'p'], desc: 'Go to Patches' },
            { keys: ['g', 'u'], desc: 'Go to Users' },
            { keys: ['g', 's'], desc: 'Go to Settings' },
            { keys: ['g', 'h'], desc: 'Go to Dashboard' },
          ].map((shortcut, i) => (
            <div key={i} className="flex items-center justify-between">
              <span className="text-sm text-text-secondary">{shortcut.desc}</span>
              <div className="flex items-center gap-1">
                {shortcut.keys.map((key, j) => (
                  <kbd
                    key={j}
                    className="rounded border border-bg-border bg-bg px-2 py-0.5 text-xs font-mono text-text-primary"
                  >
                    {key}
                  </kbd>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* API Keys Section */}
      <div className="rounded-xl border border-bg-border bg-bg-card p-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/10">
              <Key className="h-5 w-5 text-accent" />
            </div>
            <div>
              <h2 className="text-sm font-medium text-text-primary">API Keys</h2>
              <p className="text-xs text-text-secondary">Manage your API keys for programmatic access</p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => setShowKeyForm(!showKeyForm)}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-accent-dark"
          >
            <Plus className="h-3.5 w-3.5" />
            New Key
          </button>
        </div>

        {createdKey && (
          <div className="mt-4 rounded-lg border border-status-online/30 bg-status-online/10 p-3">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs font-medium text-status-online">API Key Created</p>
                <p className="text-xs text-text-muted">Copy this key now — it won't be shown again</p>
              </div>
              <button
                type="button"
                onClick={() => setCreatedKey(null)}
                className="text-text-muted hover:text-text-primary"
              >
                <XCircle className="h-4 w-4" />
              </button>
            </div>
            <div className="mt-2 flex items-center gap-2">
              <code className="flex-1 rounded bg-bg px-3 py-2 text-xs font-mono text-text-primary">
                {createdKey.key}
              </code>
              <button
                type="button"
                onClick={() => navigator.clipboard.writeText(createdKey.key)}
                className="rounded bg-bg-secondary px-3 py-2 text-xs text-text-primary transition-colors hover:bg-bg"
              >
                Copy
              </button>
            </div>
          </div>
        )}

        {showKeyForm && (
          <div className="mt-4 rounded-lg border border-bg-border bg-bg p-4">
            <h3 className="text-sm font-medium text-text-primary">Create New API Key</h3>
            <div className="mt-3 space-y-3">
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-text-secondary">Name</label>
                <input
                  type="text"
                  value={newKeyName}
                  onChange={(e) => setNewKeyName(e.target.value)}
                  placeholder="e.g. Production Key"
                  className="w-full rounded-lg border border-bg-border bg-bg-card px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
                />
              </div>
              <div className="flex items-center gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-medium text-text-secondary">Scopes</label>
                  <div className="flex items-center gap-3">
                    {['read', 'write'].map((scope) => (
                      <label key={scope} className="flex items-center gap-1.5 text-xs text-text-secondary">
                        <input
                          type="checkbox"
                          checked={newKeyScopes.includes(scope)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setNewKeyScopes([...newKeyScopes, scope]);
                            } else {
                              setNewKeyScopes(newKeyScopes.filter((s) => s !== scope));
                            }
                          }}
                          className="rounded border-bg-border"
                        />
                        {scope}
                      </label>
                    ))}
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-medium text-text-secondary">Expires</label>
                  <select
                    value={newKeyExpires}
                    onChange={(e) => setNewKeyExpires(e.target.value)}
                    className="rounded-lg border border-bg-border bg-bg-card px-3 py-1.5 text-xs text-text-primary"
                  >
                    <option value="never">Never</option>
                    <option value="1h">1 hour</option>
                    <option value="24h">24 hours</option>
                    <option value="7d">7 days</option>
                    <option value="30d">30 days</option>
                    <option value="90d">90 days</option>
                  </select>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleCreateKey}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-medium text-text-primary transition-colors hover:bg-accent-dark"
                >
                  <Key className="h-3.5 w-3.5" />
                  Create Key
                </button>
                <button
                  type="button"
                  onClick={() => setShowKeyForm(false)}
                  className="rounded-lg bg-bg-secondary px-4 py-2 text-xs text-text-primary transition-colors hover:bg-bg"
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        )}

        {loadingKeys ? (
          <div className="mt-4 flex items-center justify-center py-8">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-accent border-t-transparent" />
          </div>
        ) : apiKeys.length === 0 ? (
          <div className="mt-4 rounded-lg border border-dashed border-bg-border p-8 text-center">
            <Key className="mx-auto h-8 w-8 text-text-muted" />
            <p className="mt-2 text-sm text-text-secondary">No API keys yet</p>
            <p className="text-xs text-text-muted">Create a key to access the OurWay API programmatically</p>
          </div>
        ) : (
          <div className="mt-4 space-y-2">
            {apiKeys.map((key) => (
              <div key={key.id} className="flex items-center justify-between rounded-lg border border-bg-border bg-bg p-3">
                <div className="flex items-center gap-3">
                  <Key className="h-4 w-4 text-accent" />
                  <div>
                    <p className="text-sm font-medium text-text-primary">{key.name}</p>
                    <p className="text-xs text-text-muted">
                      {key.key.substring(0, 8)}...{key.key.substring(key.key.length - 4)}
                      {key.revoked_at && ' (revoked)'}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] text-accent">
                    {JSON.parse(key.scopes).join(', ')}
                  </span>
                  {key.expires_at && (
                    <span className="rounded-full bg-bg-secondary px-2 py-0.5 text-[10px] text-text-secondary">
                      expires {new Date(key.expires_at).toLocaleDateString()}
                    </span>
                  )}
                  <button
                    type="button"
                    onClick={() => handleRotateKey(key.id)}
                    title="Rotate key"
                    className="rounded p-1 text-text-muted hover:bg-bg-secondary hover:text-text-primary"
                  >
                    <RotateCw className="h-3.5 w-3.5" />
                  </button>
                  {!key.revoked_at && (
                    <button
                      type="button"
                      onClick={() => handleRevokeKey(key.id)}
                      title="Revoke key"
                      className="rounded p-1 text-text-muted hover:bg-bg-secondary hover:text-status-warning"
                    >
                      <XCircle className="h-3.5 w-3.5" />
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => handleDeleteKey(key.id)}
                    title="Delete key"
                    className="rounded p-1 text-text-muted hover:bg-bg-secondary hover:text-status-error"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Danger Zone */}
      <div className="rounded-xl border border-status-error/30 bg-bg-card p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-status-error/10">
            <Trash2 className="h-5 w-5 text-status-error" />
          </div>
          <div>
            <h2 className="text-sm font-medium text-text-primary">Danger Zone</h2>
            <p className="text-xs text-text-secondary">Irreversible actions</p>
          </div>
        </div>

        <div className="mt-4 space-y-3">
          <div className="flex items-center justify-between rounded-lg border border-bg-border p-3">
            <div>
              <p className="text-sm font-medium text-text-primary">Clear all monitoring data</p>
              <p className="text-xs text-text-muted">Delete all metrics history and alerts</p>
            </div>
            <button
              type="button"
              className="flex items-center gap-1.5 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs text-text-primary transition-colors hover:bg-bg hover:text-status-error"
            >
              <Database className="h-3.5 w-3.5" />
              Clear Data
            </button>
          </div>
          <div className="flex items-center justify-between rounded-lg border border-bg-border p-3">
            <div>
              <p className="text-sm font-medium text-text-primary">Delete account</p>
              <p className="text-xs text-text-muted">Permanently delete your account and all devices</p>
            </div>
            <button
              type="button"
              className="flex items-center gap-1.5 rounded-lg bg-status-error px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-status-error/80"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete Account
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
