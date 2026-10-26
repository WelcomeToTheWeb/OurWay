import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// 2.2-1 acceptance, session-panel half: on a unit whose macOS agent lacks the
// Accessibility grant, the *session* must name the reason. A session on such a
// unit is indistinguishable from a working one — frames stream, the pointer
// renders — so the warning has to sit on the screen where the hands are about to
// work, not only on the device view (which `pages/DeviceDetail.test.tsx` covers).
//
// Why this lives here and not in the browser suite: opening a session from the
// console requires the unit's wire to be live at the instant of the click, and
// `POST /api/devices/:id/sessions` answers 409 whenever the hub has no socket.
// In CI the harness's fake agent for a *darwin* unit is in that state for the
// whole run — the fleet's presence is proven by its REST surface (2.2-2's store
// tests, the devices specs), and the session-start call fails for every one of
// four retries. An assertion that needs a live wire to run at all, in an
// environment where no wire is live, is a test that silently never tests.
// The panel is a component with a prop; the requirement is asserted on it.
vi.mock('../auth/context', () => ({
  useAuth: () => ({ accessToken: 'token', hasAnyRole: () => true }),
}));
vi.mock('../api/sessions', () => ({
  sendInput: vi.fn(() => Promise.resolve(undefined)),
  endSession: vi.fn(() => Promise.resolve(undefined)),
  setQuality: vi.fn(() => Promise.resolve(undefined)),
}));

import { SessionView } from './SessionView';
import type { Session } from '../api/sessions';

const mkSession = (o: Partial<Session> = {}): Session =>
  ({
    id: '0f4f3a9c-1111-2222-3333-444455556666',
    device_id: 'dev-1',
    user_id: 'u-1',
    status: 'active',
    started_at: new Date().toISOString(),
    ...o,
  }) as Session;

/**
 * The user channel socket the overlay opens for frames. jsdom has no WebSocket,
 * and the frame path is not what is under test: a constructor that hands back a
 * dead socket is the smallest thing that lets the component mount.
 */
function stubWebSocket() {
  class FakeSocket {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSED = 3;
    readyState = 1;
    send = vi.fn();
    close = vi.fn();
    addEventListener = vi.fn();
    removeEventListener = vi.fn();
    onopen: (() => void) | null = null;
    onmessage: ((e: { data: string }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
  }
  Object.assign(FakeSocket, { OPEN: 1 });
  // @ts-expect-error the stub is intentionally partial
  window.WebSocket = FakeSocket;
  // @ts-expect-error the stub is intentionally partial
  globalThis.WebSocket = FakeSocket;
}

function openPanelInControlMode(capability: 'granted' | 'denied' | 'unknown' | 'unavailable') {
  render(<SessionView session={mkSession()} onClose={vi.fn()} inputCapability={capability} />);
  // The footer — where the warning belongs — is the control-mode footer.
  fireEvent.click(screen.getByRole('button', { name: /take control/i }));
}

describe("remote session panel · the unit's remote-control state (2.2-1)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    stubWebSocket();
  });

  it('names the Accessibility reason on a unit that is discarding input', async () => {
    openPanelInControlMode('denied');
    const status = await screen.findByRole('status');
    expect(status.textContent).toMatch(/macOS is discarding input/i);
    expect(status.textContent).toMatch(/accessibility/i);
  });

  it('says so when the unit never reported, and when the build cannot do it', async () => {
    openPanelInControlMode('unknown');
    expect((await screen.findByRole('status')).textContent).toMatch(/never reported/i);
  });

  it('stays quiet on a unit that can synthesize input', async () => {
    openPanelInControlMode('granted');
    // The footer renders in control mode; it must carry no danger status.
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('prints words, not a locale key path', async () => {
    openPanelInControlMode('denied');
    const text = (await screen.findByRole('status')).textContent ?? '';
    expect(text).not.toMatch(/devices\.input/);
    expect(text).toMatch(/discarding|grant/i);
  });
});
