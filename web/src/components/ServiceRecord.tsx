import type { ReactNode } from 'react';

/**
 * The signature move: a serviced unit's record strip — last done, next due,
 * interval — ruled into three fields, with the unit's state stamped at the
 * block's corner. Every device, on every screen, carries the same record.
 */
export function ServiceRecord({
  lastLabel,
  last,
  nextLabel,
  next,
  intervalLabel,
  interval,
  nextTone = 'default',
}: {
  lastLabel: string;
  last: ReactNode;
  nextLabel: string;
  next: ReactNode;
  intervalLabel: string;
  interval: ReactNode;
  nextTone?: 'default' | 'due' | 'alert' | 'ok';
}) {
  const tone =
    nextTone === 'due'
      ? 'text-[var(--due)]'
      : nextTone === 'alert'
        ? 'text-[var(--error)]'
        : nextTone === 'ok'
          ? 'text-[var(--online)]'
          : 'text-text-primary';

  const cells: { label: string; value: ReactNode; cls: string }[] = [
    { label: lastLabel, value: last, cls: 'text-text-primary' },
    { label: nextLabel, value: next, cls: tone },
    { label: intervalLabel, value: interval, cls: 'text-text-secondary' },
  ];

  return (
    <div className="decal-record">
      {cells.map((c) => (
        <div key={c.label}>
          <div className="field-label">{c.label}</div>
          <div className={`ow-num mt-0.5 text-[11.5px] leading-tight ${c.cls}`}>{c.value}</div>
        </div>
      ))}
    </div>
  );
}
