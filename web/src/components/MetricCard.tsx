import { SparklineChart } from './SparklineChart';

interface MetricCardProps {
  label: string;
  value: number;
  unit?: string;
  history?: number[];
  threshold?: number;
  icon?: React.ReactNode;
}

function getMetricColor(value: number, threshold?: number): string {
  if (threshold !== undefined) {
    const pct = (value / threshold) * 100;
    if (pct < 70) return 'text-status-online';
    if (pct < 90) return 'text-status-warning';
    return 'text-status-error';
  }
  if (value < 70) return 'text-status-online';
  if (value < 90) return 'text-status-warning';
  return 'text-status-error';
}

function getMetricBarColor(value: number, threshold?: number): string {
  if (threshold !== undefined) {
    const pct = (value / threshold) * 100;
    if (pct < 70) return 'bg-status-online';
    if (pct < 90) return 'bg-status-warning';
    return 'bg-status-error';
  }
  if (value < 70) return 'bg-status-online';
  if (value < 90) return 'bg-status-warning';
  return 'bg-status-error';
}

export function MetricCard({
  label,
  value,
  unit = '%',
  history,
  threshold,
  icon,
}: MetricCardProps) {
  const colorClass = getMetricColor(value, threshold);
  const barColorClass = getMetricBarColor(value, threshold);
  const max = threshold ?? 100;
  const pct = Math.min(100, (value / max) * 100);

  return (
    <div className="rounded-xl border border-bg-border bg-bg-card p-4 transition-all duration-300 hover:border-accent/50">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          {icon && <span className="text-text-secondary">{icon}</span>}
          <span className="text-xs font-medium text-text-secondary uppercase tracking-wide">
            {label}
          </span>
        </div>
        <span className={`text-2xl font-bold ${colorClass}`}>
          {typeof value === 'number' ? value.toFixed(0) : value}
          <span className="text-sm font-normal ml-0.5">{unit}</span>
        </span>
      </div>

      {/* Progress bar */}
      <div className="h-1.5 w-full rounded-full bg-bg mb-3 overflow-hidden">
        <div
          className={`h-full rounded-full ${barColorClass} transition-all duration-500 ease-out`}
          style={{ width: `${pct}%` }}
        />
      </div>

      {/* Sparkline */}
      {history && history.length > 0 && (
        <div style={{ width: '100%', height: 32 }}>
          <SparklineChart
            data={history}
            color={colorClass === 'text-status-online' ? '#22c55e' : colorClass === 'text-status-warning' ? '#eab308' : '#ef4444'}
            width={200}
            height={32}
          />
        </div>
      )}
    </div>
  );
}
