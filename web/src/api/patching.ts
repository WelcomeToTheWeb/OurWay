import client from './client';
import type {
  SoftwareUpdate,
  PatchPolicy,
  PatchDeployment,
  DeploymentResult,
  FleetUpdate,
  PatchOverview,
} from '../types/patch';

export const getDeviceUpdates = (deviceId: string) =>
  client
    .get<{ updates: SoftwareUpdate[] }>(`/devices/${deviceId}/updates`)
    .then((r) => r.data.updates);

export const scanDeviceForUpdates = (deviceId: string) =>
  client.post(`/devices/${deviceId}/updates/scan`).then((r) => r.data);

export const approveUpdate = (updateId: string) =>
  client
    .post<{ update: SoftwareUpdate }>(`/updates/${updateId}/approve`)
    .then((r) => r.data.update);

export const listPolicies = () =>
  client
    .get<{ policies: PatchPolicy[] }>('/patch/policies')
    .then((r) => r.data.policies);

export interface PolicyInput {
  name: string;
  scope?: string;
  scope_value?: string;
  schedule?: string;
  auto_reboot?: boolean;
  approval_required?: boolean;
  max_devices_per_batch?: number;
  window_start?: string;
  window_hours?: number;
  timezone?: string;
}

export const createPolicy = (data: PolicyInput) =>
  client.post<{ policy: PatchPolicy }>('/patch/policies', data).then((r) => r.data.policy);

export const updatePolicy = (id: string, data: PolicyInput) =>
  client.put<{ policy: PatchPolicy }>(`/patch/policies/${id}`, data).then((r) => r.data.policy);

export const deletePolicy = (id: string) =>
  client.delete(`/patch/policies/${id}`).then((r) => r.data);

export const listDeployments = () =>
  client
    .get<{ deployments: PatchDeployment[] }>('/patch/deployments')
    .then((r) => r.data.deployments);

export const deployNow = (deviceIds?: string[]) =>
  client
    .post<{ deployment_id: string; status: string }>('/patch/deploy', { device_ids: deviceIds || [] })
    .then((r) => r.data);

export const rollbackDeployment = (deploymentId: string) =>
  client
    .post<{ deployment_id: string; status: string }>(`/patch/deployments/${deploymentId}/rollback`)
    .then((r) => r.data);

export const listDeploymentResults = (deploymentId: string) =>
  client
    .get<{ results: DeploymentResult[] }>(`/patch/deployments/${deploymentId}/results`)
    .then((r) => r.data.results);

export const getPatchOverview = () =>
  client.get<PatchOverview>('/patch/overview').then((r) => r.data);

export const listFleetUpdates = () =>
  client.get<{ updates: FleetUpdate[] }>('/patch/updates').then((r) => r.data.updates);

export const bulkApproveUpdates = (updateIds: string[]) =>
  client
    .post<{ changed: number }>('/patch/updates/approve', { update_ids: updateIds })
    .then((r) => r.data);

export const bulkSkipUpdates = (updateIds: string[]) =>
  client
    .post<{ changed: number }>('/patch/updates/skip', { update_ids: updateIds })
    .then((r) => r.data);

export const scanDevices = (deviceIds: string[] = []) =>
  client
    .post<{ scans_sent: number }>('/patch/scan', { device_ids: deviceIds })
    .then((r) => r.data);
