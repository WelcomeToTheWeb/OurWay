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
      <span className="text-sm font-medium text-text-primary">{value}</span>
    </div>
  );
}
