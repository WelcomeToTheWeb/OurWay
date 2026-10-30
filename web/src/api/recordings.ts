import client from './client';

// 2.4-8 · remote-control session recording.
//
// A recording is a manifest row plus a file of the frames the agent already
// posted. The console reads the manifest, fetches frames by index, and lays the
// input events the audit ledger recorded over the frames they produced — so
// playback answers "who was controlling that machine at 14:00" with the acts and
// the screen in one view.

export type RecordingPolicy = 'off' | 'on' | 'control-only';

export type RecordingStatus = 'recording' | 'complete' | 'missing';

export interface SessionRecording {
  id: string;
  session_id: string;
  device_id: string;
  device_name: string;
  client_id: string;
  user_id: string;
  username: string;
  status: RecordingStatus;
  /** Why this recording exists: the installation's policy, or a named person. */
  reason: string;
  policy: RecordingPolicy;
  started_at: string;
  ended_at: string | null;
  frame_count: number;
  bytes: number;
  checksum: string;
  /** Server restarts that resumed the file. Playback shows the break. */
  interruptions: number;
  /** Frames the writer refused: over the frame cap, or once the recording hit its size cap. */
  dropped_frames: number;
  retention_until: string | null;
  playable: boolean;
}

export interface RecordingIndex {
  recordings: SessionRecording[];
  recording_policy: RecordingPolicy;
  retention_days: number;
}

export interface FrameStamp {
  index: number;
  at: string;
}

export interface RecordingMarker {
  kind: string;
  at: string;
  detail?: string;
}

export interface RecordingManifest {
  recording: SessionRecording;
  playable: boolean;
  frames?: FrameStamp[];
  markers?: RecordingMarker[];
  bytes_on_disk?: number;
  checksum_on_disk?: string;
  checksum_mismatch?: boolean;
  index_error?: string;
}

export interface RecordingEvent {
  at: string;
  action: string;
  target: string;
  input_type?: string;
}

export const listRecordings = () =>
  client.get<RecordingIndex>('/recordings').then((r) => r.data);

/** The manifest. The server audits this read as `session.recording_play`:
 *  playing a recording is reading a person's screen, so it names the player. */
export const getRecordingManifest = (id: string) =>
  client.get<RecordingManifest>(`/recordings/${id}`).then((r) => r.data);

export const getRecordingEvents = (id: string) =>
  client
    .get<{ events: RecordingEvent[] }>(`/recordings/${id}/events`)
    .then((r) => r.data.events);

/**
 * The frames the installation is recording right now, keyed by session id, plus
 * the installation's policy.
 *
 * This is the same read as the session list — `GET /api/sessions` carries the
 * recording of each session — so the live session panel can state that a session
 * **is** being recorded without adding a request.
 */
export const getSessionRecordings = () =>
  client
    .get<{
      sessions: unknown[];
      recordings: Record<string, RecordingForSession>;
      recording_policy: RecordingPolicy;
    }>('/sessions')
    .then((r) => r.data);

export interface RecordingForSession {
  id: string;
  status: RecordingStatus;
  frame_count: number;
  bytes: number;
  reason: string;
  policy: RecordingPolicy;
  started_at: string;
  interruptions: number;
  dropped_frames: number;
}

/** The toggle. It starts a recording; nothing in the product stops one. */
export const startRecording = (sessionId: string) =>
  client
    .post<{ recording: SessionRecording }>(`/sessions/${sessionId}/recording`)
    .then((r) => r.data.recording);

/**
 * One frame of a recording, as an object URL.
 *
 * An `<img src>` cannot carry the `Authorization` header, and a URL that does
 * carry a live bearer token is a token in the browser history, the proxy access
 * log and the `Referer` header — the same reasoning as the audit CSV export. So
 * the frame is fetched through the authenticated client and the caller revokes
 * the URL when the frame leaves the screen.
 */
export async function fetchRecordingFrame(id: string, index: number): Promise<string> {
  const { data } = await client.get(`/recordings/${id}/frames/${index}`, {
    responseType: 'blob',
  });
  return URL.createObjectURL(data);
}

export const revokeRecordingFrame = (url: string) => URL.revokeObjectURL(url);

/** The manifest export. A recording's record, as a file the operator keeps. */
export async function downloadRecordingManifest(id: string): Promise<void> {
  const { data } = await client.get(`/recordings/${id}/manifest`, { responseType: 'blob' });
  const url = URL.createObjectURL(new Blob([data], { type: 'application/json' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = `recording-${id}.manifest.json`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

/** The sequence file itself, for the playback tool the operator runs offline. */
export async function downloadRecordingFile(id: string): Promise<void> {
  const { data } = await client.get(`/recordings/${id}/file`, { responseType: 'blob' });
  const url = URL.createObjectURL(new Blob([data], { type: 'application/octet-stream' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = `recording-${id}.seq`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

/** Plain-language reading of a recording's reason, for the console. */
export function recordingReasonLabel(reason: string): string {
  switch (reason) {
    case 'policy:on':
      return 'the installation records every session';
    case 'policy:control-only':
      return 'the installation records sessions where someone takes control';
    case 'operator:ordered':
      return 'an administrator ordered this recording';
    case 'technician:self':
      return 'the technician started it';
    default:
      return reason;
  }
}
