import client from './client';
import type { Device, Metrics } from '../types/device';
import type { Alert as AlertType } from '../types/alert';

export const getDevices = () =>
  client
    .get<{ devices: Device[] }>('/devices')
    .then((r) => r.data.devices);

export const getDevice = (id: string) =>
  client
    .get<{ device: Device; metrics?: Metrics }>(`/devices/${id}`)
    .then((r) => r.data.device);

export const deleteDevice = (id: string) =>
  client.delete(`/devices/${id}`).then((r) => r.data);

export const clearMonitoringData = () =>
  client
    .delete<{ status: string; metrics_cleared: number; alerts_cleared: number }>(
      '/monitoring/data',
    )
    .then((r) => r.data);

export const registerAgent = (data: {
  hostname: string;
  os: string;
  arch: string;
  agentVersion: string;
}) =>
  client.post<{ deviceKey: string }>('/agents/register', data).then((r) => r.data);

export const heartbeat = (deviceKey: string) =>
  client.post(`/agents/${deviceKey}/heartbeat`).then((r) => r.data);

export const sendMetrics = (deviceKey: string, metrics: Partial<Metrics>) =>
  client.post(`/agents/${deviceKey}/metrics`, metrics).then((r) => r.data);

export const getAlerts = (params?: { severity?: string; resolved?: boolean; device_id?: string }) =>
  client
    .get<{ alerts: AlertType[] }>('/alerts', { params })
    .then((r) => r.data.alerts);

export const resolveAlert = (id: string) =>
  client.post(`/alerts/${id}/resolve`).then((r) => r.data);

export const acknowledgeAlert = (id: string) =>
  client.post(`/alerts/${id}/acknowledge`).then((r) => r.data);

export const assignAlert = (id: string, userId: string) =>
  client.post(`/alerts/${id}/assign`, { user_id: userId }).then((r) => r.data);

export const rebootDevice = (id: string, delaySeconds?: number) =>
  client
    .post(`/devices/${id}/reboot`, delaySeconds ? { delay_seconds: delaySeconds } : {})
    .then((r) => r.data);
