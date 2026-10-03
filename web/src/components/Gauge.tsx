import { useState, useEffect } from 'react';

interface GaugeProps {
  value: number;
  label: string;
  unit?: string;
  size?: number;
  sublabel?: string;
}

function getGaugeColor(value: number): string {
  if (value < 60) return '#22c55e';
  if (value < 80) return '#84cc16';
  if (value < 90) return '#eab308';
  if (value < 95) return '#f97316';
  return '#ef4444';
}

function getGaugeGradientStops(value: number): [string, string] {
  if (value < 60) return ['#10b981', '#22c55e'];
  if (value < 80) return ['#84cc16', '#a3e635'];
  if (value < 90) return ['#eab308', '#facc15'];
  if (value < 95) return ['#f97316', '#fb923c'];
  return ['#ef4444', '#f87171'];
}

export function Gauge({
  value,
  label,
  unit = '%',
  size = 120,
  sublabel,
}: GaugeProps) {
  const [animatedValue, setAnimatedValue] = useState(0);
  const color = getGaugeColor(value);
  const [startColor, endColor] = getGaugeGradientStops(value);

  // Animate to new value
  useEffect(() => {
    const start = animatedValue;
    const diff = value - start;
    const duration = 600;
    const startTime = performance.now();

    function animate(now: number) {
      const elapsed = now - startTime;
      const progress = Math.min(elapsed / duration, 1);
      const eased = 1 - Math.pow(1 - progress, 3); // ease-out cubic
      setAnimatedValue(start + diff * eased);
      if (progress < 1) {
        requestAnimationFrame(animate);
      }
    }

    requestAnimationFrame(animate);
  }, [value, animatedValue]);

  const strokeWidth = size * 0.08;
  const radius = (size - strokeWidth * 2) / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.min(100, Math.max(0, animatedValue));
  const offset = circumference - (clamped / 100) * circumference;

  const gradientId = `gauge-gradient-${label.replace(/\s+/g, '-')}`;

  return (
    <div className="flex flex-col items-center gap-1">
      <div className="relative" style={{ width: size, height: size }}>
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          className="-rotate-90"
        >
          <defs>
            <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor={startColor} />
              <stop offset="100%" stopColor={endColor} />
            </linearGradient>
          </defs>

          {/* Background track */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke="var(--bg-border)"
            strokeWidth={strokeWidth}
          />

          {/* Glow effect */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={`url(#${gradientId})`}
            strokeWidth={strokeWidth + 4}
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            strokeLinecap="round"
            className="transition-all duration-300"
            style={{ opacity: 0.3, filter: 'blur(4px)' }}
          />

          {/* Foreground fill */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={`url(#${gradientId})`}
            strokeWidth={strokeWidth}
            strokeDasharray={circumference}
            strokeDashoffset={offset}
            strokeLinecap="round"
            className="transition-all duration-100"
          />
        </svg>

        {/* Center text */}
        <div className="absolute inset-0 flex flex-col items-center justify-center rotate-0">
          <span className="text-xl font-bold text-text-primary" style={{ color }}>
            {Math.round(clamped)}
            <span className="text-sm font-medium">{unit}</span>
          </span>
        </div>
      </div>

      <div className="flex flex-col items-center gap-0.5">
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          {label}
        </span>
        {sublabel && (
          <span className="text-[10px] text-text-muted">{sublabel}</span>
        )}
      </div>
    </div>
  );
}
