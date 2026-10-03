import client from './client';

// H4: frames travel over the user WebSocket (server-relayed) and input
// over POST /api/sessions/:id/input — no WebRTC SDP/ICE state exists.
export interface Session {
  id: string;
  device_id: string;
  user_id: string;
  status: string;
  created_at: string;
  ended_at: string | null;
}

// Payloads accepted by the agent's input handler.
export type InputPayload =
  | { event: 'move' | 'click'; x: number; y: number; button?: 'left' | 'right' }
  | { event: 'scroll'; delta: number }
  | { event: 'down' | 'up'; key: string; code: string };

export const startSession = (deviceId: string) =>
  client
    .post<{ session: Session }>(`/devices/${deviceId}/sessions`)
    .then((r) => r.data);

export const sendInput = (sessionId: string, type: 'mouse' | 'key', payload: InputPayload) =>
  client.post(`/sessions/${sessionId}/input`, { type, payload }).then((r) => r.data);

export const setQuality = (sessionId: string, quality: number) =>
  client.post(`/sessions/${sessionId}/quality`, { quality }).then((r) => r.data);

export const endSession = (sessionId: string) =>
  client.delete(`/sessions/${sessionId}`).then((r) => r.data);

export const listSessions = () =>
  client.get<{ sessions: Session[] }>('/sessions').then((r) => r.data.sessions);
