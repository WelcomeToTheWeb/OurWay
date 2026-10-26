import { MicroField } from './MicroField';

interface GaugeProps {
  value: number;
  label: string;
  unit?: string;
  size?: number;
  sublabel?: string;
  history?: number[];
}

/**
 * A ruled measure, not a progress ring: 0 to 100 with quarter ticks, the
 * level filled in the state ink, the numeral on the measure.
 */
export function Gauge({ value, label, unit = '%', sublabel, history }: GaugeProps) {
  const clamped = Math.min(100, Math.max(0, value));
  const tone = clamped < 70 ? 'online' : clamped < 90 ? 'due' : 'alert';
  const color = tone === 'online' ? 'var(--online)' : tone === 'due' ? 'var(--due)' : 'var(--error)';

  return (
    <div className="panel px-3 py-2.5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="field-label">{label}</span>
        {sublabel && <span className="cap-sm cap text-text-muted">{sublabel}</span>}
      </div>
      <div className="mt-1 flex items-baseline gap-1.5">
        <span className="ow-num text-[26px] font-bold leading-none" style={{ color }}>
          {Math.round(clamped)}
        </span>
        <span className="cap cap-sm text-text-muted">{unit}</span>
      </div>

      <div
        className="relative mt-2 h-[9px] border border-rule bg-[var(--field)]"
        role="meter"
        aria-valuenow={Math.round(clamped)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={label}
      >
        <div className="absolute inset-0" aria-hidden="true">
          {[25, 50, 75].map((tick) => (
            <span
              key={tick}
              className="absolute top-0 h-full"
              style={{ left: `${tick}%`, borderLeft: '1px solid var(--rule)' }}
            />
          ))}
        </div>
        <div
          className="h-full transition-[width] duration-400 ease-[var(--ease-press)]"
          style={{ width: `${clamped}%`, background: color }}
        />
      </div>

      <div className="mt-1 flex justify-between" aria-hidden="true">
        <span className="ow-num text-[9px] text-text-muted">0</span>
        <span className="ow-num text-[9px] text-text-muted">50</span>
        <span className="ow-num text-[9px] text-text-muted">100</span>
      </div>

      {history && history.length > 1 && (
        <div className="mt-2">
          <MicroField values={history} max={100} height={18} tone={tone} label={`${label} recent history`} />
        </div>
      )}
    </div>
  );
}
