import client from './client';

export interface TagCount {
  tag: string;
  count: number;
}

export const listTags = () =>
  client.get<{ tags: TagCount[] }>('/tags').then((r) => r.data.tags);

export const setDeviceTags = (deviceId: string, tags: string[]) =>
  client.put<{ tags: string[] }>(`/devices/${deviceId}/tags`, { tags }).then((r) => r.data.tags);

export const bulkTags = (deviceIds: string[], add: string[], remove: string[]) =>
  client
    .post<{ updated: number }>('/devices/tags/bulk', { device_ids: deviceIds, add, remove })
    .then((r) => r.data);
