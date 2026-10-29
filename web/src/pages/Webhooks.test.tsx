import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within, cleanup } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { Webhooks } from './Webhooks';
import { AuthProvider } from '../auth/context';
import en from '../i18n/locales/en.json';
import de from '../i18n/locales/de.json';
import fr from '../i18n/locales/fr.json';
import type { Channel, ChannelDelivery } from '../api/channels';

vi.mock('../api/channels', () => ({
  listChannels: vi.fn(),
  createChannel: vi.fn(),
  updateChannel: vi.fn(),
  deleteChannel: vi.fn(),
  testChannel: vi.fn(),
  listChannelDeliveries: vi.fn(),
  retryChannelDelivery: vi.fn(),
  parseChannelConfig: (raw: Channel['config']) =>
    typeof raw === 'string' ? JSON.parse(raw || '{}') : raw || {},
  parseChannelEvents: (raw: string) => JSON.parse(raw || '[]'),
}));
vi.mock('../api/clients', () => ({ getClients: vi.fn() }));

import {
  createChannel,
  deleteChannel,
  listChannelDeliveries,
  listChannels,
  retryChannelDelivery,
  testChannel,
  updateChannel,
} from '../api/channels';
import { getClients } from '../api/clients';

const mockedList = vi.mocked(listChannels);
const mockedCreate = vi.mocked(createChannel);
const mockedUpdate = vi.mocked(updateChannel);
const mockedDelete = vi.mocked(deleteChannel);
const mockedTest = vi.mocked(testChannel);
const mockedDeliveries = vi.mocked(listChannelDeliveries);
const mockedRetry = vi.mocked(retryChannelDelivery);
const mockedClients = vi.mocked(getClients);

function channel(over: Partial<Channel> = {}): Channel {
  return {
    id: 'chan-mail',
    name: 'On-call mailbox',
    kind: 'email',
    url: '',
    events: '["alert_created","patch_deployed"]',
    headers: '{}',
    enabled: true,
    config: { to: ['oncall@corp.example'] },
    scope_clients: '[]',
    min_severity: 'critical',
    tags: '[]',
    last_error: '',
    last_delivered_at: null,
    created_at: '2026-10-29T10:00:00Z',
    ...over,
  };
}

function delivery(over: Partial<ChannelDelivery> = {}): ChannelDelivery {
  return {
    id: 'del-1',
    webhook_id: 'chan-mail',
    event: 'alert_created',
    payload: '{}',
    status: 'pending',
    status_code: 500,
    response_body: 'endpoint refused',
    attempts: 2,
    next_retry_at: '2026-10-29T10:30:00Z',
    delivered_at: null,
    created_at: '2026-10-29T10:00:00Z',
    ...over,
  };
}

async function findLedger() {
  const ledger = await screen.findByRole('region', { name: /Notification channels/ });
  return within(ledger).getByRole('table');
}

function renderPage() {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={['/webhooks']}>
        <Routes>
          <Route path="/webhooks" element={<Webhooks />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}

async function openForm() {
  fireEvent.click(await screen.findByRole('button', { name: /Add channel/ }));
  return screen.findByRole('form', { name: /Add channel/ });
}

function fill(field: string | RegExp, value: string) {
  const input = screen.getByLabelText(field) as HTMLInputElement;
  fireEvent.change(input, { target: { value } });
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockedList.mockResolvedValue({
    channels: [
      channel(),
      channel({
        id: 'chan-hook',
        name: 'Fleet hook',
        kind: 'webhook',
        url: 'https://hook.invalid/fleet',
        config: {},
        min_severity: '',
      }),
    ],
    mail: { configured: true, host: 'mail.corp.example', port: 587, sender: 'ourway@corp.example' },
  });
  mockedClients.mockResolvedValue([
    { id: 'client-alpha', name: 'Alpha Co' },
    { id: 'client-beta', name: 'Beta Co' },
  ] as never);
  mockedCreate.mockResolvedValue({ channel: channel() });
  mockedUpdate.mockResolvedValue(channel());
  mockedDeliveries.mockResolvedValue([]);
});

