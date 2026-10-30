import { useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { MonitorSmartphone, RefreshCw, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { endSession, type Session } from '../api/sessions';
import { startRecording, type SessionRecording } from '../api/recordings';
import { useFleet } from '../hooks/useFleet';
import { useClientScope } from '../hooks/useClientScope';
import { clientNameForSlug } from '../utils/clientFilter';
import { ClientFacet } from '../components/ClientFacet';
import { invalidateFleet } from '../stores/fleet';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';
import { agoLabel } from '../utils/policyScope';

/** Who is sitting at somebody else's screen right now. */
export function Sessions() {
  const { t } = useTranslation();
  // The session ledger is a fleet collection (2.2-2): the dashboard reads the
  // same list, so opening this page next to it costs no second read.
  const { data: sessions, loading, refresh } = useFleet('sessions', { owner: true });
  // 2.4-8: the recording ledger is a collection of the same read model, so the
  // session ledger can state which sessions **are** being recorded without adding
  // a read of its own. Joining by `session_id` is the whole join.
  const { data: recordingIndex } = useFleet('recordings');
  // The tenant facet (2.3-5): a session names a device, so filtering the
  // ledger by tenant is a lookup on that tenant's unit set.
  const scope = useClientScope();
  const visibleSessions =
    scope.slug === '' ? sessions : sessions.filter((x) => scope.scopedDeviceIds.has(x.device_id));
  const recordingBySession = new Map<string, SessionRecording>(
    recordingIndex.recordings.map((r) => [r.session_id, r])
  );
  const policy = recordingIndex.recording_policy;

  const [endingId, setEndingId] = useState<string | null>(null);
  const [startingId, setStartingId] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(() => refresh(), [refresh]);

  const handleRecord = async (session: Session) => {
    setStartingId(session.id);
    setError('');
    try {
      await startRecording(session.id);
      // The recording ledger is the collection the column reads; the mutation
      // that created a row tells the model, not the page.
      invalidateFleet('recordings');
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setStartingId(null);
      setPending(null);
    }
  };

  const handleEnd = async (session: Session) => {
    setEndingId(session.id);
    try {
      await endSession(session.id);
      // The ledger is shared: tell the model the row is gone so a dashboard
      // open in another tab does not keep showing it for half a minute.
      invalidateFleet('sessions');
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setEndingId(null);
      setPending(null);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('sheet.sessionsTitle')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {visibleSessions.length} open remote session{visibleSessions.length === 1 ? '' : 's'}
            {scope.slug !== '' && ` · ${clientNameForSlug(scope.clients, scope.slug)}` }
          </p>
        </div>
        <ClientFacet
          clients={scope.clients}
          value={scope.slug}
          onChange={scope.setSlug}
          includeUnassigned={scope.includeUnassigned}
        />
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
      ) : visibleSessions.length === 0 ? (
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
                <th>{t('sessions.colTenant')}</th>
                <th>Opened</th>
                <th>State</th>
                <th>Recording</th>
                <th className="text-right">Action</th>
              </tr>
            </thead>
            <tbody>
              {visibleSessions.map((s) => (
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
                  <td className="text-[11px] text-text-secondary">{clientNameForSlug(scope.clients, scope.slugOf(s.device_id))}</td>
                  <td className="text-text-secondary">{agoLabel(s.created_at)}</td>
                  <td>
                    <Stamp tone={s.status === 'active' ? 'online' : 'neutral'}>{s.status}</Stamp>
                  </td>
                  <td className="text-[11px] text-text-secondary">
                    {recordingBySession.has(s.id) ? (
                      <Stamp tone={recordingBySession.get(s.id)!.status === 'recording' ? 'due' : 'service'}>
                        {recordingBySession.get(s.id)!.status === 'recording' ? 'Recording' : 'Recorded'}
                      </Stamp>
                    ) : policy === 'off' ? (
                      <span>recording off</span>
                    ) : (
                      <button
                        onClick={() => handleRecord(s)}
                        disabled={startingId === s.id}
                        className="btn px-2 py-1 text-[11px]"
                        title="Start recording this session. Nothing in the product stops a recording once it has started."
                      >
                        Record
                      </button>
                    )}
                  </td>
                  <td className="text-right">
                    <button
                      onClick={() =>
                        setPending(
                          pendingConfirm(
                            t,
                            'endSession',
                            () => handleEnd(s),
                            { id: s.id.slice(0, 8) }
                          )
                        )
                      }
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

      <ConfirmSheet
        request={pending?.request ?? null}
        busy={endingId !== null}
        onCancel={() => setPending(null)}
        onConfirm={() => pending?.run()}
      />
    </div>
  );
}
