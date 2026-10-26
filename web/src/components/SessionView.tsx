import { useEffect, useRef, useState, useCallback } from 'react';
import { X, MousePointer2, Keyboard, Monitor, Settings } from 'lucide-react';
import { sendInput, endSession, setQuality as setSessionQuality } from '../api/sessions';
import type { Session } from '../api/sessions';
import { useAuth } from '../auth/context';
import { Stamp } from './Stamp';

interface SessionViewProps {
  session: Session;
  onClose: () => void;
}

type SessionMode = 'view' | 'control';

/**
 * The remote screen: an overlay with a plate header, the state stamped, and
 * the control affordances named plainly. Standard controls — buttons, a
 * range slider — nothing theatrical between the technician and the screen.
 */
export function SessionView({ session, onClose }: SessionViewProps) {
  const { accessToken } = useAuth();
  const frameRef = useRef<HTMLImageElement>(null);
  const [status, setStatus] = useState<'connecting' | 'active' | 'ended'>('connecting');
  const [mode, setMode] = useState<SessionMode>('view');
  const [quality, setQualityValue] = useState(80);
  const [showSettings, setShowSettings] = useState(false);
  const [frame, setFrame] = useState<string | null>(null);

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

  return (
    <div className="fixed inset-0 z-50 flex flex-col" style={{ background: 'var(--ink-plate)' }} role="dialog" aria-label="Remote session">
      <header
        className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-2"
        style={{ borderColor: 'rgba(232,228,216,0.18)' }}
      >
        <div className="flex items-center gap-3">
          <Monitor className="h-4 w-4" style={{ color: 'var(--ink-on-plate)' }} aria-hidden="true" />
          <span className="ow-display text-[15px]" style={{ color: 'var(--ink-on-plate)' }}>
            Remote session
          </span>
          <span className="cap-sm cap" style={{ color: 'var(--ink-on-plate-muted)' }}>
            {session.id.slice(0, 8)}
          </span>
          <Stamp tone={status === 'active' ? 'online' : status === 'connecting' ? 'due' : 'alert'}>
            {status === 'active' ? 'Connected' : status === 'connecting' ? 'Connecting' : 'Ended'}
          </Stamp>
          <Stamp tone={mode === 'control' ? 'service' : 'neutral'}>{mode === 'control' ? 'Controlling' : 'Watching'}</Stamp>
        </div>
        <div className="flex items-center gap-2">
          {mode === 'view' ? (
            <button onClick={() => setMode('control')} className="btn btn-primary px-2.5 py-1.5 text-[12px]">
              <MousePointer2 className="h-3.5 w-3.5" aria-hidden="true" /> Take control
            </button>
          ) : (
            <button onClick={() => setMode('view')} className="btn px-2.5 py-1.5 text-[12px]">
              Release control
            </button>
          )}
          <button
            onClick={() => setShowSettings((s) => !s)}
            aria-expanded={showSettings}
            className="btn px-2.5 py-1.5 text-[12px]"
          >
            <Settings className="h-3.5 w-3.5" aria-hidden="true" /> Quality
          </button>
          <button onClick={handleClose} className="btn px-2.5 py-1.5 text-[12px] text-[var(--error)]">
            <X className="h-3.5 w-3.5" aria-hidden="true" /> End session
          </button>
        </div>
      </header>

      {showSettings && (
        <div className="mx-4 mt-2 flex items-center gap-3 border border-rule px-3 py-2" style={{ background: 'var(--bg-card)' }}>
          <label className="cap-sm cap text-text-muted" htmlFor="session-quality">
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
            className="flex-1"
            style={{ accentColor: 'var(--service)' }}
          />
          <span className="ow-num w-10 text-right text-[12px] text-text-primary">{quality}%</span>
        </div>
      )}

      <div className="relative flex flex-1 items-center justify-center p-4">
        {frame ? (
          <img
            ref={frameRef}
            src={frame}
            alt="Remote session"
            tabIndex={mode === 'control' ? 0 : -1}
            className="max-h-full max-w-full border outline-none"
            style={{ borderColor: 'rgba(232,228,216,0.25)' }}
            onMouseMove={handleMouseMove}
            onMouseDown={handleClick}
            onWheel={handleScroll}
            onKeyDown={handleKeyDown}
            onKeyUp={handleKeyUp}
            onContextMenu={(e) => e.preventDefault()}
          />
        ) : (
          <p className="cap cap-lg" style={{ color: 'var(--ink-on-plate-muted)' }}>
            {status === 'connecting' ? 'Connecting to the unit…' : 'Waiting for the first frame…'}
          </p>
        )}
      </div>

      {mode === 'control' && (
        <footer
          className="flex flex-wrap items-center gap-4 border-t px-4 py-2 text-[11px]"
          style={{ borderColor: 'rgba(232,228,216,0.18)', color: 'var(--ink-on-plate-muted)' }}
        >
          <span className="flex items-center gap-1.5">
            <MousePointer2 className="h-3 w-3" aria-hidden="true" /> Pointer moves the unit's cursor
          </span>
          <span className="flex items-center gap-1.5">
            <Keyboard className="h-3 w-3" aria-hidden="true" /> Click the screen, then type
          </span>
          <span>Right-click opens the context menu</span>
        </footer>
      )}
    </div>
  );
}
