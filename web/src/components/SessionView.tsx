import { useEffect, useRef, useState, useCallback } from 'react';
import { X, MousePointer2, Keyboard, Monitor, Settings } from 'lucide-react';
import { sendInput, endSession, setQuality as setSessionQuality } from '../api/sessions';
import type { Session } from '../api/sessions';
import { startRecording } from '../api/recordings';
import { invalidateFleet } from '../stores/fleet';
import { useFleet } from '../hooks/useFleet';
import { useAuth } from '../auth/context';
import { Stamp, type StampTone } from './Stamp';
import { useTranslation } from 'react-i18next';

interface SessionViewProps {
  session: Session;
  onClose: () => void;
  /** What the unit reported about its own input capability (2.2-1). Control
      mode on a unit that cannot synthesize input must say so on the screen
      where the technician is about to type, not only on the device view. */
  inputCapability?: 'granted' | 'denied' | 'unavailable' | 'unknown';
}

type SessionMode = 'view' | 'control';

/* --- plate chrome ------------------------------------------------------- */
/* The session surface is one slate plate in both renditions, so it carries the
   rail's own ink, rules and raised fill. The state tokens are tuned for the
   light canvas; on the plate each is composited toward `--rail-text` so a 12px
   semibold caption still clears 4.5:1 in the day rendition. Token-only: every
   alpha here is a mix of two tokens, never a literal colour. */
const PLATE = {
  text: 'var(--rail-text)',
  muted: 'var(--rail-text-muted)',
  line: 'var(--rail-line)',
  raised: 'var(--rail-bg-raised)',
  danger: 'color-mix(in srgb, var(--color-danger) 72%, var(--rail-text))',
  warning: 'color-mix(in srgb, var(--warning) 78%, var(--rail-text))',
  dangerFill: 'color-mix(in srgb, var(--color-danger) 12%, var(--rail-bg))',
  dangerLine: 'color-mix(in srgb, var(--color-danger) 32%, var(--rail-line))',
} as const;

/** A `.stamp` tone lifted onto the plate. Static strings: Tailwind reads them
    from source, so they must not be built at runtime. */
const PLATE_STAMP: Record<StampTone, string> = {
  online: '!text-[color:color-mix(in_srgb,var(--online)_78%,var(--rail-text))]',
  offline: '!text-[color:var(--rail-text-muted)]',
  alert: '!text-[color:color-mix(in_srgb,var(--color-danger)_72%,var(--rail-text))]',
  due: '!text-[color:color-mix(in_srgb,var(--due)_78%,var(--rail-text))]',
  service: '!text-[color:color-mix(in_srgb,var(--service)_78%,var(--rail-text))]',
  stamp: '!text-[color:color-mix(in_srgb,var(--stamp)_72%,var(--rail-text))]',
  neutral: '!text-[color:var(--rail-text-muted)]',
};

/** Control geometry on the plate: 32px, `rounded`, one 150ms press curve. */
const BTN = 'btn min-h-8 rounded px-3 text-[12.5px] transition-colors duration-150 ease-press';
/** The same control painted for the plate. `.btn` lives in the components
    layer, so these utilities win without `!important`. */
const BTN_PLATE =
  'border-[color:var(--rail-line)] bg-[color:var(--rail-bg-raised)] text-[color:var(--rail-text)] ' +
  'hover:border-[color:color-mix(in_srgb,var(--rail-text)_26%,transparent)] ' +
  'hover:bg-[color:color-mix(in_srgb,var(--rail-text)_10%,transparent)]';

/**
 * The remote screen: an overlay with a plate header, the state stamped, and
 * the control affordances named plainly. Standard controls — buttons, a
 * range slider — nothing theatrical between the technician and the screen.
 */