describe('notification channel ledger', () => {
  it("states each channel's kind, destination and routing", async () => {
    renderPage();
    const table = await findLedger();

    const mailRow = within(table).getByText('On-call mailbox').closest('tr') as HTMLElement;
    expect(within(mailRow).getByText('Mailbox')).toBeInTheDocument();
    expect(within(mailRow).getByText('oncall@corp.example')).toBeInTheDocument();
    // The routing is stated on the row: which tenants, which floor.
    expect(within(mailRow).getByText('Every tenant')).toBeInTheDocument();
    expect(within(mailRow).getByText(/critical only/)).toBeInTheDocument();

    const hookRow = within(table).getByText('Fleet hook').closest('tr') as HTMLElement;
    expect(within(hookRow).getByText('HTTP endpoint')).toBeInTheDocument();
    expect(within(hookRow).getByText('https://hook.invalid/fleet')).toBeInTheDocument();
    expect(within(hookRow).getByText(/any event/)).toBeInTheDocument();
  });

  it('names the mail transport the installation has', async () => {
    renderPage();
    const notice = await screen.findByTestId('mail-transport');
    expect(notice).toHaveTextContent('mail.corp.example');
    expect(notice).toHaveTextContent('587');
    expect(notice).toHaveTextContent('ourway@corp.example');
  });

  it('says so when the installation has no mail server', async () => {
    mockedList.mockResolvedValue({
      channels: [channel()],
      mail: { configured: false },
    });
    renderPage();
    const notice = await screen.findByRole('status');
    expect(notice).toHaveTextContent('no mail server');
  });

  it('asks for recipients, not an endpoint, when the kind is mailbox', async () => {
    renderPage();
    const form = await openForm();
    fireEvent.click(within(form).getByRole('radio', { name: 'Mailbox' }));
    expect(within(form).getByLabelText(/Recipients/)).toBeInTheDocument();
    expect(
      within(form).queryByLabelText(/^(Endpoint|URL)$/)
    ).not.toBeInTheDocument();
  });

  it('sends the kind and its recipients, and no endpoint, for a mailbox', async () => {
    renderPage();
    const form = await openForm();
    fireEvent.click(within(form).getByRole('radio', { name: 'Mailbox' }));
    fill(/^Name$/, 'Night mailbox');
    fill(/Recipients/, 'night@corp.example, oncall@corp.example');
    fireEvent.click(within(form).getByRole('button', { name: /Save/ }));

    await waitFor(() => expect(mockedCreate).toHaveBeenCalled());
    const payload = mockedCreate.mock.calls[0][0];
    expect(payload.kind).toBe('email');
    expect(payload.config).toEqual({ to: ['night@corp.example', 'oncall@corp.example'] });
    expect(payload.url).toBe('');
  });

  it('keeps the refusal on screen when a channel was not saved', async () => {
    renderPage();
    mockedCreate.mockRejectedValueOnce(
      new Error('this installation has no mail server: set SMTP_HOST and SMTP_PORT before adding an email channel')
    );
    const form = await openForm();
    fireEvent.click(within(form).getByRole('radio', { name: 'Mailbox' }));
    fill(/^Name$/, 'Night mailbox');
    fill(/Recipients/, 'night@corp.example');
    fireEvent.click(within(form).getByRole('button', { name: /Save/ }));

    const answer = await screen.findByRole('alert');
    expect(answer).toHaveTextContent('no mail server');
    // The form is still open: the operator fixes it, the console did not save.
    expect(await screen.findByRole('form', { name: /Add channel/ })).toBeInTheDocument();
  });

  it('deletes a channel only through the sheet, naming what stops arriving', async () => {
    renderPage();
    const table = await findLedger();
    const row = within(table).getByText('On-call mailbox').closest('tr') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: /Delete/ }));

    const sheet = await screen.findByRole('dialog');
    expect(sheet).toHaveTextContent('Delete this notification channel?');
    expect(sheet).toHaveTextContent('On-call mailbox');
    expect(sheet).toHaveTextContent('Alert created');
    expect(mockedDelete).not.toHaveBeenCalled();

    fireEvent.click(within(sheet).getByRole('button', { name: 'Delete channel' }));
    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith('chan-mail'));
  });

  it('queues a test through the same routing and says where to read it', async () => {
    renderPage();
    const table = await findLedger();
    const row = within(table).getByText('Fleet hook').closest('tr') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: /Send a test/ }));

    await waitFor(() => expect(mockedTest).toHaveBeenCalledWith('chan-hook'));
    expect(await screen.findByRole('status')).toHaveTextContent('delivery history');
  });

  it('shows the delivery ledger with the peer answer and the retry, for any kind', async () => {
    mockedDeliveries.mockResolvedValue([
      delivery(),
      delivery({
        id: 'del-2',
        status: 'delivered',
        status_code: 250,
        response_body: 'queued',
        attempts: 1,
        next_retry_at: null,
      }),
    ]);
    renderPage();
    const table = await findLedger();
    const row = within(table).getByText('On-call mailbox').closest('tr') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: /Delivery history/ }));

    const dialog = await screen.findByRole('dialog', { name: /Delivery history/ });
    expect(within(dialog).getAllByText('Alert created')).toHaveLength(2);
    expect(within(dialog).getByText((text) => text.includes('endpoint refused'), { selector: 'span' })).toBeInTheDocument();
    expect(within(dialog).getByText(/Next retry/)).toBeInTheDocument();
    expect(within(dialog).getByText('queued')).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole('button', { name: /Retry now/ }));
    await waitFor(() => expect(mockedRetry).toHaveBeenCalledWith('chan-mail', 'del-1'));
  });

  it('states the tenant scope by the tenant name', async () => {
    mockedList.mockResolvedValue({
      channels: [channel({ scope_clients: '["client-alpha"]' })],
      mail: { configured: true, host: 'mail', port: 25, sender: 'x' },
    });
    renderPage();
    const table = await findLedger();
    expect(within(table).getByText('Alpha Co')).toBeInTheDocument();
  });

  it('names a channel that cannot be tested, instead of reporting a send', async () => {
    mockedTest.mockRejectedValueOnce(new Error('the channel does not subscribe to the test event'));
    renderPage();
    const table = await findLedger();
    const row = within(table).getByText('On-call mailbox').closest('tr') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: /Send a test/ }));

    const answer = await screen.findByRole('alert');
    expect(answer.textContent).toContain('does not subscribe to the test event');
  });
});

