import client from './client';
import type { FileTransfer } from '../types/file';

export const uploadFile = (file: File) => {
  const formData = new FormData();
  formData.append('file', file);
  return client
    .post<{ transfer_id: string; filename: string; size_bytes: number }>(
      '/files/upload',
      formData,
      { headers: { 'Content-Type': 'multipart/form-data' } }
    )
    .then((r) => r.data);
};

export const pushFile = (transferId: string, deviceIds: string[], destination?: string) =>
  client
    .post<{ status: string; device_count: number }>('/files/push', {
      transfer_id: transferId,
      device_ids: deviceIds,
      destination,
    })
    .then((r) => r.data);

export const pullFile = (deviceId: string, sourcePath: string) =>
  client
    .post<{ transfer_id: string; status: string }>('/files/pull', {
      device_id: deviceId,
      source_path: sourcePath,
    })
    .then((r) => r.data);

export const listTransfers = () =>
  client
    .get<{ transfers: FileTransfer[] }>('/files/transfers')
    .then((r) => r.data.transfers);

export const getTransfer = (id: string) =>
  client
    .get<{ transfer: FileTransfer }>(`/files/transfers/${id}`)
    .then((r) => r.data.transfer);
