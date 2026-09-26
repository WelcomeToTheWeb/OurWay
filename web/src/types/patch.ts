export interface SoftwareUpdate {
  id: string;
  device_id: string;
  source: string;
  title: string;
  version: string;
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
  created_at: string;
  updated_at: string;
}

export interface PatchDeployment {
  id: string;
  policy_id: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  devices_total: number;
  devices_success: number;
  devices_failed: number;
  started_at?: string;
  completed_at?: string;
  created_at: string;
}