describe('localization of the channel console', () => {
  const enStrings = en.channels as Record<string, unknown>;
  const deStrings = de.channels as Record<string, unknown>;
  const frStrings = fr.channels as Record<string, unknown>;

  it('carries the channel copy in all three locales', () => {
    const keys = Object.keys(enStrings);
    expect(Object.keys(deStrings)).toEqual(keys);
    expect(Object.keys(frStrings)).toEqual(keys);
    for (const key of ['title', 'subtitle', 'addChannel', 'testSend', 'deliveries', 'mailNotice']) {
      for (const locale of [deStrings, frStrings]) {
        const value = locale[key];
        expect(typeof value).toBe('string');
        expect((value as string).length).toBeGreaterThan(3);
      }
    }
  });

  it('does not translate the destination, the endpoint, or the mail notice into English', () => {
    // A German operator reading "Every tenant" in a German console is the defect
    // the locale gate exists for; the phrase must differ per locale.
    const enFloor = (enStrings.floor as Record<string, string>).none;
    const deFloor = (deStrings.floor as Record<string, string>).none;
    const frFloor = (frStrings.floor as Record<string, string>).none;
    expect(new Set([enFloor, deFloor, frFloor]).size).toBe(3);
    expect(new Set([enStrings.mailNotice, deStrings.mailNotice, frStrings.mailNotice]).size).toBe(3);
  });

  it('names every channel kind and every event in English', () => {
    const kinds = enStrings.kindLabel as Record<string, string>;
    expect(kinds).toEqual({
      webhook: 'HTTP endpoint',
      slack: 'Slack',
      teams: 'Teams',
      email: 'Mailbox',
    });
    const events = enStrings.types as Record<string, string>;
    for (const event of ['alert_created', 'device_offline', 'patch_deployed', 'session_started', 'test']) {
      expect(events[event]).toBeTruthy();
    }
  });
});
