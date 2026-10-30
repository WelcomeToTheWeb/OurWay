import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { Recordings } from './Recordings';
import { AuthProvider } from '../auth/context';
import { useFleetStore } from '../stores/fleet';
import type { RecordingIndex, RecordingManifest, SessionRecording } from '../api/recordings';

vi.mock('../api/recordings', () => ({
  listRecordings: vi.fn(),
  getRecordingManifest: vi.fn(),
  getRecordingEvents: vi.fn(),
  fetchRecordingFrame: vi.fn(),
  startRecording: vi.fn(),
  downloadRecordingManifest: vi.fn(),
  downloadRecordingFile: vi.fn(),
  recordingReasonLabel: (reason: string) => reason,
}));
vi.mock('../api/clients', () => ({ getClients: vi.fn(() => Promise.resolve([])), getClientRollups: vi.fn(() => Promise.resolve([])) }));
vi.mock('../api/devices', () => ({ getDevices: vi.fn(() => Promise.resolve([])), getAlerts: vi.fn(() => Promise.resolve([])) }));
vi.mock('../api/patching', () => ({ getPatchOverview: vi.fn(() => Promise.resolve(null)), listPolicies: vi.fn(() => Promise.resolve([])) }));
vi.mock('../api/sessions', () => ({ listSessions: vi.fn(() => Promise.resolve([])) }));

import {
  fetchRecordingFrame,
  getRecordingEvents,
  getRecordingManifest,
  listRecordings,
} from '../api/recordings';

const mockedList = vi.mocked(listRecordings);
const mockedManifest = vi.mocked(getRecordingManifest);
const mockedEvents = vi.mocked(getRecordingEvents);
const mockedFrame = vi.mocked(fetchRecordingFrame);

function recording(over: Partial<SessionRecording> = {}): SessionRecording {
  return {
    id: 'rec-1',
    session_id: 'sess-1',
    device_id: 'unit-a',
    device_name: 'alpha-web-01',
    client_id: 'c-1',
    user_id: 'u-1',
    username: 'sam',
    status: 'complete',
    reason: 'policy:control-only',
    policy: 'control-only',
    started_at: '2026-10-30T14:00:00Z',
    ended_at: '2026-10-30T14:05:00Z',
    frame_count: 4,
    bytes: 40000,
    checksum: 'abc123def456',
    interruptions: 1,
    dropped_frames: 2,
    retention_until: '2026-11-29T14:00:00Z',
    playable: true,
    ...over,
  };
}

function index(over: Partial<RecordingIndex> = {}): RecordingIndex {
  return { recordings: [recording()], recording_policy: 'control-only', retention_days: 30, ...over };
}

function manifest(over: Partial<RecordingManifest> = {}): RecordingManifest {
  return {
    recording: recording(),
    playable: true,
    frames: [
      { index: 0, at: '2026-10-30T14:00:00Z' },
      { index: 1, at: '2026-10-30T14:00:01Z' },
    ],
    markers: [
      { kind: 'resume', at: '2026-10-30T14:02:00Z', detail: 'writing resumed' },
      { kind: 'cap', at: '2026-10-30T14:04:00Z' },
    ],
    checksum_on_disk: 'abc123def456',
    ...over,
  };
}

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={['/recordings']}>
        <Routes>
          <Route path="/recordings" element={<Recordings />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}

beforeEach(() => {
  cleanup();
  // jsdom has no object-URL machinery, and the player's frames arrive as object
  // URLs because an `<img src>` cannot carry the bearer token. The stub is the
  // browser's contract, not a behaviour we are testing.
  vi.stubGlobal('URL', {
    createObjectURL: vi.fn(() => 'blob:https://console.test/frame'),
    revokeObjectURL: vi.fn(),
  });
  useFleetStore.getState().resetFleet();
  mockedList.mockReset().mockResolvedValue(index());
  mockedManifest.mockReset().mockResolvedValue(manifest());
  mockedEvents.mockReset().mockResolvedValue([
    { at: '2026-10-30T14:00:00.500Z', action: 'session.input', target: 'device', input_type: 'key' },
  ]);
  mockedFrame.mockReset().mockResolvedValue('blob:https://console.test/frame-0');
  useFleetStore.getState().setCollection('recordings', index());
});

describe('Recordings ledger (2.4-8)', () => {
  it('names the unit, the account and the time, which is what the audit question asks', () => {
    renderPage();
    expect(screen.getByText('alpha-web-01')).toBeInTheDocument();
    expect(screen.getByText('sam')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
  });

  it('states the interruption and the dropped frames the file alone cannot show', () => {
    renderPage();
    expect(screen.getByText(/1 resumes/)).toBeInTheDocument();
    expect(screen.getByText(/2 dropped/)).toBeInTheDocument();
  });

  it('says plainly when the installation records nothing', async () => {
    useFleetStore.getState().setCollection('recordings', index({ recordings: [], recording_policy: 'off' }));
    renderPage();
    await waitFor(() =>
      expect(screen.getByText(/This installation records no sessions/)).toBeInTheDocument()
    );
  });

  it('states the policy and the retention window when it records', () => {
    renderPage();
    expect(screen.getByText(/from the moment someone takes control/)).toBeInTheDocument();
    expect(screen.getByText(/kept 30 days/)).toBeInTheDocument();
  });

  it('opens playback through the audited manifest read', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /^Play$/ }));
    await waitFor(() => expect(mockedManifest).toHaveBeenCalledWith('rec-1'));
    expect(await screen.findByText('Session recording')).toBeInTheDocument();
    expect(mockedEvents).toHaveBeenCalledWith('rec-1');
  });

  it('shows the break a restart left in the recording, and the cap that ended it', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /^Play$/ }));
    expect(await screen.findByText(/after the server stopped/)).toBeInTheDocument();
    expect(await screen.findByText(/reached its size limit/)).toBeInTheDocument();
  });

  it('lays the input the ledger recorded over the frame it produced', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /^Play$/ }));
    expect(await screen.findByText(/session\.input|key/)).toBeInTheDocument();
  });

  it('asks the authenticated client for the frame, because an img src carries no bearer token', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /^Play$/ }));
    await waitFor(() => expect(mockedFrame).toHaveBeenCalledWith('rec-1', 0));
  });
});
