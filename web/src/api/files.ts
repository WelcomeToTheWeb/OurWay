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

// downloadFile fetches a stored file (GET /files/:transfer_id/file) and
// triggers a browser download under its original name.
export const downloadFile = async (transferId: string, filename: string) => {
  const { data } = await client.get(`/files/${transferId}/file`, {
    responseType: 'blob',
  });
  const url = URL.createObjectURL(data as Blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};
