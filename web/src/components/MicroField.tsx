/**
 * A micro-field: the recent past of a headline figure in a narrow strip. Every
 * headline number on this console carries one, so a figure that moved reads as
 * having moved, and the onset of a state change is marked. It is a measurement,
 * drawn as a line and its area — never a decorative texture — and it is honest
 * about a single sample rather than pretending to be a trend.
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

  const w = 100;
  const span = Math.max(source.length - 1, 1);
  const y = (v: number) => height - 1 - (Math.max(v, 0) / peak) * (height - 3);
  const points = source.map((v, i) => [ (i / span) * w, y(v) ] as const);
  const trace = points.map(([px, py], i) => `${i === 0 ? 'M' : 'L'}${px.toFixed(2)},${py.toFixed(2)}`).join(' ');
  const onsetIndex = onset != null && onset >= 0 ? Math.round((onset / Math.max(values.length - 1, 1)) * span) : null;
  const last = points[points.length - 1];

  return (
    <svg
      role="img"
      aria-label={label ?? `Recent history, peak ${Math.round(peak)}`}
      viewBox={`0 0 ${w} ${height}`}
      preserveAspectRatio="none"
      width="100%"
      height={height}
      className={`block border-t border-rule pt-1 ${className}`}
      style={{ color }}
    >
      {source.length > 1 && (
        <>
          <path d={`${trace} L${w},${height} L0,${height} Z`} fill="currentColor" opacity={0.09} stroke="none" />
          <path
            d={trace}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.25}
            strokeLinejoin="round"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
        </>
      )}
      {onsetIndex != null && (
        <line
          x1={(onsetIndex / span) * w}
          x2={(onsetIndex / span) * w}
          y1={0}
          y2={height}
          stroke="var(--stamp)"
          strokeWidth={1}
          strokeDasharray="2 2"
          vectorEffect="non-scaling-stroke"
          opacity={0.7}
        />
      )}
      {/* The newest sample, so the eye lands on now. */}
      <circle
        cx={last[0]}
        cy={last[1]}
        r={1.8}
        fill="currentColor"
        style={{ transformBox: 'fill-box' }}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

function sample(values: number[], n: number): number[] {
  const out: number[] = [];
  const step = values.length / n;
  for (let i = 0; i < n; i += 1) out.push(values[Math.min(values.length - 1, Math.floor(i * step))]);
  return out;
}
