import type { LucideIcon } from 'lucide-react';

interface InfoRowProps {
  label: string;
  value: React.ReactNode;
  icon?: LucideIcon;
}

/**
 * A record line: caption label on the left rule, value on the measure.
 * Hairline-ruled, tabular, and it truncates inside the block rather than
 * widening it — UUIDs and versions are long.
 */
export function InfoRow({ label, value, icon: Icon }: InfoRowProps) {
  return (
    <div className="flex items-baseline gap-3 border-b border-rule py-1.5 last:border-b-0">
      <span className="flex w-32 shrink-0 items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.09em] text-text-muted">
        {Icon && <Icon className="h-3.5 w-3.5" aria-hidden="true" />}
        {label}
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-2 text-[13px] font-medium text-text-primary">
        {typeof value === 'string' ? (
          <span className="ow-num truncate" title={value}>
            {value}
          </span>
        ) : (
          value
        )}
      </span>
    </div>
  );
}
