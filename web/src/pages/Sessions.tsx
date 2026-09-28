import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { MonitorSmartphone, RefreshCw, X } from 'lucide-react';
import { listSessions, endSession, type Session } from '../api/sessions';
import { useTranslation } from 'react-i18next';

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

export function Sessions() {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [endingId, setEndingId] = useState<string | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      setSessions(await listSessions());
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const handleEnd = async (session: Session) => {
    if (!confirm(`End session ${session.id.slice(0, 8)}?`)) return;
    setEndingId(session.id);
    try {
      await endSession(session.id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setEndingId(null);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">Sessions</h1>
          <p className="text-sm text-text-secondary">
            {sessions.length} active remote session{sessions.length === 1 ? '' : 's'}
          </p>
        </div>
        <button
          onClick={load}
          className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-2 text-sm text-text-primary transition-colors hover:bg-bg"
        >
          <RefreshCw className="h-4 w-4" />
          {t('common.refresh', 'Refresh')}
        </button>
      </div>

      {error && <p className="text-sm text-status-error">{error}</p>}

      {loading ? (
        <div className="flex h-32 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-accent" />
        </div>
      ) : sessions.length === 0 ? (
        <div className="rounded-xl border border-bg-border bg-bg-card p-8 text-center">
          <MonitorSmartphone className="mx-auto h-8 w-8 text-text-muted" />
          <p className="mt-3 text-sm text-text-secondary">
            No active sessions. Start one from a device's detail page.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {sessions.map((s) => (
            <div
              key={s.id}
              className="flex items-center justify-between rounded-xl border border-bg-border bg-bg-card p-4"
            >
              <div className="flex items-center gap-4">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-accent/15 text-accent">
                  <MonitorSmartphone className="h-5 w-5" />
                </div>
                <div>
                  <p className="text-sm font-medium text-text-primary">
                    {s.id.slice(0, 8)}…
                  </p>
                  <p className="text-xs text-text-secondary">
                    <Link
                      to={`/devices/${s.device_id}`}
                      className="text-accent hover:text-accent-dark"
                    >
                      Device {s.device_id.slice(0, 8)}…
                    </Link>
                    {' • '}
                    {timeAgo(s.created_at)}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <span
                  className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase ${
                    s.status === 'active'
                      ? 'bg-status-online/15 text-status-online'
                      : 'bg-accent/15 text-accent'
                  }`}
                >
                  {s.status}
                </span>
                <button
                  onClick={() => handleEnd(s)}
                  disabled={endingId === s.id}
                  className="flex items-center gap-1.5 rounded-lg bg-red-500/10 px-3 py-1.5 text-sm text-status-error transition-colors hover:bg-red-500/20 disabled:opacity-50"
                >
                  <X className="h-4 w-4" />
                  End
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
