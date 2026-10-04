export type Severity = 'critical' | 'important' | 'moderate' | 'low' | 'unspecified';

export interface SoftwareUpdate {
  id: string;
  device_id: string;
  source: string;
  title: string;
  version: string;
  external_id?: string;
  kb?: string;
  severity?: Severity;
  category?: string;
  size_bytes: number;
  status: 'detected' | 'approved' | 'downloading' | 'installing' | 'installed' | 'failed' | 'skipped';
  installed_at?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface PatchPolicy {
  id: string;
  name: string;
  scope: 'all' | 'tags' | 'devices';
  scope_value: string;
  schedule: 'daily' | 'weekly' | 'monthly';
  auto_reboot: boolean;
  approval_required: boolean;
  max_devices_per_batch: number;
  window_start: string;
  window_hours: number;
  timezone: string;
  last_run_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface PatchDeployment {
  id: string;
  policy_id: string | null;
  status: 'pending' | 'running' | 'completed' | 'failed';
  devices_total: number;
  devices_success: number;
  devices_failed: number;
  started_at?: string;
  completed_at?: string;
  created_at: string;
}

export interface DeploymentResult {
  id: string;
  deployment_id: string;
  device_id: string;
  result: 'success' | 'failed';
  kind: 'deploy' | 'rollback';
  message?: string;
  created_at: string;
  updated_at: string;
}

export interface FleetUpdate {
  key: string;
  title: string;
  kb: string;
  severity: Severity;
  source: string;
  category: string;
  devices: number;
  statuses: Record<string, number>;
  update_ids: string[];
  detected_ids: string[];
}

export interface DeviceCompliance {
  device_id: string;
  name: string;
  os: string;
  status: string;
  detected: number;
  approved: number;
  installing: number;
  failed: number;
  critical: number;
  reboot_pending: boolean;
  compliant: boolean;
}

export interface PatchOverview {
  totals: {
    devices: number;
    compliant: number;
    pending: number;
    critical: number;
    reboot_pending: number;
  };
  devices: DeviceCompliance[];
}
