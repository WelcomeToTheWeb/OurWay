export type AlertSeverity = 'info' | 'warning' | 'critical';

export interface Alert {
  id: string;
  device_id: string;
  device_name: string;
  severity: AlertSeverity;
  message: string;
  metric: string;
  value: number;
  threshold: number;
  resolved: boolean;
  acknowledged: boolean;
  acknowledged_by?: string;
  assigned_to?: string;
  created_at: string;
}
