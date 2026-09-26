import { useEffect, useRef, useState } from 'react';
import type { Metrics } from '../types/device';
import { useMetricsStore } from '../stores/metrics';

export type WSMessage =
  | { type: 'metrics'; device_id: string; device?: string; metrics: Metrics }
  | { type: 'status'; device_id: string; status: 'online' | 'offline' }
  | { type: 'heartbeat'; device_id: string; device?: string }
  | { type: 'alert'; alert_id: string; device_id: string }
  | { type: 'connected' }
  | { type: 'error'; message: string };

export function useWebSocket(token: string | null) {
  const [connected, setConnected] = useState(false);
  const [lastMetrics, setLastMetrics] = useState<Metrics | null>(null);
  const [lastDeviceId, setLastDeviceId] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tokenRef = useRef(token);

  tokenRef.current = token;

  const addMetric = useMetricsStore((s) => s.addMetric);

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
          const msg: WSMessage = JSON.parse(event.data);
          switch (msg.type) {
            case 'metrics':
              setLastMetrics(msg.metrics);
              setLastDeviceId(msg.device_id);
              addMetric(msg.device_id, msg.metrics);
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
  }, [token, addMetric]);

  return { connected, lastMetrics, lastDeviceId };
}
