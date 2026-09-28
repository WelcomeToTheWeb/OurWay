import { useEffect, useRef, useState } from 'react';
import type { Metrics } from '../types/device';
import { useMetricsStore } from '../stores/metrics';
import { useDevicesStore } from '../stores/devices';

// Raw message as sent by the server: {type, payload}
interface RawWSMessage {
  type: string;
  payload?: {
    device_id?: string;
    device?: string;
    metrics?: Metrics;
    status?: 'online' | 'offline';
    alert_id?: string;
    [key: string]: unknown;
  };
}

export type WSMessage =
  | { type: 'metrics'; device_id: string; device?: string; metrics: Metrics }
  | { type: 'status'; device_id: string; status: 'online' | 'offline' }
  | { type: 'heartbeat'; device_id: string; device?: string }
  | { type: 'alert'; alert_id: string; device_id: string }
  | { type: 'connected' }
  | { type: 'error'; message: string };

// Flatten server's {type, payload} envelope into the WSMessage shape the UI expects
function flatten(raw: RawWSMessage): WSMessage | null {
  const p = raw.payload || {};
  switch (raw.type) {
    case 'metrics':
      if (p.device_id && p.metrics) return { type: 'metrics', device_id: p.device_id, device: p.device, metrics: p.metrics };
      break;
    case 'status':
      if (p.device_id && p.status) return { type: 'status', device_id: p.device_id, status: p.status };
      break;
    case 'heartbeat':
      if (p.device_id) return { type: 'heartbeat', device_id: p.device_id, device: p.device };
      break;
    case 'alert':
      if (p.alert_id && p.device_id) return { type: 'alert', alert_id: p.alert_id, device_id: p.device_id };
      break;
    case 'connected':
      return { type: 'connected' };
    case 'error':
      return { type: 'error', message: String(p.message || 'unknown error') };
    default:
      return null;
  }
  return null;
}

export function useWebSocket(token: string | null) {
  const [connected, setConnected] = useState(false);
  const [lastMetrics, setLastMetrics] = useState<Metrics | null>(null);
  const [lastDeviceId, setLastDeviceId] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tokenRef = useRef(token);

  tokenRef.current = token;

  const addMetric = useMetricsStore((s) => s.addMetric);
  const updateDeviceStatus = useDevicesStore((s) => s.updateDeviceStatus);

  useEffect(() => {
    if (!token) return;

    const connect = () => {
      if (wsRef.current?.readyState === WebSocket.OPEN) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.host;
      const url = `${protocol}//${host}/ws?token=${encodeURIComponent(token)}`;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => setConnected(true);

      ws.onmessage = (event) => {
        try {
          const raw: RawWSMessage = JSON.parse(event.data);
          const msg = flatten(raw);
          if (!msg) return;
          switch (msg.type) {
            case 'metrics':
              setLastMetrics(msg.metrics);
              setLastDeviceId(msg.device_id);
              addMetric(msg.device_id, msg.metrics);
              break;
            case 'status':
              // Realtime presence: update the device list immediately.
              updateDeviceStatus(msg.device_id, msg.status);
              break;
            case 'heartbeat':
              // A heartbeat means the device is alive.
              updateDeviceStatus(msg.device_id, 'online');
              break;
            case 'alert':
              // Let mounted pages (Alerts) refresh immediately.
              window.dispatchEvent(
                new CustomEvent('ourway:alert', { detail: msg })
              );
              break;
            case 'connected':
              setConnected(true);
              break;
          }
        } catch {
          // ignore malformed messages
        }
      };

      ws.onclose = () => {
        setConnected(false);
        reconnectTimeout.current = setTimeout(connect, 3000);
      };

      ws.onerror = () => ws.close();
    };

    connect();

    return () => {
      wsRef.current?.close();
      if (reconnectTimeout.current) clearTimeout(reconnectTimeout.current);
    };
  }, [token, addMetric, updateDeviceStatus]);

  return { connected, lastMetrics, lastDeviceId };
}
