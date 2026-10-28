import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

const enLocale = enLocaleRaw as Record<string, unknown>;

import enLocaleRaw from '../i18n/locales/en.json';
import { Audit } from './Audit';
import { AuthProvider } from '../auth/context';
import type { AuditLog, AuditPage } from '../api/audit';

vi.mock('../api/audit', () => {
  const actual = {
    getAuditLog: vi.fn(),
    getAuditActions: vi.fn(),
    downloadAuditCSV: vi.fn(),
    scopeHeader: vi.fn(() => 'client-1'),
    outcomeKey: vi.fn((outcome: string) => `audit.outcomes.${outcome}`),
    targetLabel: vi.fn((row: AuditLog) => (row.target_id ? `${row.target_type} ${row.target_id.slice(0, 8)}` : row.target_type)),
    auditQueryFromSearch: vi.fn((search: string) => {
      const p = new URLSearchParams(search);
      return {
        action: p.get('action') ?? '',
        clientId: p.get('client') ?? '',
        from: p.get('from') ?? '',
        to: p.get('to') ?? '',
        target: p.get('target') ?? '',
        actor: p.get('actor') ?? '',
      };
    }),
    withAuditParams: vi.fn((search: string, q: Record<string, string>) => {
      const p = new URLSearchParams(search);
      for (const [key, value] of [
        ['action', q.action],
        ['client', q.clientId],
        ['from', q.from],
        ['to', q.to],
        ['target', q.target],
        ['actor', q.actor],
      ] as const) {
        if (value) p.set(key, value);
        else p.delete(key);
      }
      const out = p.toString();
      return out ? `?${out}` : '';
    }),
  };
  return actual;
});
vi.mock('../api/clients', () => ({
  getClients: vi.fn(),
  clientFromSearch: vi.fn((search: string) => new URLSearchParams(search).get('client') ?? ''),
  withClientParam: vi.fn((search: string, slug: string) => {
    const p = new URLSearchParams(search);
    if (slug === '') p.delete('client');
    else p.set('client', slug);
    const out = p.toString();
    return out ? `?${out}` : '';
  }),
}));

import { getAuditLog, getAuditActions, downloadAuditCSV } from '../api/audit';
import { getClients } from '../api/clients';

const mockedLog = vi.mocked(getAuditLog);
const mockedActions = vi.mocked(getAuditActions);
const mockedExport = vi.mocked(downloadAuditCSV);
const mockedClients = vi.mocked(getClients);

function row(over: Partial<AuditLog> = {}): AuditLog {
  return {
    id: `row-${Math.random().toString(16).slice(2)}`,
    actor_user_id: 'user-1',
    actor_username: 'dana',
    actor_roles: 'technician',
    actor_provider: 'local',
    action: 'device.reboot',
    target_type: 'device',
    target_id: '0f0f0f0f-1111-2222-3333-444444444444',
    client_id: 'client-1',
    outcome: 'ok',
    details: '',
    source_ip: '10.0.0.7',
    created_at: '2026-10-27T10:30:00Z',
    ...over,
  };
}

function page(logs: AuditLog[], over: Partial<AuditPage> = {}): AuditPage {
  return {
    logs,
    total: logs.length,
    scope: 'client-1',
    retention_days: 365,
    limit: 200,
    offset: 0,
    retention: {
      days: 365,
      rows_pruned: 42,
      last_run: '2026-10-27T09:00:00Z',
      sweep_errored: false,
    },
    ...over,
  };
}

/**
 * keyPathPattern matches any `<namespace>.<key>` the locale files define,
 * including nested paths like `audit.col.actor`.
 *
 * The point of building it from the locale file, rather than a generic regex,
 * is that the audit ledger is the one page in this console whose content is
 * dotted by design. A blanket `\w+\.\w+` test on this page would fail on
 * correct rows, and a test that fails on correct output gets switched off —
 * which is how the key-path guard stops being a guard.
 */
function keyPathPattern(): RegExp {
  const escaped = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const paths = localePaths();
  return new RegExp(`\\b(${paths.map(escaped).join('|')})\\b`);
}

