import type { ReactNode } from 'react';
import type { Device } from '../types/device';

/**
 * A stamp: the world's state vocabulary. A state is printed — outlined,
 * uppercase, letterpress — never a coloured dot on a tinted pill.
 */
export type StampTone =
  | 'online'
  | 'offline'
  | 'alert'
  | 'due'
  | 'service'
  | 'stamp'
  | 'neutral';

const toneClass: Record<StampTone, string> = {
  online: 'text-[var(--online)]',
  offline: 'text-text-muted',
  alert: 'text-[var(--error)]',
  due: 'text-[var(--due)]',
  service: 'text-[var(--service)]',
  stamp: 'text-[var(--stamp)]',
  neutral: 'text-text-secondary',
};

export function Stamp({
  tone = 'neutral',
  children,
  className = '',
  settle = false,
  title,
}: {
  tone?: StampTone;
  children: ReactNode;
  className?: string;
  settle?: boolean;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={`stamp ${toneClass[tone]} ${settle ? 'ow-stamped' : ''} ${className}`}
    >
      {children}
    </span>
  );
}

const statusCopy: Record<Device['status'], { label: string; tone: StampTone; meaning: string }> = {
  online: { label: 'Online', tone: 'online', meaning: 'Reporting on schedule' },
  offline: { label: 'Offline', tone: 'offline', meaning: 'No heartbeat received' },
  alert: { label: 'Alert', tone: 'alert', meaning: 'A threshold is breached' },
};

/** Device presence, printed. */
export function StatusStamp({ status }: { status: Device['status'] }) {
  const { label, tone, meaning } = statusCopy[status] ?? statusCopy.offline;
  return (
    <Stamp tone={tone} title={meaning}>
      {label}
    </Stamp>
  );
}

/**
 * The live-link indicator for the shift bar: a stamp, with the link's
 * meaning in its title.
 */
export function LinkStamp({ connected }: { connected: boolean }) {
  return (
    <span className="flex items-center gap-2">
      <span className="cap-sm cap text-text-muted">Link</span>
      <Stamp tone={connected ? 'online' : 'offline'} title={connected ? 'WebSocket connected' : 'WebSocket down — figures are from the last refresh'}>
        {connected ? 'Live' : 'Stale'}
      </Stamp>
    </span>
  );
}
