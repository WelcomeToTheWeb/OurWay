import { Link } from 'react-router-dom';
import { Monitor, Apple, Server } from 'lucide-react';
import type { Device } from '../types/device';
import { StatusBadge } from './StatusBadge';

const osIcons: Record<string, typeof Monitor> = {
  linux: Server,
  windows: Monitor,
  darwin: Apple,
};

function osLabel(os: string): string {
  switch (os) {
    case 'linux':
      return 'Linux';
    case 'windows':
      return 'Windows';
    case 'darwin':
      return 'macOS';
    default:
      return os;
  }
}

function lastSeenLabel(lastSeen: string): string {
  try {
    const d = new Date(lastSeen);
    if (isNaN(d.getTime())) return 'Unknown';
    const diff = Date.now() - d.getTime();
    if (diff < 60000) return 'Just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    return d.toLocaleDateString();
  } catch {
    return 'Unknown';
  }
}

export function DeviceCard({ device }: { device: Device }) {
  const Icon = osIcons[device.os] || Monitor;

  return (
    <Link
      to={`/devices/${device.id}`}
      className="group flex h-full flex-col gap-4 rounded-xl border border-bg-border bg-bg-card p-4 transition-colors hover:border-accent"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-bg">
            <Icon className="h-5 w-5 text-accent" />
          </div>
          <div>
            <p className="text-sm font-medium text-text-primary group-hover:text-accent">
              {device.name}
            </p>
            <p className="text-xs text-text-secondary">{device.hostname}</p>
          </div>
        </div>
        <StatusBadge status={device.status} />
      </div>

      <div className="flex items-center justify-between text-xs">
        <span className="text-text-secondary">{osLabel(device.os)}</span>
        <span className="text-text-muted">
          Last seen: {lastSeenLabel(device.last_seen)}
        </span>
      </div>
    </Link>
  );
}
