import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Download, Pause, Play, X } from 'lucide-react';
import {
  downloadRecordingFile,
  downloadRecordingManifest,
  fetchRecordingFrame,
  getRecordingEvents,
  getRecordingManifest,
  recordingReasonLabel,
  type RecordingEvent,
  type RecordingManifest,
} from '../api/recordings';
import { Stamp } from './Stamp';

// 2.4-8 · playback.
//
// The manifest read is the act that opens a recording, and the server audits it
// (`session.recording_play`), so opening this panel is a recorded act and the
// panel says so on the screen. The frames arrive one at a time through the
// authenticated client — an `<img src>` cannot carry the bearer token, and a URL
// that does is a token in the browser history.
//
// What the panel states that a video player would hide: the minutes a server
// restart left unrecorded, the frames the size cap refused, the input events the
// ledger recorded for this session, and the fact that this recording is a
// person's screen.

interface RecordingPlayerProps {
  recordingId: string;
  onClose: () => void;
}

function clockLabel(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function bytesLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} kB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function RecordingPlayer({ recordingId, onClose }: RecordingPlayerProps) {
  const [manifest, setManifest] = useState<RecordingManifest | null>(null);
  const [events, setEvents] = useState<RecordingEvent[]>([]);
  const [index, setIndex] = useState(0);
  const [frameUrl, setFrameUrl] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [playing, setPlaying] = useState(false);
  const [loadingFrame, setLoadingFrame] = useState(false);
  const urlRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getRecordingManifest(recordingId)
      .then((m) => {
        if (cancelled) return;
        setManifest(m);
        setIndex(0);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'The recording could not be opened');
      });
    getRecordingEvents(recordingId)
      .then((rows) => {
        if (!cancelled) setEvents(rows);
      })
      .catch(() => {
        // The overlay is a reading of the ledger. A ledger read that fails leaves
        // the screen playable and the overlay empty, stated below.
        if (!cancelled) setEvents([]);
      });
    return () => {
      cancelled = true;
    };
  }, [recordingId]);

  const frames = manifest?.frames ?? [];
  const playable = (manifest?.playable ?? false) && frames.length > 0;

  const showFrame = useCallback(
    async (at: number) => {
      if (!playable) return;
      setLoadingFrame(true);
      try {
        const url = await fetchRecordingFrame(recordingId, at);
        if (urlRef.current) URL.revokeObjectURL(urlRef.current);
        urlRef.current = url;
        setFrameUrl(url);
      } catch {
        setError('That frame is not in this recording.');
      } finally {
        setLoadingFrame(false);
      }
    },
    [playable, recordingId],
  );

  useEffect(() => {
    void showFrame(index);
    return () => {
      setLoadingFrame(false);
    };
  }, [index, showFrame]);

  useEffect(
    () => () => {
      if (urlRef.current) URL.revokeObjectURL(urlRef.current);
    },
    [],
  );

  // Playback steps at a frame a second: the agent posts at roughly that rate, so
  // the recording plays at the speed the session ran at.
  useEffect(() => {
    if (!playing) return;
    const timer = window.setInterval(() => {
      setIndex((i) => {
        if (i >= frames.length - 1) {
          setPlaying(false);
          return i;
        }
        return i + 1;
      });
    }, 1000);
    return () => window.clearInterval(timer);
  }, [playing, frames.length]);

  const current = frames[index];
  const eventsHere = current
    ? events.filter((e) => {
        const at = new Date(e.at).getTime();
        const frame = new Date(current.at).getTime();
        const prev = index > 0 ? new Date(frames[index - 1].at).getTime() : frame - 1000;
        return at > prev && at <= frame;
      })
    : [];

  const resumeMarkers = (manifest?.markers ?? []).filter((m) => m.kind === 'resume');
  const capMarkers = (manifest?.markers ?? []).filter((m) => m.kind === 'cap');
  const truncated = (manifest?.markers ?? []).filter((m) => m.kind === 'truncated');

  return (
    <div
      className="fixed inset-0 z-50 flex flex-col"
      style={{ background: 'var(--ink-plate)' }}
      role="dialog"
      aria-label="Session recording playback"
    >
      <header
        className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-2"
        style={{ borderColor: 'rgba(232,228,216,0.18)' }}
      >
        <div className="flex flex-wrap items-center gap-3">
          <span className="ow-display text-[15px]" style={{ color: 'var(--ink-on-plate)' }}>
            Session recording
          </span>
          <span className="cap cap-sm" style={{ color: 'var(--ink-on-plate-muted)' }}>
            {manifest ? `${manifest.recording.device_name || manifest.recording.device_id.slice(0, 8)} · ${manifest.recording.username}` : recordingId.slice(0, 8)}
          </span>
          <Stamp tone="alert">Recorded screen</Stamp>
          {manifest && (
            <Stamp tone={manifest.recording.status === 'recording' ? 'due' : 'neutral'}>
              {manifest.recording.status === 'recording' ? 'Still recording' : manifest.recording.status}
            </Stamp>
          )}
        </div>
        <div className="flex items-center gap-2">
          {manifest && (
            <button
              onClick={() => void downloadRecordingManifest(recordingId)}
              className="btn px-2.5 py-1.5 text-[12px]"
              style={{ color: 'var(--ink-on-plate)', borderColor: 'rgba(232,228,216,0.3)' }}
            >
              <Download className="h-3.5 w-3.5" aria-hidden="true" /> Manifest
            </button>
          )}
          {manifest?.playable && (
            <button
              onClick={() => void downloadRecordingFile(recordingId)}
              className="btn px-2.5 py-1.5 text-[12px]"
              style={{ color: 'var(--ink-on-plate)', borderColor: 'rgba(232,228,216,0.3)' }}
            >
              <Download className="h-3.5 w-3.5" aria-hidden="true" /> Sequence file
            </button>
          )}
          <button
            onClick={onClose}
            className="btn px-2.5 py-1.5 text-[12px] text-[var(--error)]"
            style={{ borderColor: 'rgba(232,228,216,0.3)' }}
          >
            <X className="h-3.5 w-3.5" aria-hidden="true" /> Close
          </button>
        </div>
      </header>

      <div
        className="flex flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2 text-[12px]"
        style={{ color: 'var(--ink-on-plate-muted)', background: 'rgba(0,0,0,0.18)' }}
      >
        <span>{manifest ? clockLabel(manifest.recording.started_at) : '…'} → {manifest?.recording.ended_at ? clockLabel(manifest.recording.ended_at) : 'running'}</span>
        <span>{manifest?.frames?.length ?? 0} frames</span>
        <span>{bytesLabel(manifest?.bytes_on_disk ?? manifest?.recording.bytes ?? 0)}</span>
        <span>opened by {recordingReasonLabel(manifest?.recording.reason ?? '')}</span>
        {manifest?.recording.checksum && <span className="ow-num">sha256 {manifest.recording.checksum.slice(0, 12)}…</span>}
      </div>

      {error && (
        <p role="alert" className="mx-4 mt-2 text-[13px] text-[var(--error)]">
          {error}
        </p>
      )}

      {!playable && !error && manifest && (
        <p className="mx-4 mt-3 text-[13px]" style={{ color: 'var(--ink-on-plate-muted)' }}>
          This recording has no frames to play. Its manifest row is the record: {manifest.recording.frame_count}{' '}
          frames were recorded{manifest.recording.status === 'missing' ? ', and the file is gone' : ''}.
        </p>
      )}

      {(resumeMarkers.length > 0 || capMarkers.length > 0 || truncated.length > 0) && (
        <ul className="mx-4 mt-2 space-y-1 text-[12px]" style={{ color: 'var(--ink-on-plate-muted)' }}>
          {resumeMarkers.map((m, i) => (
            <li key={`resume-${i}`}>
              Writing resumed at {clockLabel(m.at)} after the server stopped — the minutes before it are not recorded.
            </li>
          ))}
          {capMarkers.map((m, i) => (
            <li key={`cap-${i}`}>
              The recording reached its size limit at {clockLabel(m.at)}; {manifest?.recording.dropped_frames ?? 0}{' '}
              later frames are not stored.
            </li>
          ))}
          {truncated.map((m, i) => (
            <li key={`trunc-${i}`}>The file ends mid-record: {m.detail ?? 'the last frame is incomplete'}.</li>
          ))}
          {manifest?.checksum_mismatch && (
            <li style={{ color: 'var(--error)' }}>
              The file on disk is not the file the manifest describes: its checksum differs.
            </li>
          )}
        </ul>
      )}

      <div className="relative flex flex-1 items-center justify-center p-4">
        {frameUrl ? (
          <img
            src={frameUrl}
            alt={`Recording frame ${index + 1} of ${frames.length}`}
            className="max-h-full max-w-full border outline-none"
            style={{ borderColor: 'rgba(232,228,216,0.25)' }}
          />
        ) : (
          <p className="cap cap-lg" style={{ color: 'var(--ink-on-plate-muted)' }}>
            {loadingFrame ? 'Fetching the frame…' : playable ? 'No frame yet' : 'Nothing to play'}
          </p>
        )}
      </div>

      {playable && (
        <footer
          className="flex flex-wrap items-center gap-3 border-t px-4 py-3"
          style={{ borderColor: 'rgba(232,228,216,0.18)' }}
        >
          <button
            onClick={() => setPlaying((p) => !p)}
            className="btn btn-primary px-2.5 py-1.5 text-[12px]"
            aria-label={playing ? 'Pause playback' : 'Start playback'}
          >
            {playing ? <Pause className="h-3.5 w-3.5" aria-hidden="true" /> : <Play className="h-3.5 w-3.5" aria-hidden="true" />}
            {playing ? 'Pause' : 'Play'}
          </button>
          <input
            type="range"
            min={0}
            max={Math.max(frames.length - 1, 0)}
            value={index}
            onChange={(e) => {
              setPlaying(false);
              setIndex(Number(e.target.value));
            }}
            aria-label="Frame"
            className="min-w-40 flex-1"
            style={{ accentColor: 'var(--service)' }}
          />
          <span className="ow-num text-[12px]" style={{ color: 'var(--ink-on-plate)' }}>
            {index + 1}/{frames.length}
          </span>
          <span className="ow-num text-[12px]" style={{ color: 'var(--ink-on-plate-muted)' }}>
            {current ? clockLabel(current.at) : '—'}
          </span>
          <div className="flex min-w-56 flex-wrap items-center gap-2">
            {eventsHere.length === 0 ? (
              <span className="cap cap-sm" style={{ color: 'var(--ink-on-plate-muted)' }}>
                No input at this frame
              </span>
            ) : (
              eventsHere.map((e, i) => (
                <span key={i} className="cap cap-sm flex items-center gap-1" style={{ color: 'var(--ink-on-plate)' }}>
                  <Check className="h-3 w-3" aria-hidden="true" />
                  {clockLabel(e.at)} · {e.input_type || e.action}
                </span>
              ))
            )}
          </div>
        </footer>
      )}
    </div>
  );
}
