import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { MonitorSmartphone, RefreshCw, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { listSessions, endSession, type Session } from '../api/sessions';
import { Stamp } from '../components/Stamp';
import { agoLabel } from '../utils/policyScope';

/** Who is sitting at somebody else's screen right now. */
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
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('sheet.sessionsTitle')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {sessions.length} open remote session{sessions.length === 1 ? '' : 's'}
          </p>
        </div>
        <button onClick={load} className="btn">
          <RefreshCw className="h-3.5 w-3.5" />
          {t('common.refresh', 'Refresh')}
        </button>
      </div>

      {error && (
        <p role="alert" className="text-[13px] text-[var(--error)]">
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex h-40 items-center justify-center">
          <RefreshCw className="h-6 w-6 animate-spin text-[var(--service)]" aria-hidden="true" />
        </div>
      ) : sessions.length === 0 ? (
        <div className="panel flex flex-col items-center gap-3 px-6 py-14 text-center">
          <span className="flex h-14 w-14 items-center justify-center border border-rule">
            <MonitorSmartphone className="h-6 w-6 text-text-muted" aria-hidden="true" />
          </span>
          <p className="text-sm font-semibold text-text-primary">No one is remote-controlled right now</p>
          <p className="max-w-md text-[13px] text-text-secondary">
            Start a session from a unit's detail page — the agent streams its screen to this console, and to the
            native viewer on Windows.
          </p>
        </div>
      ) : (
        <div className="panel overflow-hidden">
          <table className="ledger">
            <thead>
              <tr>
                <th>Session</th>
                <th>Unit</th>
                <th>Opened</th>
                <th>State</th>
                <th className="text-right">Action</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.id} className="text-[13px]">
                  <td className="ow-num">{s.id.slice(0, 8)}…</td>
                  <td>
                    <Link
                      to={`/devices/${s.device_id}`}
                      className="ow-num text-[var(--service)] hover:underline"
                    >
                      {s.device_id.slice(0, 8)}…
                    </Link>
                  </td>
                  <td className="text-text-secondary">{agoLabel(s.created_at)}</td>
                  <td>
                    <Stamp tone={s.status === 'active' ? 'online' : 'neutral'}>{s.status}</Stamp>
                  </td>
                  <td className="text-right">
                    <button
                      onClick={() => handleEnd(s)}
                      disabled={endingId === s.id}
                      className="btn px-2 py-1 text-[11px]"
                    >
                      <X className="h-3.5 w-3.5" aria-hidden="true" />
                      End
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
