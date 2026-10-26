/**
 * A micro-field: barcode-density history in a narrow strip. Every headline
 * number on this console carries its recent past here, with the onset of a
 * change marked, so a figure that moved reads as having moved.
 */
export function MicroField({
  values,
  max,
  height = 22,
  onset,
  tone = 'service',
  label,
  bars = 72,
  className = '',
}: {
  values: number[];
  max?: number;
  height?: number;
  /** Index (0-based, in `values`) where the series changed state. */
  onset?: number;
  tone?: 'service' | 'alert' | 'due' | 'online' | 'neutral';
  label?: string;
  bars?: number;
  className?: string;
}) {
  const color =
    tone === 'alert'
      ? 'var(--error)'
      : tone === 'due'
        ? 'var(--due)'
        : tone === 'online'
          ? 'var(--online)'
          : tone === 'neutral'
            ? 'var(--text-muted)'
            : 'var(--service)';

  const source = values.length > bars ? sample(values, bars) : values;
  const peak = Math.max(max ?? 0, ...source, 1);

  if (source.length === 0) {
    return (
      <div
        className={`flex items-center border-t border-rule pt-1 text-[10px] text-text-muted ${className}`}
        style={{ height }}
      >
        No history
      </div>
    );
  }

  const unit = 3;
  const width = source.length * unit;
  const onsetIndex = onset != null && onset >= 0 ? Math.round((onset / Math.max(values.length - 1, 1)) * (source.length - 1)) : null;

  return (
    <svg
      role="img"
      aria-label={label ?? `Recent history, peak ${Math.round(peak)}`}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      width="100%"
      height={height}
      className={`block border-t border-rule pt-1 ${className}`}
      style={{ color }}
    >
      {source.map((v, i) => {
        const h = Math.max(1, (Math.max(v, 0) / peak) * height);
        return (
          <rect
            key={i}
            x={i * unit}
            y={height - h}
            width={unit - 1}
            height={h}
            fill="currentColor"
            opacity={0.3 + 0.7 * (v / peak)}
          />
        );
      })}
      {onsetIndex != null && (
        <line
          x1={onsetIndex * unit}
          x2={onsetIndex * unit}
          y1={0}
          y2={height}
          stroke="var(--stamp)"
          strokeWidth={1}
          strokeDasharray="2 2"
        />
      )}
    </svg>
  );
}

function sample(values: number[], n: number): number[] {
  const out: number[] = [];
  const step = values.length / n;
  for (let i = 0; i < n; i += 1) out.push(values[Math.min(values.length - 1, Math.floor(i * step))]);
  return out;
}
