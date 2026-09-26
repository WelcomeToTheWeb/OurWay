import { useEffect, useRef, useState, useCallback } from 'react';
import { X, MousePointer2, Keyboard, Monitor, Settings } from 'lucide-react';
import { submitAnswer, addICECandidate, sendInput, endSession } from '../api/sessions';
import type { Session } from '../api/sessions';

interface SessionViewProps {
  session: Session;
  offer: string;
  onClose: () => void;
}

type SessionMode = 'view' | 'control';

export function SessionView({ session, offer, onClose }: SessionViewProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dataChRef = useRef<RTCDataChannel | null>(null);
  const [status, setStatus] = useState<'connecting' | 'active' | 'ended'>('connecting');
  const [mode, setMode] = useState<SessionMode>('view');
  const [quality, setQuality] = useState(80);
  const [showSettings, setShowSettings] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Parse the offer and set up WebRTC connection
  useEffect(() => {
    let cancelled = false;

    const setupConnection = async () => {
      try {
        // Create RTCPeerConnection
        const pc = new RTCPeerConnection({
          iceServers: [
            { urls: 'stun:stun.l.google.com:19302' },
          ],
        });
        pcRef.current = pc;

        // Handle incoming tracks (video stream from device)
        pc.ontrack = (event) => {
          if (videoRef.current) {
            videoRef.current.srcObject = event.streams[0];
          }
        };

        // Handle ICE candidates
        pc.onicecandidate = (event) => {
          if (event.candidate) {
            addICECandidate(session.id, JSON.stringify(event.candidate)).catch(console.error);
          }
        };

        // Handle connection state
        pc.onconnectionstatechange = () => {
          if (cancelled) return;
          switch (pc.connectionState) {
            case 'connected':
              setStatus('active');
              break;
            case 'failed':
            case 'disconnected':
              setStatus('ended');
              setError('Connection lost');
              break;
            case 'closed':
              setStatus('ended');
              break;
          }
        };

        // Create data channel for input events
        const dataCh = pc.createDataChannel('ourway-session', { ordered: true });
        dataChRef.current = dataCh;

        dataCh.onopen = () => {
          setStatus('active');
        };

        dataCh.onmessage = (event) => {
          // Handle screen frames from device
          if (event.data instanceof ArrayBuffer) {
            // Process binary frame data (JPEG)
            const blob = new Blob([event.data], { type: 'image/jpeg' });
            const url = URL.createObjectURL(blob);
            const img = new Image();
            img.onload = () => {
              const canvas = canvasRef.current;
              if (canvas) {
                canvas.width = img.width;
                canvas.height = img.height;
                const ctx = canvas.getContext('2d');
                if (ctx) {
                  ctx.drawImage(img, 0, 0);
                }
              }
              URL.revokeObjectURL(url);
            };
            img.src = url;
          } else {
            // Handle JSON messages
            try {
              const msg = JSON.parse(event.data);
              if (msg.type === 'session_end') {
                setStatus('ended');
              }
            } catch {
              // Ignore parse errors
            }
          }
        };

        // Set the remote offer
        const offerDesc = JSON.parse(offer);
        await pc.setRemoteDescription(offerDesc);

        // Create and send answer
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        await submitAnswer(session.id, JSON.stringify(answer));

      } catch (err) {
        if (!cancelled) {
          setError(`Failed to connect: ${(err as Error).message}`);
          setStatus('ended');
        }
      }
    };

    setupConnection();

    return () => {
      cancelled = true;
      if (pcRef.current) {
        pcRef.current.close();
      }
      if (dataChRef.current) {
        dataChRef.current.close();
      }
    };
  }, [session.id, offer]);

  // Send mouse events
  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    if (mode !== 'control' || status !== 'active' || !canvasRef.current) return;

    const rect = canvasRef.current.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * 100;
    const y = ((e.clientY - rect.top) / rect.height) * 100;

    sendInput(session.id, 'mouse', { event: 'move', x, y }).catch(console.error);
  }, [mode, status, session.id]);

  const handleClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    if (mode !== 'control' || status !== 'active' || !canvasRef.current) return;

    const rect = canvasRef.current.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * 100;
    const y = ((e.clientY - rect.top) / rect.height) * 100;
    const button = e.button === 2 ? 'right' : 'left';

    sendInput(session.id, 'mouse', { event: 'click', x, y, button }).catch(console.error);
  }, [mode, status, session.id]);

  const handleScroll = useCallback((e: React.WheelEvent<HTMLCanvasElement>) => {
    if (mode !== 'control' || status !== 'active') return;
    e.preventDefault();
    sendInput(session.id, 'mouse', { event: 'scroll', delta: e.deltaY }).catch(console.error);
  }, [mode, status, session.id]);

  // Send keyboard events
  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (mode !== 'control' || status !== 'active') return;
    sendInput(session.id, 'key', { event: 'down', key: e.key, code: e.code }).catch(console.error);
  }, [mode, status, session.id]);

  const handleKeyUp = useCallback((e: React.KeyboardEvent) => {
    if (mode !== 'control' || status !== 'active') return;
    sendInput(session.id, 'key', { event: 'up', key: e.key, code: e.code }).catch(console.error);
  }, [mode, status, session.id]);

  // Close session
  const handleClose = async () => {
    try {
      await endSession(session.id);
    } catch {
      // Ignore
    }
    onClose();
  };

  // Focus capture for keyboard events
  useEffect(() => {
    if (mode === 'control' && status === 'active') {
      canvasRef.current?.focus();
    }
  }, [mode, status]);

  return (
    <div className="fixed inset-0 bg-black/90 z-50 flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-900 border-b border-gray-700">
        <div className="flex items-center gap-3">
          <Monitor size={18} className="text-blue-400" />
          <span className="text-text-primary font-medium">Remote Session</span>
          <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${
            status === 'active' ? 'bg-green-500/20 text-green-400' :
            status === 'connecting' ? 'bg-yellow-500/20 text-yellow-400' :
            'bg-red-500/20 text-red-400'
          }`}>
            {status === 'active' ? 'Connected' : status === 'connecting' ? 'Connecting...' : 'Ended'}
          </span>
        </div>
        <div className="flex items-center gap-2">
          {mode === 'view' && (
            <button
              onClick={() => setMode('control')}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded text-sm text-text-primary"
            >
              <MousePointer2 size={14} /> Take Control
            </button>
          )}
          {mode === 'control' && (
            <button
              onClick={() => setMode('view')}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-600 hover:bg-gray-700 rounded text-sm text-text-primary"
            >
              Release Control
            </button>
          )}
          <button
            onClick={() => setShowSettings(!showSettings)}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-sm text-text-primary"
          >
            <Settings size={14} /> Quality
          </button>
          <button
            onClick={handleClose}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-700 rounded text-sm text-text-primary"
          >
            <X size={14} /> End
          </button>
        </div>
      </div>

      {/* Quality settings */}
      {showSettings && (
        <div className="mx-4 mt-2 bg-gray-800 rounded p-3 flex items-center gap-4">
          <span className="text-gray-300 text-sm">Quality:</span>
          <input
            type="range"
            min={1}
            max={100}
            value={quality}
            onChange={(e) => setQuality(Number(e.target.value))}
            className="flex-1"
          />
          <span className="text-gray-300 text-sm w-8">{quality}%</span>
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="mx-4 mt-2 bg-red-900/50 border border-red-700 rounded p-2 text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Main content */}
      <div className="flex-1 p-4 flex items-center justify-center relative">
        <canvas
          ref={canvasRef}
          tabIndex={mode === 'control' ? 0 : -1}
          className="max-w-full max-h-full rounded shadow-lg cursor-crosshair outline-none"
          onMouseMove={handleMouseMove}
          onMouseDown={handleClick}
          onWheel={handleScroll}
          onKeyDown={handleKeyDown}
          onKeyUp={handleKeyUp}
          onContextMenu={(e) => e.preventDefault()}
        />
        {status === 'connecting' && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/50 rounded">
            <div className="text-text-primary text-lg">Connecting to device...</div>
          </div>
        )}
      </div>

      {/* Footer */}
      {mode === 'control' && (
        <div className="px-4 py-2 bg-gray-900 border-t border-gray-700 flex items-center gap-4 text-xs text-gray-400">
          <span className="flex items-center gap-1"><MousePointer2 size={12} /> Move to control mouse</span>
          <span className="flex items-center gap-1"><Keyboard size={12} /> Click screen, then type</span>
          <span>Right-click for context menu</span>
        </div>
      )}
    </div>
  );
}
