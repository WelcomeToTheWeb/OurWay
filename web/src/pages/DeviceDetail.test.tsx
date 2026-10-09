import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { DeviceDetail } from './DeviceDetail';
import type { Device } from '../types/device';

// H5 regression test: the detail page must switch the device to short-
// interval streaming while it is open and stop it on the way out.

vi.mock('../api/devices', () => ({
  getDevice: vi.fn(),
  deleteDevice: vi.fn(),
  rebootDevice: vi.fn(),
  startDeviceStream: vi.fn().mockResolvedValue({ status: 'streaming', interval: 2 }),
  stopDeviceStream: vi.fn().mockResolvedValue({ status: 'stopped' }),
}));
vi.mock('../api/sessions', () => ({
  startSession: vi.fn(),
}));
vi.mock('../api/installers', () => ({
  downloadInstaller: vi.fn(),
  fetchInstallers: vi.fn().mockResolvedValue([]),
}));
vi.mock('../components/SessionView', () => ({
  SessionView: () => null,
}));
vi.mock('../components/InstallerPanel', () => ({
  InstallerPanel: () => null,
}));
vi.mock('../auth/context', () => ({
  useAuth: () => ({ accessToken: 't', hasAnyRole: () => true, hasRole: () => true }),
}));
vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: true }),
}));

import { getDevice, startDeviceStream, stopDeviceStream } from '../api/devices';

const device: Device = {
  id: 'dev-1',
  name: 'web-1',
  hostname: 'web-1.lan',
  os: 'linux',
  arch: 'amd64',
  agent_version: '1.0.0',
  status: 'online',
  last_seen: new Date().toISOString(),
  public_ip: '203.0.113.1',
  private_ip: '10.0.0.1',
  created_at: '',
  updated_at: '',
  tags: [],
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/devices/dev-1']}>
      <Routes>
        <Route path="/devices/:id" element={<DeviceDetail />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('DeviceDetail streaming toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getDevice).mockResolvedValue(device);
  });

  it('starts 2 s streaming on mount and stops it on unmount', async () => {
    const { unmount } = renderPage();
    await screen.findByText('web-1');
    expect(startDeviceStream).toHaveBeenCalledWith('dev-1', 2);
    unmount();
    expect(stopDeviceStream).toHaveBeenCalledWith('dev-1');
  });

  it('does not leave streaming on when the API errors', async () => {
    vi.mocked(startDeviceStream).mockRejectedValueOnce(new Error('503'));
    const { unmount } = renderPage();
    await screen.findByText('web-1');
    unmount();
    // A failed start must not crash the page; the stop call is still safe.
    expect(stopDeviceStream).toHaveBeenCalledWith('dev-1');
  });
});