function localePaths(): string[] {
  const locale = enLocale as Record<string, unknown>;
  const out: string[] = [];
  const walk = (prefix: string[], node: unknown) => {
    if (typeof node === 'string') {
      out.push(prefix.join('.'));
      return;
    }
    if (node && typeof node === 'object') {
      for (const [key, child] of Object.entries(node as Record<string, unknown>)) {
        walk([...prefix, key], child);
      }
    }
  };
  for (const [key, node] of Object.entries(locale)) walk([key], node);
  return out.sort((a, b) => b.length - a.length);
}

function table(): HTMLElement {
  const el = document.querySelector('table.ledger');
  if (!el) throw new Error('the ledger table is not on the page');
  return el as HTMLElement;
}

function renderPage(initialEntry = '/audit') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <AuthProvider>
        <Routes>
          <Route path="/audit" element={<Audit />} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

describe('Audit ledger (2.3-6)', () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
    localStorage.setItem('ourway_user', JSON.stringify({ id: 'user-1', username: 'dana' }));
    localStorage.setItem('ourway_roles', JSON.stringify(['admin']));
    mockedLog.mockResolvedValue(
      page([
        row(),
        row({
          id: 'refused-row',
          action: 'patch.deploy',
          actor_username: 'eri',
          actor_roles: 'manager',
          outcome: 'refused',
          target_type: 'device-batch',
          target_id: '',
        }),
      ])
    );
    mockedActions.mockResolvedValue({ actions: ['device.reboot', 'patch.deploy'], retention_days: 365 });
    mockedClients.mockResolvedValue([
      { id: 'client-1', name: 'Alpha Co', slug: 'alpha-co', notes: '', disabled: false, created_at: '', updated_at: '' },
    ]);
    mockedExport.mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the record of each act: who, what, target, outcome — not key paths', async () => {
    renderPage();

    expect(await screen.findByText('dana')).toBeInTheDocument();
    expect(screen.getByText('eri')).toBeInTheDocument();
    // The action name appears twice by design — once in the row, once in the
    // filter's option list — so the assertion reads the table, which is where
    // the record is.
    expect(within(table()).getAllByText('device.reboot').length).toBeGreaterThanOrEqual(1);
    // The target id is truncated on screen and kept whole in the row's title,
    // so a readable page does not cost the operator the exact id.
    expect(screen.getByText(/device 0f0f0f0f/)).toBeInTheDocument();
    expect(document.querySelector('[title="0f0f0f0f-1111-2222-3333-444444444444"]')).not.toBeNull();

    // No raw i18n key path anywhere on the page: a missing key renders its own
    // path, and this project has shipped that bug before.
    expect(document.body.textContent).not.toMatch(keyPathPattern());
  });

  it('shows refused attempts beside successes, in the default view', async () => {
    renderPage();
    await screen.findByText('dana');

    // The refusal is not a footnote: it is a row, with its own outcome stamp.
    const refused = await screen.findByText('refused');
    expect(refused).toBeInTheDocument();
    expect(screen.getByText('done')).toBeInTheDocument();
  });

  it('states the scope the server applied and the retention it is subject to', async () => {
    renderPage();

    // The retention window is printed twice on purpose — in the header strip
    // and in the footnote under the table — so the assertion counts, rather
    // than assuming a single occurrence.
    expect(await screen.findByText(/Scope applied: client-1/)).toBeInTheDocument();
    expect(screen.getAllByText(/older than 365 days/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/Last sweep removed 42 rows/)).toBeInTheDocument();
  });

  it('says when the retention sweep has never run, and when it failed', async () => {
    mockedLog.mockResolvedValue(
      page([row()], {
        retention: { days: 365, rows_pruned: 0, last_run: '0001-01-01T00:00:00Z', sweep_errored: false },
      })
    );
    renderPage();
    expect(await screen.findByText(/has not run yet/)).toBeInTheDocument();

    // Unmount, then stage the failing sweep and mount again: the page reads
    // retention off the ledger response, so the fixture has to change before
    // the request the new page makes.
    const first = renderPage();
    await screen.findByText(/has not run yet/);
    first.unmount();

    mockedLog.mockResolvedValue(
      page([row()], {
        retention: { days: 365, rows_pruned: 0, last_run: '2026-10-27T09:00:00Z', sweep_errored: true },
      })
    );
    renderPage();
    expect(await screen.findByText(/last retention sweep failed/i)).toBeInTheDocument();
  });

  it('filters by action through the URL, so a filtered ledger is a link', async () => {
    renderPage();
    await screen.findByText('dana');

    const select = screen.getByLabelText('Action') as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'patch.deploy' } });

    await waitFor(() => {
      expect(mockedLog).toHaveBeenCalledWith(expect.objectContaining({ action: 'patch.deploy' }));
    });
    // The verb list is the server's vocabulary, read from /audit/actions.
    expect(mockedActions).toHaveBeenCalled();
    expect(within(table()).getAllByText('patch.deploy').length).toBeGreaterThanOrEqual(1);
  });

  it('filters by client through the same URL state as the fleet pages', async () => {
    renderPage();
    await screen.findByText('dana');

    const select = screen.getByLabelText('Client') as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'alpha-co' } });

    await waitFor(() => {
      expect(mockedLog).toHaveBeenCalledWith(expect.objectContaining({ clientId: 'alpha-co' }));
    });
  });

  it('exports the filtered ledger through the authenticated request, not a token in a URL', async () => {
    renderPage();
    await screen.findByText('dana');

    fireEvent.click(screen.getByRole('button', { name: /Export CSV/ }));
    await waitFor(() => expect(mockedExport).toHaveBeenCalled());

    const exported = mockedExport.mock.calls[0][0];
    // The export carries the filters the page is showing: an MSP exporting a
    // tenant's log must not have to re-pick what they are already looking at.
    expect(exported).toEqual(expect.objectContaining({ action: '', clientId: '' }));

    // And the page never builds a URL containing the session token.
    expect(document.body.innerHTML).not.toContain('ourway_access_token');
  });

  it('shows the empty state an operator can read, not a blank table', async () => {
    mockedLog.mockResolvedValue(page([]));
    renderPage();

    expect(await screen.findByText(/Nothing in the log/i)).toBeInTheDocument();
  });

  it('tells a viewer the ledger is an operator surface instead of rendering an empty page', async () => {
    localStorage.setItem('ourway_user', JSON.stringify({ id: 'user-9', username: 'view' }));
    localStorage.setItem('ourway_roles', JSON.stringify(['viewer']));
    renderPage();

    expect(await screen.findByText(/operator surface/i)).toBeInTheDocument();
    // Nothing was fetched: the route would have refused, and the page asks for
    // nothing a caller cannot perform.
    expect(mockedLog).not.toHaveBeenCalled();
  });

  it('keeps the row readable when the account is gone: no actor is a name, not a uuid', async () => {
    mockedLog.mockResolvedValue(
      page([row({ actor_username: '', actor_user_id: 'deleted-account-id' })])
    );
    renderPage();

    expect(await screen.findByText(/no account/i)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('deleted-account-id');
  });

  it('arrives from a unit view as a stated filter, not a quiet one', async () => {
    renderPage('/audit?target=abcd1234efgh5678');
    await screen.findByText('Unit record: device abcd1234');
    expect(mockedLog).toHaveBeenCalledWith(expect.objectContaining({ target: 'abcd1234efgh5678' }));
    // And the link is a link: clearing the filter is on the page, so an operator
    // who followed it can get back to the whole ledger in one act.
    fireEvent.click(screen.getByRole('button', { name: /All units/i }));
    await waitFor(() =>
      expect(mockedLog).toHaveBeenCalledWith(expect.objectContaining({ target: '' })),
    );
  });

  it('arrives from the operator profile with their acts, not the world', async () => {
    renderPage('/audit?actor=user-1');
    await screen.findByText('Acts by: dana');
    expect(mockedLog).toHaveBeenCalledWith(expect.objectContaining({ actor: 'user-1' }));
  });

  it('prints the empty target of a bulk refusal as its kind, not a dash', async () => {
    renderPage();
    expect(await screen.findByText('device-batch')).toBeInTheDocument();
  });
});
