import { Link } from 'react-router-dom';
import { Monitor, Apple, Server, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Device } from '../types/device';
import type { DeviceCompliance, PatchPolicy } from '../types/patch';
import { StatusStamp } from './Stamp';
import { ServiceRecord } from './ServiceRecord';
import { deviceRecord, policyForDevice } from '../utils/policyScope';

const osIcons: Record<string, typeof Monitor> = {
  linux: Server,
  windows: Monitor,
  darwin: Apple,
};

export function osLabel(os: string): string {
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

/**
 * A device decal: the unit's identity, its state stamp, and its service
 * record — last seen, next due, interval — printed in one ruled block.
 */
export function DeviceCard({
  device,
  compliance,
  policies = [],
  clientName,
}: {
  device: Device;
  compliance?: DeviceCompliance;
  policies?: PatchPolicy[];
  /**
   * The tenant this unit belongs to (2.3-5). Printed on the decal because a
   * technician working two tenants must know which fleet a card is in before
   * they click it; the fleet is filtered server-side, so the card is the last
   * place the answer can be seen.
   */
  clientName?: string;
}) {
  const Icon = osIcons[device.os] || Monitor;
  const { t } = useTranslation();
  const record = deviceRecord(device, compliance, policyForDevice(policies, device), {
    lastSeen: t('record.lastSeen'),
    nextDue: t('record.nextDue'),
    interval: t('record.interval'),
    noPolicy: t('record.noPolicy'),
  });
  const tags = device.tags ?? [];

  return (
    <Link
      to={`/devices/${device.id}`}
      className="decal group flex h-full flex-col no-underline"
      aria-label={`${device.name} — ${device.status}`}
    >
      <div className="flex items-start justify-between gap-2 px-3 pt-3">
        <div className="flex min-w-0 items-start gap-2.5">
          <span
            className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center border border-rule"
            aria-hidden="true"
          >
            <Icon className="h-4 w-4 text-text-secondary" />
          </span>
          <span className="min-w-0">
            <span className="block truncate text-[14px] font-semibold text-text-primary group-hover:text-[var(--service)]">
              {device.name}
            </span>
            <span className="cap-sm cap block truncate text-text-muted">
              {osLabel(device.os)} · {device.arch} · {device.hostname}
            </span>
            {clientName && (
              <span
                className="cap-sm cap block truncate text-[var(--service)]"
                title={clientName}
                aria-label={`Tenant ${clientName}`}
              >
                {clientName}
              </span>
            )}
          </span>
        </div>
        <span className="flex shrink-0 flex-col items-end gap-1">
          <StatusStamp status={device.status} />
          {device.reboot_pending && (
            <span className="flex items-center gap-1 text-[10px] text-[var(--due)]" title="Reboot pending">
              <RotateCw className="h-3 w-3" aria-hidden="true" />
              <span className="cap-sm cap">Reboot</span>
            </span>
          )}
        </span>
      </div>

      {tags.length > 0 && (
        <div className="flex flex-wrap gap-1 px-3 pt-2">
          {tags.slice(0, 4).map((tag) => (
            <span key={tag} className="cap-sm cap border border-rule px-1.5 py-0.5 text-text-secondary">
              {tag}
            </span>
          ))}
          {tags.length > 4 && <span className="cap-sm cap text-text-muted">+{tags.length - 4}</span>}
        </div>
      )}

      <div className="mt-2 flex-1" />
      <ServiceRecord {...record} />
    </Link>
  );
}
