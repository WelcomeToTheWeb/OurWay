interface MetricGaugeProps {
  value: number;
  label: string;
  size?: number;
  unit?: string;
}

function getColor(value: number): string {
  if (value < 70) return '#22c55e';
  if (value < 90) return '#eab308';
  return '#ef4444';
}

export function MetricGauge({ value, label, size = 80, unit = '%' }: MetricGaugeProps) {
  const radius = 30;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.min(100, Math.max(0, value));
  const offset = circumference - (clamped / 100) * circumference;
  const color = getColor(clamped);

  return (
    <div className="flex flex-col items-center gap-1">
      <div
        className="relative"
        style={{ width: size, height: size }}
      >
        <svg
          width={size}
          height={size}
          viewBox={`0 0 60 60`}
          className="absolute inset-0 -rotate-90"
        >
          <circle
            cx="30"
            cy="30"
            r={radius}
            fill="none"
            stroke="#334155"
            strokeWidth="4"
          />
          <circle
            cx="30"
            cy="30"
            r={radius}
            fill="none"
            stroke={color}
            strokeWidth="4"
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            strokeLinecap="round"
            className="transition-all duration-500"
          />
        </svg>
        <div
          className="absolute inset-0 flex flex-col items-center justify-center"
          style={{ transform: 'rotate(0deg)' }}
        >
          <span className="text-sm font-semibold text-text-primary">{Math.round(clamped)}</span>
          {unit && (
            <span className="text-[10px] text-text-secondary">{unit}</span>
          )}
        </div>
      </div>
      <span className="text-xs text-text-secondary">{label}</span>
    </div>
  );
}
