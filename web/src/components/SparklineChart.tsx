import { Line, LineChart, ResponsiveContainer } from 'recharts';

interface SparklineChartProps {
  data: number[];
  color?: string;
  width?: number;
  height?: number;
  fill?: boolean;
}

export function SparklineChart({
  data,
  color = '#3b82f6',
  width = 120,
  height = 32,
  fill = true,
}: SparklineChartProps) {
  if (!data || data.length === 0) {
    return (
      <div
        className="flex items-center justify-center rounded bg-bg"
        style={{ width, height }}
      >
        <span className="text-[10px] text-text-muted">No data</span>
      </div>
    );
  }

  const chartData = data.map((value, i) => ({ i, value }));

  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={chartData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
        {fill && (
          <defs>
            <linearGradient id={`spark-grad-${color}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.3} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
        )}
        <Line
          type="monotone"
          dataKey="value"
          stroke={color}
          strokeWidth={2}
          dot={false}
          fill={fill ? `url(#spark-grad-${color})` : 'none'}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}
