import type { ReactNode } from 'react';
import { MicroField } from './MicroField';
import { onsetIndex } from '../stores/fleetHistory';

interface MetricCardProps {
  label: string;
  value: number;
  unit?: string;
  history?: number[];
  threshold?: number;
  icon?: ReactNode;
}

/**
 * A measured quantity, printed the way the decal sheet prints one: a caption,
 * a large tabular numeral, a ruled threshold line, and the recent past as a
 * micro-field with the onset of the current level marked.
 */
export function MetricCard({ label, value, unit = '%', history, threshold }: MetricCardProps) {
  const limit = threshold ?? 100;
  const ratio = Math.min(value / limit, 1.35);
  const tone = ratio < 0.7 ? 'online' : ratio < 0.9 ? 'due' : 'alert';
  const color = tone === 'online' ? 'var(--online)' : tone === 'due' ? 'var(--due)' : 'var(--error)';
  const widthPct = Math.min(100, (value / limit) * 100);

  return (
    <div className="panel px-3 py-2.5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="field-label">{label}</span>
        <span className="cap-sm cap text-text-muted">
          {threshold != null ? `limit ${limit}${unit}` : `of ${limit}${unit}`}
        </span>
      </div>

      <div className="mt-1 flex items-baseline gap-1.5">
        <span className="ow-num text-[28px] font-bold leading-none" style={{ color }}>
          {Number.isFinite(value) ? value.toFixed(0) : '—'}
        </span>
        <span className="cap cap-sm text-text-muted">{unit}</span>
      </div>

      <div className="relative mt-2 h-[7px] border border-rule bg-[var(--field)]" aria-hidden="true">
        <div
          className="h-full transition-[width] duration-400 ease-[var(--ease-press)]"
          style={{ width: `${widthPct}%`, background: color }}
        />
        {threshold != null && threshold < limit && (
          <span
            className="absolute top-0 h-full"
            style={{ left: `${(threshold / limit) * 100}%`, borderLeft: '1px dashed var(--rule-strong)' }}
          />
        )}
      </div>

      {history && history.length > 1 && (
        <div className="mt-2">
          <MicroField
            values={history}
            max={limit}
            height={20}
            onset={onsetIndex(history)}
            tone={tone === 'online' ? 'online' : tone === 'due' ? 'due' : 'alert'}
            label={`${label}: recent history`}
          />
        </div>
      )}
    </div>
  );
}
