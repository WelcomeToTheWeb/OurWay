import {
  LineChart as RechartsLineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts';

interface YKey {
  key: string;
  name: string;
  color: string;
}

interface LineChartProps {
  data: Record<string, unknown>[];
  xKey: string;
  yKeys: YKey[];
  height?: number;
  yDomain?: [number, number];
  yLabel?: string;
  referenceLine?: { y: number; label: string; color?: string };
}

function CustomTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: Array<{ name: string; value: number; color: string }>;
  label?: string;
}) {
  if (!active || !payload || payload.length === 0) return null;

  return (
    <div className="rounded-lg border border-bg-border bg-bg-secondary p-3 shadow-lg">
      <p className="mb-2 text-xs font-medium text-text-secondary">{label}</p>
      {payload.map((entry) => (
        <div key={entry.name} className="flex items-center gap-2">
          <span
            className="inline-block h-2 w-2 rounded-full"
            style={{ backgroundColor: entry.color }}
          />
          <span className="text-xs text-text-primary">
            {entry.name}: {typeof entry.value === 'number' ? entry.value.toFixed(1) : entry.value}
          </span>
        </div>
      ))}
    </div>
  );
}

export function LineChart({
  data,
  xKey,
  yKeys,
  height = 200,
  yDomain = [0, 100],
  yLabel,
  referenceLine,
}: LineChartProps) {
  if (!data || data.length === 0) {
    return (
      <div
        className="flex items-center justify-center rounded-lg bg-bg/50 text-sm text-text-muted"
        style={{ height }}
      >
        Waiting for data...
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={height}>
      <RechartsLineChart
        data={data}
        margin={{ top: 5, right: 10, left: 0, bottom: 5 }}
      >
        <CartesianGrid
          stroke="var(--bg-border)"
          vertical={false}
          strokeDasharray="3 3"
        />
        <XAxis
          dataKey={xKey}
          tick={{ fill: 'var(--text-secondary)', fontSize: 10 }}
          tickLine={false}
          axisLine={{ stroke: 'var(--bg-border)' }}
          interval="preserveStartEnd"
        />
        <YAxis
          tick={{ fill: 'var(--text-secondary)', fontSize: 10 }}
          tickLine={false}
          axisLine={{ stroke: 'var(--bg-border)' }}
          domain={yDomain}
          label={
            yLabel
              ? {
                  value: yLabel,
                  angle: -90,
                  position: 'left',
                  fill: 'var(--text-secondary)',
                  fontSize: 10,
                }
              : undefined
          }
        />
        <Tooltip
          content={<CustomTooltip />}
          wrapperStyle={{ outline: 'none' }}
        />
        {referenceLine && (
          <ReferenceLine
            y={referenceLine.y}
            stroke={referenceLine.color || '#ef4444'}
            strokeDasharray="4 4"
            label={{
              value: referenceLine.label,
              position: 'right',
              fill: referenceLine.color || '#ef4444',
              fontSize: 10,
            }}
          />
        )}
        {yKeys.map((yk) => (
          <Line
            key={yk.key}
            type="monotone"
            dataKey={yk.key}
            stroke={yk.color}
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4, fill: yk.color }}
            name={yk.name}
            isAnimationActive={false}
          />
        ))}
      </RechartsLineChart>
    </ResponsiveContainer>
  );
}