export function SessionView({ session, onClose, inputCapability }: SessionViewProps) {
  const { accessToken } = useAuth();
  const { t } = useTranslation();
  const frameRef = useRef<HTMLImageElement>(null);
  const [status, setStatus] = useState<'connecting' | 'active' | 'ended'>('connecting');
  const [mode, setMode] = useState<SessionMode>('view');
  const [quality, setQualityValue] = useState(80);
  const [showSettings, setShowSettings] = useState(false);
  const [frame, setFrame] = useState<string | null>(null);
  const [recordingError, setRecordingError] = useState('');
  // 2.4-8: the installation's recording promise is stated on the screen where the
  // technician is about to type, not only on a ledger page. The recording ledger is
  // a shared collection, so this panel adds no read of its own.
  const { data: recordingIndex } = useFleet('recordings');
  const recording = recordingIndex.recordings.find((r) => r.session_id === session.id);
  const policy = recordingIndex.recording_policy;

  const handleRecord = () => {
    setRecordingError('');
    startRecording(session.id)
      .then(() => invalidateFleet('recordings'))
      .catch((e: unknown) => setRecordingError(e instanceof Error ? e.message : 'The recording could not be started'));
  };

  // Receive screen frames over the reliable user WebSocket: the server
  // relays agent frames as "session_frame" events (base64 JPEG). The
  // WebRTC data-channel path was removed (H4); WS is the only video
  // transport.
  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/ws`;
    const ws = new WebSocket(url, ['ourway-auth', accessToken]);

    ws.onmessage = (event) => {
      if (cancelled) return;
      try {
        const raw = JSON.parse(event.data) as { type: string; payload?: { session_id?: string; data?: string } };
        if (raw.type === 'session_frame' && raw.payload?.session_id === session.id && raw.payload?.data) {
          setFrame(`data:image/jpeg;base64,${raw.payload.data}`);
          setStatus('active');
        }
      } catch {
        // Ignore malformed messages
      }
    };

    return () => {
      cancelled = true;
      ws.close();
    };
  }, [accessToken, session.id]);

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLImageElement>) => {
      if (mode !== 'control' || status !== 'active' || !frameRef.current) return;
      const rect = frameRef.current.getBoundingClientRect();
      const x = ((e.clientX - rect.left) / rect.width) * 100;
      const y = ((e.clientY - rect.top) / rect.height) * 100;
      sendInput(session.id, 'mouse', { event: 'move', x, y }).catch(console.error);
    },
    [mode, status, session.id],
  );

  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLImageElement>) => {
      if (mode !== 'control' || status !== 'active' || !frameRef.current) return;
      const rect = frameRef.current.getBoundingClientRect();
      const x = ((e.clientX - rect.left) / rect.width) * 100;
      const y = ((e.clientY - rect.top) / rect.height) * 100;
      const button = e.button === 2 ? 'right' : 'left';
      sendInput(session.id, 'mouse', { event: 'click', x, y, button }).catch(console.error);
    },
    [mode, status, session.id],
  );

  const handleScroll = useCallback(
    (e: React.WheelEvent<HTMLImageElement>) => {
      if (mode !== 'control' || status !== 'active') return;
      e.preventDefault();
      sendInput(session.id, 'mouse', { event: 'scroll', delta: e.deltaY }).catch(console.error);
    },
    [mode, status, session.id],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (mode !== 'control' || status !== 'active') return;
      sendInput(session.id, 'key', { event: 'down', key: e.key, code: e.code }).catch(console.error);
    },
    [mode, status, session.id],
  );

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent) => {
      if (mode !== 'control' || status !== 'active') return;
      sendInput(session.id, 'key', { event: 'up', key: e.key, code: e.code }).catch(console.error);
    },
    [mode, status, session.id],
  );

  const handleClose = async () => {
    try {
      await endSession(session.id);
    } catch {
      // Ignore
    }
    onClose();
  };

  useEffect(() => {
    if (mode === 'control' && status === 'active') {
      frameRef.current?.focus();
    }
  }, [mode, status]);

  const statusTone: StampTone =
    status === 'active' ? 'online' : status === 'connecting' ? 'due' : 'alert';
  const modeTone: StampTone = mode === 'control' ? 'service' : 'neutral';

  return (
    <div className="plate fixed inset-0 z-50 flex flex-col" role="dialog" aria-label="Remote session">
      <header
        className="flex min-h-12 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b px-4 py-2"
        style={{ borderColor: PLATE.line, background: PLATE.raised }}
      >
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <Monitor className="h-4 w-4 shrink-0" style={{ color: PLATE.muted }} aria-hidden="true" />
          <span className="ow-display text-[15px]" style={{ color: PLATE.text }}>
            Remote session
          </span>
          <span className="cap cap-sm ow-num" style={{ color: PLATE.muted }}>
            {session.id.slice(0, 8)}
          </span>
          <Stamp tone={statusTone} className={PLATE_STAMP[statusTone]}>
            {status === 'active' ? 'Connected' : status === 'connecting' ? 'Connecting' : 'Ended'}
          </Stamp>
          <Stamp tone={modeTone} className={PLATE_STAMP[modeTone]}>{mode === 'control' ? 'Controlling' : 'Watching'}</Stamp>
          {recording ? (
            <Stamp tone="alert" className={PLATE_STAMP.alert}>
              {recording.status === 'recording'
                ? `Recording · ${recording.frame_count} frames`
                : 'Recorded'}
            </Stamp>
          ) : (
            <Stamp tone="neutral" className={PLATE_STAMP.neutral}>Not recorded</Stamp>
          )}
        </div>
        <div className="flex items-center gap-2">
          {mode === 'view' ? (
            <button onClick={() => setMode('control')} className={BTN}>
              <MousePointer2 className="h-3.5 w-3.5" aria-hidden="true" /> Take control
            </button>
          ) : (
            <button onClick={() => setMode('view')} className={`${BTN} ${BTN_PLATE}`}>
              Release control
            </button>
          )}
          {!recording && policy !== 'off' && (
            <button
              onClick={handleRecord}
              className={`${BTN} ${BTN_PLATE}`}
              title="Start recording this session. Nothing in the product stops a recording once it has started."
            >
              Record session
            </button>
          )}
          <button
            onClick={() => setShowSettings((s) => !s)}
            aria-expanded={showSettings}
            className={`${BTN} ${BTN_PLATE}`}
          >
            <Settings className="h-3.5 w-3.5" aria-hidden="true" /> Quality
          </button>
          <button
            onClick={handleClose}
            className={`${BTN} ${BTN_PLATE}`}
            style={{ color: PLATE.danger }}
          >
            <X className="h-3.5 w-3.5" aria-hidden="true" /> End session
          </button>
        </div>
      </header>

      {recordingError && (
        <p
          role="alert"
          className="mx-4 mt-2 flex items-center gap-2 rounded border px-3 py-2 text-[12.5px]"
          style={{ color: PLATE.danger, background: PLATE.dangerFill, borderColor: PLATE.dangerLine }}
        >
          {recordingError}
        </p>
      )}

      {showSettings && (
        <div
          className="mx-4 mt-2 flex flex-wrap items-center gap-x-3 gap-y-2 rounded border px-3 py-2"
          style={{ background: PLATE.raised, borderColor: PLATE.line }}
        >
          <label className="cap cap-sm" htmlFor="session-quality" style={{ color: PLATE.muted }}>
            Frame quality
          </label>
          <input
            id="session-quality"
            type="range"
            min={1}
            max={100}
            value={quality}
            onChange={(e) => {
              const q = Number(e.target.value);
              setQualityValue(q);
              // Tell the server, which forwards the quality to the agent's
              // capture loop (JPEG quality).
              setSessionQuality(session.id, q).catch(console.error);
            }}
            className="h-8 min-w-40 flex-1"
            style={{ accentColor: 'var(--service)' }}
          />
          <span className="ow-num w-10 text-right text-[12.5px]" style={{ color: PLATE.text }}>{quality}%</span>
        </div>
      )}

      <div className="relative flex flex-1 items-center justify-center p-4">
        {frame ? (
          <img
            ref={frameRef}
            src={frame}
            alt="Remote session"
            tabIndex={mode === 'control' ? 0 : -1}
            className="max-h-full max-w-full rounded-sm border shadow-elev2"
            style={{ borderColor: mode === 'control' ? 'var(--service)' : PLATE.line }}
            onMouseMove={handleMouseMove}
            onMouseDown={handleClick}
            onWheel={handleScroll}
            onKeyDown={handleKeyDown}
            onKeyUp={handleKeyUp}
            onContextMenu={(e) => e.preventDefault()}
          />
        ) : (
          <p className="cap cap-lg" style={{ color: PLATE.muted }}>
            {status === 'connecting' ? 'Connecting to the unit…' : 'Waiting for the first frame…'}
          </p>
        )}
      </div>

      {mode === 'control' && (
        <footer
          className="flex min-h-10 flex-wrap items-center gap-x-5 gap-y-2 border-t px-4 py-2 text-[11.5px]"
          style={{ borderColor: PLATE.line, color: PLATE.muted, background: PLATE.raised }}
        >
          <span className="flex items-center gap-1.5">
            <MousePointer2 className="h-3 w-3" aria-hidden="true" /> Pointer moves the unit's cursor
          </span>
          <span className="flex items-center gap-1.5">
            <Keyboard className="h-3 w-3" aria-hidden="true" /> Click the screen, then type
          </span>
          <span>Right-click opens the context menu</span>
          {/* The unit's own report, stated where the technician is about to
              type: on macOS without the Accessibility grant, every keystroke
              is discarded and the session looks fully live. Printed as a stamp
              in the plate-lifted state colour, so it holds on the slate. */}
          {inputCapability && inputCapability !== 'granted' && (
            <span
              className="stamp ow-rise"
              role="status"
              style={{ color: inputCapability === 'denied' ? PLATE.danger : PLATE.warning }}
            >
              {t(`devices.input${inputCapability === 'unknown' ? 'Unknown' : inputCapability === 'denied' ? 'Denied' : 'Unavailable'}`)}
              {inputCapability === 'denied'
                ? '— macOS is discarding input: grant Accessibility on the unit'
                : inputCapability === 'unknown'
                  ? '— this unit never reported its input capability'
                  : '— this agent build cannot synthesize input'}
            </span>
          )}
        </footer>
      )}
    </div>
  );
}
