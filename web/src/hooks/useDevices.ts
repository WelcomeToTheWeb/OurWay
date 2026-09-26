import { useCallback, useEffect } from 'react';
import { getDevices } from '../api/devices';
import { useDevicesStore } from '../stores/devices';


export function useDevices() {
  const devices = useDevicesStore((s) => s.devices);
  const loading = useDevicesStore((s) => s.loading);
  const error = useDevicesStore((s) => s.error);
  const streamingDeviceId = useDevicesStore((s) => s.streamingDeviceId);
  const setDevices = useDevicesStore((s) => s.setDevices);
  const setLoading = useDevicesStore((s) => s.setLoading);
  const setError = useDevicesStore((s) => s.setError);
  const setStreaming = useDevicesStore((s) => s.setStreaming);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getDevices();
      setDevices(data);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [setDevices, setLoading, setError]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return {
    devices,
    loading,
    error,
    refresh,
    streamingDeviceId,
    setStreaming,
  };
}
