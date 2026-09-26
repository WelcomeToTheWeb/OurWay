import type { Device } from '../types/device';

type Status = Device['status'];

const config: Record<Status, { label: string; className: string; dotColor: string }> = {
  online: {
    label: 'Online',
    className: 'bg-status-online/15 text-status-online ring-1 ring-inset ring-status-online/30',
    dotColor: 'bg-status-online',
  },
  offline: {
    label: 'Offline',
    className: 'bg-status-offline/15 text-status-offline ring-1 ring-inset ring-status-offline/30',
    dotColor: 'bg-status-offline',
  },
  alert: {
    label: 'Alert',
    className: 'bg-status-error/15 text-status-error ring-1 ring-inset ring-status-error/30',
    dotColor: 'bg-status-error',
  },
};

export function StatusBadge({ status }: { status: Status }) {
  const { label, className, dotColor } = config[status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${className}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${dotColor} ${status === 'online' ? 'animate-pulse' : ''}`} />
      {label}
    </span>
  );
}
