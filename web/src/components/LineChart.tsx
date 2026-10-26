import {
  LineChart as RechartsLineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  ResponsiveContainer,
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
    <div className="border border-rule bg-bg-card px-2.5 py-2 text-[11px]">
      <p className="cap-sm cap mb-1 text-text-muted">{label}</p>
      {payload.map((entry) => (
        <div key={entry.name} className="flex items-baseline gap-2">
          <span className="inline-block h-2 w-2" style={{ backgroundColor: entry.color }} />
          <span className="text-text-primary">
            {entry.name}:{' '}
            <span className="ow-num font-semibold">{typeof entry.value === 'number' ? entry.value.toFixed(1) : entry.value}</span>
          </span>
        </div>
      ))}
    </div>
  );
}

/**
 * A measured trace: hairline rules on the value axis only, tabular numerals
 * on the ticks, and the threshold as a dashed rule labelled in the state ink.
 */
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
        className="flex items-center justify-center border border-dashed border-rule text-[12px] text-text-muted"
        style={{ height }}
      >
        Waiting for the next reporting interval
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={height}>
      <RechartsLineChart data={data} margin={{ top: 6, right: 8, left: 0, bottom: 4 }}>
        <XAxis
          dataKey={xKey}
          tick={{ fill: 'var(--text-muted)', fontSize: 10, fontFamily: 'JetBrains Mono, monospace' }}
          tickLine={false}
          axisLine={{ stroke: 'var(--rule-strong)' }}
          interval="preserveStartEnd"
        />
        <YAxis
          tick={{ fill: 'var(--text-muted)', fontSize: 10, fontFamily: 'JetBrains Mono, monospace' }}
          tickLine={false}
          axisLine={{ stroke: 'var(--rule-strong)' }}
          domain={yDomain}
          label={
            yLabel
              ? {
                  value: yLabel,
                  angle: -90,
                  position: 'left',
                  fill: 'var(--text-muted)',
                  fontSize: 10,
                }
              : undefined
          }
        />
        <Tooltip content={<CustomTooltip />} wrapperStyle={{ outline: 'none' }} />
        {referenceLine && (
          <ReferenceLine
            y={referenceLine.y}
            stroke={referenceLine.color || 'var(--error)'}
            strokeDasharray="4 4"
            label={{
              value: referenceLine.label,
              position: 'right',
              fill: referenceLine.color || 'var(--error)',
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
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3, fill: yk.color }}
            name={yk.name}
            isAnimationActive={false}
          />
        ))}
      </RechartsLineChart>
    </ResponsiveContainer>
  );
}
