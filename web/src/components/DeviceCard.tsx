import { Link } from 'react-router-dom';
import { Monitor, Apple, Server, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Device } from '../types/device';
import type { DeviceCompliance, PatchPolicy } from '../types/patch';
import { StatusStamp } from './Stamp';
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
 * The card is a fixed box, and the box is the contract: `Devices.tsx` sizes the
 * virtualised window from this number (rowHeight = DEVICE_CARD_H + 2 × the cell
 * gutter), so a row in the window and a card in the responsive grid are the same
 * height to the pixel. Everything inside is single-line and clipped, and the
 * record line is pinned to the bottom edge with `mt-auto`, so a unit with no
 * tenant and no tags still closes at the same place as one with both.
 */
export const DEVICE_CARD_H = 148;

/** The card's gutter inside a virtualised cell. Devices.tsx adds it to DEVICE_CARD_H. */
export const DEVICE_CARD_GUTTER = 6;

/** The metric line's tone: the patch threshold's own colour, never a decoration. */
const nextToneClass = {
  default: 'text-text-primary',
  due: 'text-due',
  alert: 'text-error',
  ok: 'text-online',
} as const;

/**
 * A device card: the unit's identity, its state, and the three figures a
 * technician reads before opening it — last seen, next due, interval — set as
 * one quiet metric line under a hairline.
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
   * The tenant this unit belongs to (2.3-5). Printed on the card because a
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
      data-device-card={device.id}
      style={{ height: DEVICE_CARD_H }}
      className="decal group flex flex-col overflow-hidden no-underline"
      aria-label={`${device.name} — ${device.status}`}
    >
      <div className="flex items-start gap-2 px-3 pt-2.5">
        <span
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded border border-rule bg-[var(--field)]"
          aria-hidden="true"
        >
          <Icon className="h-4 w-4 text-text-secondary" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[14px] font-semibold leading-tight text-text-primary group-hover:text-service">
            {device.name}
          </span>
          <span className="mt-0.5 block truncate font-mono text-[12px] leading-snug text-text-muted">
            {osLabel(device.os)} · {device.arch} · {device.hostname}
          </span>
          {clientName && (
            <span
              className="cap-sm cap mt-1 block truncate text-service"
              title={clientName}
              aria-label={`Tenant ${clientName}`}
            >
              {clientName}
            </span>
          )}
        </span>
        <span className="flex shrink-0 flex-col items-end gap-1">
          <StatusStamp status={device.status} />
          {device.reboot_pending && (
            <span className="flex items-center gap-1 text-[10px] text-due" title="Reboot pending">
              <RotateCw className="h-3 w-3" aria-hidden="true" />
              <span className="cap-sm cap">Reboot</span>
            </span>
          )}
        </span>
      </div>

      {tags.length > 0 && (
        <div className="flex h-[17px] flex-wrap gap-1 overflow-hidden px-3 pt-1.5">
          {tags.slice(0, 4).map((tag) => (
            <span key={tag} className="cap-sm cap rounded-sm border border-rule px-1.5 text-text-secondary">
              {tag}
            </span>
          ))}
          {tags.length > 4 && <span className="cap-sm cap text-text-muted">+{tags.length - 4}</span>}
        </div>
      )}

      <div className="mt-auto border-t border-rule px-3 pb-2 pt-2">
        <div className="flex items-baseline justify-between gap-3">
          <span className="flex min-w-0 items-baseline gap-1.5">
            <span className="field-label shrink-0">{record.lastLabel}</span>
            <span className="ow-num truncate text-[11.5px] leading-tight text-text-secondary">{record.last}</span>
          </span>
          <span className="flex min-w-0 items-baseline gap-1.5">
            <span className="field-label shrink-0">{record.nextLabel}</span>
            <span
              className={`ow-num truncate text-[11.5px] leading-tight ${nextToneClass[record.nextTone]}`}
            >
              {record.next}
            </span>
          </span>
        </div>
        <div className="mt-1 flex items-baseline gap-1.5">
          <span className="field-label shrink-0">{record.intervalLabel}</span>
          <span className="ow-num truncate text-[11.5px] leading-tight text-text-muted">{record.interval}</span>
        </div>
      </div>
    </Link>
  );
}
