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
  seedHistory: (deviceId: string, rows: MetricsRow[]) => void;
  clearHistory: (deviceId: string) => void;
}

/**
 * One server history row. A page opened on a device shows that day's
 * recorded shape, not just the samples that arrive while the tab is open,
 * so the store accepts the server's rows directly.
 */
export interface MetricsRow {
  timestamp: string;
  cpu: number;
  ram: number;
  disk_usage: number;
  net_in: number;
  net_out: number;
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

  seedHistory: (deviceId, rows) => {
    set((state) => {
      const cpu: number[] = [];
      const ram: number[] = [];
      const disk: number[] = [];
      const netIn: number[] = [];
      const netOut: number[] = [];
      const timestamps: string[] = [];
      for (const row of rows) {
        cpu.push(row.cpu);
        ram.push(row.ram);
        disk.push(row.disk_usage);
        netIn.push(row.net_in);
        netOut.push(row.net_out);
        timestamps.push(new Date(row.timestamp).toISOString().slice(11, 16));
      }
      const from = Math.max(0, cpu.length - MAX_POINTS);
      return {
        history: {
          ...state.history,
          [deviceId]: {
            cpu: cpu.slice(from),
            ram: ram.slice(from),
            disk: disk.slice(from),
            netIn: netIn.slice(from),
            netOut: netOut.slice(from),
            timestamps: timestamps.slice(from),
          },
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
