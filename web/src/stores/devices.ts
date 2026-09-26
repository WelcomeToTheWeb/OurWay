import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { Device, Metrics } from '../types/device';

interface MetricHistoryEntry {
  time: number;
  cpu: number;
  ram: number;
  disk: number;
}

interface DeviceState {
  devices: Device[];
  loading: boolean;
  error: string | null;
  streamingDeviceId: string | null;
  metricsHistory: Record<string, MetricHistoryEntry[]>;
  lastUpdate: number | null;

  // Actions
  setDevices: (devices: Device[]) => void;
  addOrUpdateDevice: (device: Device) => void;
  removeDevice: (deviceId: string) => void;
  setStreaming: (deviceId: string | null) => void;
  addMetricHistory: (deviceId: string, metrics: Metrics) => void;
  clearMetricHistory: () => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  touchLastUpdate: () => void;
  updateDeviceStatus: (deviceId: string, status: Device['status']) => void;
}

const MAX_HISTORY = 60;

export const useDevicesStore = create<DeviceState>()(
  persist(
    (set) => ({
      devices: [],
      loading: false,
      error: null,
      streamingDeviceId: null,
      metricsHistory: {},
      lastUpdate: null,

      setDevices: (devices) => set({ devices }),

      addOrUpdateDevice: (device) =>
        set((state) => {
          const idx = state.devices.findIndex((d) => d.id === device.id);
          if (idx >= 0) {
            const devices = [...state.devices];
            devices[idx] = device;
            return { devices };
          }
          return { devices: [...state.devices, device] };
        }),

      removeDevice: (deviceId) =>
        set((state) => ({
          devices: state.devices.filter((d) => d.id !== deviceId),
        })),

      setStreaming: (deviceId) => set({ streamingDeviceId: deviceId }),

      addMetricHistory: (deviceId, metrics) =>
        set((state) => {
          const existing = state.metricsHistory[deviceId] || [];
          const entry: MetricHistoryEntry = {
            time: Date.now(),
            cpu: metrics.cpu,
            ram: metrics.ram,
            disk: metrics.disk_usage,
          };
          const updated = [...existing, entry].slice(-MAX_HISTORY);
          return {
            metricsHistory: {
              ...state.metricsHistory,
              [deviceId]: updated,
            },
          };
        }),

      clearMetricHistory: () => set({ metricsHistory: {} }),

      setLoading: (loading) => set({ loading }),
      setError: (error) => set({ error }),

      touchLastUpdate: () => set({ lastUpdate: Date.now() }),

      updateDeviceStatus: (deviceId, status) =>
        set((state) => ({
          devices: state.devices.map((d) =>
            d.id === deviceId ? { ...d, status, last_seen: new Date().toISOString() } : d
          ),
        })),
    }),
    {
      name: 'ourway-devices',
      partialize: (state) => ({
        devices: state.devices,
        metricsHistory: state.metricsHistory,
      }),
    }
  )
);
