import { useCallback } from 'react';
import { useFleet } from './useFleet';
import { useFleetStore } from '../stores/fleet';
import type { Device } from '../types/device';

/**
 * The unit list, as a view on the shared fleet read model (2.2-2).
 *
 * It used to own a poller: every component that called it fetched
 * `GET /api/devices` on mount. Now the list arrives from the fleet store,
 * where one ref-counted poller serves every subscriber, so a second page
 * open on the fleet costs nothing.
 */
export function useDevices(): {
  devices: Device[];
  loading: boolean;
  error: string | null;
  refresh: () => void;
  streamingDeviceId: string | null;
  setStreaming: (deviceId: string | null) => void;
} {
  const { data, loading, error, refresh } = useFleet('devices', { owner: true });
  const streamingDeviceId = useFleetStore((s) => s.streamingDeviceId);
  const setStreaming = useFleetStore((s) => s.setStreaming);

  const refreshNow = useCallback(() => refresh(), [refresh]);

  return {
    devices: data,
    loading,
    error: error ?? null,
    refresh: refreshNow,
    streamingDeviceId,
    setStreaming,
  };
}
