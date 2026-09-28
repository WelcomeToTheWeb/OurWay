import client from './client';

export interface Session {
  id: string;
  device_id: string;
  user_id: string;
  status: string;
  offer_sdp: string;
  answer_sdp: string;
  ice_candidates: string[];
  created_at: string;
  ended_at: string | null;
}

export const startSession = (deviceId: string) =>
  client
    .post<{ session: Session; offer: string }>(`/devices/${deviceId}/sessions`)
    .then((r) => r.data);

export const submitAnswer = (sessionId: string, answer: string) =>
  client.post(`/sessions/${sessionId}/answer`, { answer }).then((r) => r.data);

export const addICECandidate = (sessionId: string, candidate: string) =>
  client
    .post(`/sessions/${sessionId}/ice`, { candidate })
    .then((r) => r.data);

export const sendInput = (sessionId: string, type: string, payload: any) =>
  client.post(`/sessions/${sessionId}/input`, { type, payload }).then((r) => r.data);

export const setQuality = (sessionId: string, quality: number) =>
  client.post(`/sessions/${sessionId}/quality`, { quality }).then((r) => r.data);

export const endSession = (sessionId: string) =>
  client.delete(`/sessions/${sessionId}`).then((r) => r.data);

export const listSessions = () =>
  client.get<{ sessions: Session[] }>('/sessions').then((r) => r.data.sessions);
