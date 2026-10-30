import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { Download, Play, RefreshCw, ScrollText } from 'lucide-react';

import {
  downloadRecordingManifest,
  recordingReasonLabel,
  type SessionRecording,
} from '../api/recordings';
import { useFleet } from '../hooks/useFleet';
import { useClientScope } from '../hooks/useClientScope';
import { clientNameForSlug } from '../utils/clientFilter';
import { ClientFacet } from '../components/ClientFacet';
import { Stamp, type StampTone } from '../components/Stamp';
import { RecordingPlayer } from '../components/RecordingPlayer';

// 2.4-8 · the recording ledger.
//
// The question this page answers is the one a compliance conversation starts
// with: *who was controlling that machine at 14:00*. So the row leads with the
// unit, the account, and the time — and the reason a recording exists is printed
// in the row, because "the installation records control sessions" and "an
// operator ordered this one" are different facts about the same screen.
//
// Playing a recording is reading a person's screen. The server audits the
// manifest read that opens it, and the page says so in the header: a person who
// watches a recording leaves a record of watching.

function statusTone(status: SessionRecording['status']): StampTone {
  if (status === 'recording') return 'due';
  if (status === 'missing') return 'alert';
  return 'online';
}

function clockLabel(iso: string | null): string {
  if (!iso) return 'running';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function bytesLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} kB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function Recordings() {
  const { data, loading, refresh } = useFleet('recordings', { owner: true });
  const scope = useClientScope();
  const [playing, setPlaying] = useState<string | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(() => refresh(), [refresh]);

  const rows =
    scope.slug === ''
      ? data.recordings
      : data.recordings.filter((r) => scope.scopedDeviceIds.has(r.device_id));

  const policy = data.recording_policy;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">Session recordings</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {rows.length} recording{rows.length === 1 ? '' : 's'}
            {scope.slug !== '' && ` · ${clientNameForSlug(scope.clients, scope.slug)}`}
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
          Refresh
        </button>
      </div>

      <div className="panel px-4 py-3">
        <p className="text-[13px] text-text-secondary">
          {policy === 'off' ? (
            <>
              <strong className="text-text-primary">This installation records no sessions.</strong> Set{' '}
              <code className="ow-num">RECORD_SESSIONS</code> to <code className="ow-num">on</code> or{' '}
              <code className="ow-num">control-only</code> and restart the server to record them. Nothing here
              will fill up: recording is a promise the installation makes, and it is not making one.
            </>
          ) : (
            <>
              <strong className="text-text-primary">
                Recording is on: {policy === 'on' ? 'every session is recorded from its start' : 'a session is recorded from the moment someone takes control'}
                .
              </strong>{' '}
              Recordings are kept {data.retention_days} days, then pruned with their files by the same sweep
              that prunes metric history. Playing a recording is audited.
            </>
          )}
        </p>
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
      ) : rows.length === 0 ? (
        <div className="panel flex flex-col items-center gap-3 px-6 py-14 text-center">
          <span className="flex h-14 w-14 items-center justify-center border border-rule">
            <ScrollText className="h-6 w-6 text-text-muted" aria-hidden="true" />
          </span>
          <p className="text-sm font-semibold text-text-primary">No recordings to read</p>
          <p className="max-w-md text-[13px] text-text-secondary">
            A recording exists when the installation's policy covers a session, or when the technician of a live
            session asks for one from the session panel.
          </p>
        </div>
      ) : (
        <div className="panel overflow-hidden">
          <table className="ledger">
            <thead>
              <tr>
                <th>Unit</th>
                <th>Account</th>
                <th>Tenant</th>
                <th>From</th>
                <th>To</th>
                <th>Frames</th>
                <th>Size</th>
                <th>State</th>
                <th>Why it exists</th>
                <th className="text-right">Playback</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className="text-[13px]">
                  <td>
                    <Link
                      to={`/devices/${r.device_id}`}
                      className="ow-num text-[var(--service)] hover:underline"
                    >
                      {r.device_name || `${r.device_id.slice(0, 8)}…`}
                    </Link>
                  </td>
                  <td className="text-text-primary">{r.username}</td>
                  <td className="text-[11px] text-text-secondary">
                    {clientNameForSlug(scope.clients, scope.slugOf(r.device_id))}
                  </td>
                  <td className="ow-num text-text-secondary">{clockLabel(r.started_at)}</td>
                  <td className="ow-num text-text-secondary">{clockLabel(r.ended_at)}</td>
                  <td className="ow-num text-text-primary">
                    {r.frame_count}
                    {r.dropped_frames > 0 && (
                      <span className="cap cap-sm text-[var(--error)]"> ·{r.dropped_frames} dropped</span>
                    )}
                    {r.interruptions > 0 && (
                      <span className="cap cap-sm text-text-muted"> ·{r.interruptions} resumes</span>
                    )}
                  </td>
                  <td className="ow-num text-text-secondary">{bytesLabel(r.bytes)}</td>
                  <td>
                    <Stamp tone={statusTone(r.status)}>{r.status}</Stamp>
                  </td>
                  <td className="text-[11px] text-text-secondary">
                    {recordingReasonLabel(r.reason)}
                    <button
                      onClick={() => void downloadRecordingManifest(r.id).catch(() => setError('The manifest export failed'))}
                      className="ml-2 btn px-1.5 py-0.5 text-[11px]"
                      title="Export the manifest (audited)"
                    >
                      <Download className="h-3 w-3" aria-hidden="true" />
                    </button>
                  </td>
                  <td className="text-right">
                    {r.playable ? (
                      <button
                        onClick={() => setPlaying(r.id)}
                        className="btn px-2 py-1 text-[11px]"
                        title="Playback is audited: this read names you and this recording"
                      >
                        <Play className="h-3.5 w-3.5" aria-hidden="true" /> Play
                      </button>
                    ) : (
                      <span className="cap cap-sm text-text-muted">no frames</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <p className="cap cap-sm text-text-muted">
        Playing a recording opens the frames and the keyboard and mouse the ledger recorded for that session, in
        the order they happened. The read that opens it is written to the audit ledger.
      </p>

      {playing && <RecordingPlayer recordingId={playing} onClose={() => setPlaying(null)} />}
    </div>
  );
}
