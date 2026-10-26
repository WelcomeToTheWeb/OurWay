import { StatusStamp } from './Stamp';

/**
 * Kept as a name (many pages import it); implemented as the world's state
 * stamp — printed, not tinted.
 */
export function StatusBadge({ status }: { status: 'online' | 'offline' | 'alert' }) {
  return <StatusStamp status={status} />;
}
