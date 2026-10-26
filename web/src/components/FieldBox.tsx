import type { ReactNode } from 'react';
import { MicroField } from './MicroField';
import { onsetIndex } from '../stores/fleetHistory';

/**
 * A ruled field box: caption above, measured value below, and the recent
 * past as a micro-field. This is the world's atom — the same box the service
 * decal's LAST / NEXT / INTERVAL record is built from.
 */
export function FieldBox({
  label,
  value,
  hint,
  tone = 'default',
  right,
  history,
  historyMax,
  className = '',
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  tone?: 'default' | 'alert' | 'due' | 'ok' | 'service';
  right?: ReactNode;
  history?: number[];
  historyMax?: number;
  className?: string;
}) {
  const valueTone =
    tone === 'alert'
      ? 'text-[var(--error)]'
      : tone === 'due'
        ? 'text-[var(--due)]'
        : tone === 'ok'
          ? 'text-[var(--online)]'
          : tone === 'service'
            ? 'text-[var(--service)]'
            : 'text-text-primary';

  const historyTone =
    tone === 'alert' ? 'alert' : tone === 'due' ? 'due' : tone === 'ok' ? 'online' : 'service';

  return (
    <div className={`field ${className}`}>
      <div className="flex items-baseline justify-between gap-3">
        <span className="field-label">{label}</span>
        {right}
      </div>
      <div className={`mt-0.5 text-[15px] font-semibold leading-tight ${valueTone}`}>{value}</div>
      {hint && <div className="mt-0.5 text-[11px] leading-snug text-text-secondary">{hint}</div>}
      {history && history.length > 1 && (
        <div className="mt-2">
          <MicroField
            values={history}
            max={historyMax}
            height={20}
            tone={historyTone}
            onset={onsetIndex(history)}
            label={`${label}: recent history`}
          />
        </div>
      )}
    </div>
  );
}

/** A caption over a hairline — the way a decal sheet names a band. */
export function BandHead({
  children,
  right,
  className = '',
}: {
  children: ReactNode;
  right?: ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex items-baseline justify-between gap-4 ${className}`}>
      <h2 className="cap cap-lg text-text-muted">{children}</h2>
      {right && <div className="flex items-center gap-2">{right}</div>}
    </div>
  );
}
