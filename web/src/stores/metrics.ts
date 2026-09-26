import { create } from 'zustand';
import type { Metrics } from '../types/device';

const MAX_POINTS = 120;

interface DeviceHistory {
  cpu: number[];
  ram: number[];
  disk: number[];
  netIn: number[];
  netOut: number[];
  timestamps: string[];
}

interface MetricsStore {
  history: Record<string, DeviceHistory>;
  addMetric: (deviceId: string, metrics: Metrics) => void;
  clearHistory: (deviceId: string) => void;
}

function emptyHistory(): DeviceHistory {
  return {
    cpu: [],
    ram: [],
    disk: [],
    netIn: [],
    netOut: [],
    timestamps: [],
  };
}

export const useMetricsStore = create<MetricsStore>((set) => ({
  history: {},

  addMetric: (deviceId, metrics) => {
    set((state) => {
      const existing = state.history[deviceId] || emptyHistory();

      const newTimestamp = new Date().toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
      });

      const cpu = [...existing.cpu, metrics.cpu];
      const ram = [...existing.ram, metrics.ram];
      const disk = [...existing.disk, metrics.disk_usage];
      const netIn = [...existing.netIn, metrics.net_in];
      const netOut = [...existing.netOut, metrics.net_out];
      const timestamps = [...existing.timestamps, newTimestamp];

      // Keep only the last MAX_POINTS entries
      const sliceFrom = Math.max(0, cpu.length - MAX_POINTS);
      cpu.splice(0, sliceFrom);
      ram.splice(0, sliceFrom);
      disk.splice(0, sliceFrom);
      netIn.splice(0, sliceFrom);
      netOut.splice(0, sliceFrom);
      timestamps.splice(0, sliceFrom);

      return {
        history: {
          ...state.history,
          [deviceId]: { cpu, ram, disk, netIn, netOut, timestamps },
        },
      };
    });
  },

  clearHistory: (deviceId) => {
    set((state) => {
      const newHistory = { ...state.history };
      delete newHistory[deviceId];
      return { history: newHistory };
    });
  },
}));
