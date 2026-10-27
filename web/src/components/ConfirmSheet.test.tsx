import { describe, it, expect, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { useState } from 'react';
import { ConfirmSheet, type ConfirmSheetRequest } from './ConfirmSheet';
import { confirmRequest, type ConfirmAction } from '../utils/confirmSheet';
import i18n from '../i18n';

// The real locales, not a stub: a confirmation whose copy is a string
// concatenation in English is exactly the bug this migration exists to fix.
const t = ((key: string, options?: Record<string, unknown>) =>
  i18n.t(key, options)) as Parameters<typeof confirmRequest>[0];

const ACTIONS: ConfirmAction[] = [
  'endSession',
  'deleteDevice',
  'deleteUser',
  'deleteWebhook',
  'deleteSsoBinding',
  'deletePolicy',
  'purgeMetrics',
  'deleteAccount',
  'deleteApiKey',
  'revokeApiKey',
];

function Harness({
  request,
  onConfirm,
  onCancel,
  busy,
}: {
  request: ConfirmSheetRequest;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
}) {
  return <ConfirmSheet request={request} busy={busy} onConfirm={onConfirm} onCancel={onCancel} />;
}

afterEach(() => {
  void i18n.changeLanguage('en');
});

describe('ConfirmSheet', () => {
  it('renders nothing when nothing is being confirmed', () => {
    const { container } = render(
      <ConfirmSheet request={null} onConfirm={() => {}} onCancel={() => {}} />
    );
    expect(container.textContent).toBe('');
  });

  it('names the target, states the consequence, and puts the verb on the control', () => {
    const request = confirmRequest(t, 'deleteDevice', { name: 'web-01' });
    render(<Harness request={request} onConfirm={() => {}} onCancel={() => {}} />);

    expect(screen.getByRole('dialog', { name: 'Delete unit web-01?' })).toBeTruthy();
    expect(screen.getByText(/This cannot be undone/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Delete unit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy();
    // "OK" is not a confirmation: the control says what it does.
    expect(screen.queryByRole('button', { name: /^ok$/i })).toBeNull();
  });

  it('writes nothing until the verb is pressed', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(
      <Harness request={confirmRequest(t, 'endSession', { id: 'a1b2c3d4' })} onConfirm={onConfirm} onCancel={onCancel} />
    );

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'End session' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('Escape cancels, and the sheet opens with the destructive control not focused', () => {
    const onCancel = vi.fn();
    render(
      <Harness request={confirmRequest(t, 'purgeMetrics')} onConfirm={() => {}} onCancel={onCancel} />
    );
    // Cancel is focused, not "Delete history": a sheet must not open armed.
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Cancel' }));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('rests both controls while the act is in flight', () => {
    render(
      <Harness
        request={confirmRequest(t, 'deleteAccount')}
        busy
        onConfirm={() => {}}
        onCancel={() => {}}
      />
    );
    expect((screen.getByRole('button', { name: 'Delete account' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Cancel' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('speaks words in en, de and fr for every destructive act — never a key path', () => {
    for (const lang of ['en', 'de', 'fr']) {
      void i18n.changeLanguage(lang);
      for (const action of ACTIONS) {
        const request = confirmRequest(t, action, { name: 'unit-01', id: 'a1b2c3d4', metrics: '12', alerts: '3' });
        const values = [request.title, request.body, request.confirmLabel];
        for (const value of values) {
          // An unresolved i18n key renders its own path on screen.
          expect(value, `${lang}/${action}: ${value}`).not.toContain('confirm.');
          expect(value, `${lang}/${action}: ${value}`).not.toContain('settings.');
          expect(value.trim().length, `${lang}/${action} is empty`).toBeGreaterThan(3);
        }
      }
      const de = confirmRequest(t, 'deleteDevice', { name: 'web-01' });
      const fr = confirmRequest(t, 'deleteDevice', { name: 'web-01' });
      if (lang === 'de') expect(de.title).toContain('löschen');
      if (lang === 'fr') expect(fr.title).toContain('Supprimer');
    }
  });

  it('is the page state pattern: the sheet closes when the act finishes', () => {
    function Page() {
      const [pending, setPending] = useState<ReturnType<typeof buildPending> | null>(buildPending());
      return (
        <div>
          <button onClick={() => setPending(buildPending())}>delete</button>
          <ConfirmSheet
            request={pending?.request ?? null}
            onCancel={() => setPending(null)}
            onConfirm={() => setPending(null)}
          />
        </div>
      );
    }
    function buildPending() {
      return { request: confirmRequest(t, 'deleteWebhook', { name: 'ops' }), run: () => {} };
    }

    render(<Page />);
    expect(screen.getByRole('dialog', { name: 'Delete webhook ops?' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Delete webhook' }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
