import type { LucideIcon } from 'lucide-react';

interface InfoRowProps {
  label: string;
  value: React.ReactNode;
  icon?: LucideIcon;
}

export function InfoRow({ label, value, icon: Icon }: InfoRowProps) {
  return (
    <div className="flex items-center gap-3 py-2">
      <span className="flex w-32 shrink-0 items-center gap-2 text-xs text-text-secondary">
        {Icon && <Icon className="h-3.5 w-3.5 text-text-muted" />}
        {label}
      </span>
      {/* min-w-0 + flex-1 let long values (UUIDs, versions) truncate
          inside the card instead of pushing the box wider. Plain
          strings get an ellipsis plus a title tooltip with the full
          value; node values (copy-button rows) truncate their own text. */}
      <span className="flex min-w-0 flex-1 items-center gap-2 text-sm font-medium text-text-primary">
        {typeof value === 'string' ? (
          <span className="truncate" title={value}>
            {value}
          </span>
        ) : (
          value
        )}
      </span>
    </div>
  );
}
