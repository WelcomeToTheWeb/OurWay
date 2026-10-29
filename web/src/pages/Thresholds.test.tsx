import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within, cleanup } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { Thresholds } from './Thresholds';
import { AuthProvider } from '../auth/context';
import en from '../i18n/locales/en.json';
import de from '../i18n/locales/de.json';
import fr from '../i18n/locales/fr.json';
import type { ThresholdPreview, ThresholdRule, ThresholdSimulation } from '../api/thresholds';

vi.mock('../api/thresholds', () => ({
  getThresholds: vi.fn(),
  createThreshold: vi.fn(),
  updateThreshold: vi.fn(),
  deleteThreshold: vi.fn(),
  previewThresholds: vi.fn(),
  simulateThreshold: vi.fn(),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn() }));
vi.mock('../api/clients', () => ({ getClients: vi.fn() }));

import {
  createThreshold,
  deleteThreshold,
  getThresholds,
  previewThresholds,
  simulateThreshold,
  updateThreshold,
} from '../api/thresholds';
import { getDevices } from '../api/devices';
import { getClients } from '../api/clients';

const mockedRules = vi.mocked(getThresholds);
const mockedCreate = vi.mocked(createThreshold);
const mockedUpdate = vi.mocked(updateThreshold);
const mockedDelete = vi.mocked(deleteThreshold);
const mockedPreview = vi.mocked(previewThresholds);
const mockedSimulate = vi.mocked(simulateThreshold);
const mockedDevices = vi.mocked(getDevices);
const mockedClients = vi.mocked(getClients);

function rule(over: Partial<ThresholdRule> = {}): ThresholdRule {
  return {
    id: 'rule-client-cpu',
    scope_type: 'client',
    scope_value: 'client-alpha',
    metric: 'cpu',
    warning: 70,
    critical: 85,
    dedup_window_seconds: 0,
    enabled: true,
    scope_name: 'Alpha Co',
    editable: true,
    created_at: '2026-10-29T10:00:00Z',
    updated_at: '2026-10-29T10:00:00Z',
    ...over,
  };
}

function preview(over: Partial<ThresholdPreview> = {}): ThresholdPreview {
  return {
    device: { id: 'unit-a', name: 'unit-a', tags: ['prod'], client_id: 'client-alpha' },
    metrics: [
      {
        metric: 'cpu',
        candidates: [
          {
            id: 'rule-device-cpu',
            scope_type: 'device',
            scope_value: 'unit-a',
            warning: 50,
            critical: 65,
            rank: 4,
            source: 'rule',
          },
          {
            id: 'rule-tag-cpu',
            scope_type: 'tag',
            scope_value: 'prod',
            warning: 60,
            critical: 75,
            rank: 3,
            source: 'rule',
          },
          {
            id: 'rule-client-cpu',
            scope_type: 'client',
            scope_value: 'client-alpha',
            warning: 70,
            critical: 85,
            rank: 2,
            source: 'rule',
          },
          {
            id: '',
            scope_type: 'global',
            scope_value: '',
            warning: 80,
            critical: 90,
            rank: 1,
            source: 'built-in default',
          },
        ],
        applies: {
          id: 'rule-device-cpu',
          scope_type: 'device',
          scope_value: 'unit-a',
          warning: 50,
          critical: 65,
          rank: 4,
          source: 'rule',
        },
      },
      {
        metric: 'ram',
        candidates: [
          {
            id: '',
            scope_type: 'global',
            scope_value: '',
            warning: 85,
            critical: 95,
            rank: 1,
            source: 'built-in default',
          },
        ],
        applies: {
          id: '',
          scope_type: 'global',
          scope_value: '',
          warning: 85,
          critical: 95,
          rank: 1,
          source: 'built-in default',
        },
      },
    ],
    ...over,
  };
}

function simulation(over: Partial<ThresholdSimulation> = {}): ThresholdSimulation {
  return {
    device_id: 'unit-a',
    metric: 'cpu',
    value: 72,
    warning: 60,
    critical: 75,
    scope_type: 'tag',
    scope_value: 'prod',
    source: 'rule',
    would_fire: true,
    severity: 'warning',
    threshold: 60,
    message: 'CPU usage elevated: 72.0% (threshold: 60%)',
    ...over,
  };
}

function setRoles(roles: string[]) {
  localStorage.setItem('ourway_roles', JSON.stringify(roles));
}

function renderPage(path = '/thresholds') {
  return render(
    <AuthProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/thresholds" element={<Thresholds />} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>
  );
}

async function openForm() {
  fireEvent.click(await screen.findByRole('button', { name: /Add rule/ }));
  return screen.findByRole('form', { name: /Add rule/ });
}

describe('Thresholds — the rule ledger (2.4-5)', () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(['admin']);
    mockedRules.mockResolvedValue({
      rules: [rule()],
      defaults: [
        { metric: 'cpu', warning: 80, critical: 90 },
        { metric: 'ram', warning: 85, critical: 95 },
        { metric: 'disk', warning: 85, critical: 95 },
      ],
    });
    mockedDevices.mockResolvedValue([
      { id: 'unit-a', name: 'unit-a' },
      { id: 'unit-b', name: 'unit-b' },
    ] as never);
    mockedClients.mockResolvedValue([{ id: 'client-alpha', name: 'Alpha Co' }] as never);
    mockedPreview.mockResolvedValue({ data: preview() } as never);
    mockedSimulate.mockResolvedValue({ data: simulation() } as never);
    mockedCreate.mockResolvedValue(rule());
    mockedUpdate.mockResolvedValue(rule());
    mockedDelete.mockResolvedValue(undefined as never);
  });

  it('states the built-in floor in numbers, in a sentence the operator can act on', async () => {
    renderPage();
    const floor = await screen.findByText('Built-in floor');
    const band = floor.closest('div') as HTMLElement;
    // The numbers v2.3.x alerted with. A rule-less installation must be able to
    // see what it is falling back to without reading source.
    expect(within(band).getByText('80/90')).toBeInTheDocument();
    // Memory and disk share the shipped level: the band lists both.
    expect(within(band).getAllByText('85/95')).toHaveLength(2);
    expect(
      within(band).getByText(/A unit no rule covers is measured against these/)
    ).toBeInTheDocument();
  });

  it('lists each rule with the scope it names, its numbers, and the rule name', async () => {
    renderPage();
    const ledger = await screen.findByRole('table');
    const row = within(ledger).getByText('Alpha Co').closest('tr') as HTMLElement;
    expect(within(row).getByText('Tenant')).toBeInTheDocument();
    expect(within(row).getByText('CPU')).toBeInTheDocument();
    expect(within(row).getByText('70 / 85')).toBeInTheDocument();
    // A rule that says nothing about repeats says what the engine does, not "0".
    expect(within(row).getByText('engine default')).toBeInTheDocument();
  });

  it('adds a rule naming its scope, metric and levels', async () => {
    renderPage();
    const form = await openForm();
    const [scope] = within(form).getAllByRole('combobox');
    fireEvent.change(scope, { target: { value: 'tag' } });
    const tagInput = (await screen.findByRole('form', { name: /Add rule/ })).querySelector(
      'input[type="text"]'
    ) as HTMLInputElement;
    fireEvent.change(tagInput, { target: { value: 'prod' } });
    const selects = within(form).getAllByRole('combobox');
    // After switching scope: tag text, metric select, warning/critical/dedup numbers.
    const metricSelect = selects.filter(
      (el) => within(el as HTMLElement).queryByRole('option', { name: 'Disk' })
    )[0];
    fireEvent.change(metricSelect, { target: { value: 'disk' } });
    const numbers = within(form)
      .getAllByRole('spinbutton')
      .filter((el) => (el as HTMLInputElement).value !== '' && Number((el as HTMLInputElement).value) > 1);
    fireEvent.change(numbers[0], { target: { value: '55' } });
    fireEvent.change(numbers[1], { target: { value: '70' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(mockedCreate).toHaveBeenCalled());
    const sent = mockedCreate.mock.calls[0][0];
    expect(sent).toMatchObject({ scope_type: 'tag', scope_value: 'prod', metric: 'disk', warning: 55, critical: 70 });
  });

  it('refuses a rule the engine cannot use, at the page level, and the form stays open', async () => {
    mockedCreate.mockRejectedValueOnce(
      new Error('a rule for this scope and metric already exists: edit it instead of adding a second one')
    );
    renderPage();
    const form = await openForm();
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('edit it instead of adding a second one');
    expect(screen.getByRole('form', { name: /Add rule/ })).toBeInTheDocument();
  });

  it('carries the rule it removes in the confirmation, and posts the delete only on confirm', async () => {
    renderPage();
    const ledger = await screen.findByRole('table');
    fireEvent.click(within(ledger).getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('dialog');
    // "Delete this rule" with two scopes in the ledger is a guess.
    expect(dialog).toHaveTextContent('Delete this threshold rule?');
    expect(dialog).toHaveTextContent(/Tenant Alpha Co, CPU/);
    expect(mockedDelete).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete rule' }));
    await waitFor(() => expect(mockedDelete).toHaveBeenCalledWith('rule-client-cpu'));
  });

  it('disables the controls of a rule the caller cannot change', async () => {
    mockedRules.mockResolvedValue({
      rules: [rule({ id: 'rule-global-cpu', scope_type: 'global', scope_value: '', scope_name: '', editable: false })],
      defaults: [],
    });
    renderPage();
    const ledger = await screen.findByRole('table');
    const row = within(ledger).getByText('Global').closest('tr') as HTMLElement;
    // The server names a global rule in English; the console speaks the user's
    // language, so the phrase exists in all three locale files.
    expect(en.thresholds.globalName).toBe('every unit');
    expect(de.thresholds.globalName).toBeTruthy();
    expect(fr.thresholds.globalName).toBeTruthy();
    expect(within(row).getByText(en.thresholds.globalName)).toBeInTheDocument();
    expect(within(row).getByRole('button', { name: 'Delete' })).toBeDisabled();
    expect(within(row).getByRole('button', { name: 'Edit rule' })).toBeDisabled();
    expect(within(row).getByRole('checkbox')).toBeDisabled();
  });
});

describe('Thresholds — precedence preview and simulation (2.4-5)', () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
    localStorage.clear();
    setRoles(['admin']);
    mockedRules.mockResolvedValue({ rules: [], defaults: [] });
    mockedDevices.mockResolvedValue([
      { id: 'unit-a', name: 'unit-a' },
      { id: 'unit-b', name: 'unit-b' },
    ] as never);
    mockedClients.mockResolvedValue([{ id: 'client-alpha', name: 'Alpha Co' }] as never);
    mockedPreview.mockResolvedValue({ data: preview() } as never);
    mockedSimulate.mockResolvedValue({ data: simulation() } as never);
  });

  it('shows the rule that applies, then the whole chain in the order it wins', async () => {
    renderPage();
    const picker = await screen.findByRole('combobox', { name: /Choose a unit/ });
    fireEvent.change(picker, { target: { value: 'unit-a' } });
    const section = await screen.findByRole('region', { name: /What applies to a unit/ });
    await waitFor(() => expect(mockedPreview).toHaveBeenCalledWith('unit-a'));

    // Two landmarks per metric: the line that states what applies, and the chain of
    // every rule that was in the running. Inside each of them, real roles.
    const cpuApplies = section.querySelector('[data-applies="cpu"]') as HTMLElement;
    expect(cpuApplies).toHaveTextContent('50 / 65');
    expect(within(cpuApplies).getByText('Device')).toBeInTheDocument();
    // The chain: device, tag, tenant, then the built-in floor named as the floor.
    const chain = within(within(section).getByTestId('chain-cpu')).getAllByRole('listitem');
    expect(chain).toHaveLength(4);
    expect(chain[0]).toHaveTextContent('Device');
    expect(chain[1]).toHaveTextContent('Tag');
    expect(chain[2]).toHaveTextContent('Tenant');
    expect(chain[3]).toHaveTextContent('Built-in floor');
    // The preview is rules and levels only. A cache TTL from the store is not a
    // rule's repeat window, and a number the operator cannot act on does not
    // belong in this panel.
    expect(within(within(section).getByTestId('chain-cpu')).queryByText(/15\s*s/)).not.toBeInTheDocument();
  });

  it('says when a metric has no rule at all', async () => {
    renderPage();
    const picker = await screen.findByRole('combobox', { name: /Choose a unit/ });
    fireEvent.change(picker, { target: { value: 'unit-a' } });
    const section = await screen.findByRole('region', { name: /What applies to a unit/ });
    const ramApplies = section.querySelector('[data-applies="ram"]') as HTMLElement;
    expect(ramApplies).toHaveTextContent('85 / 95');
    expect(within(ramApplies).getByText('Built-in floor')).toBeInTheDocument();
    expect(within(within(section).getByTestId('chain-ram')).getAllByRole('listitem')).toHaveLength(1);
  });

  it('answers a value with the message the alert would carry, and the scope that answered', async () => {
    renderPage();
    const picker = await screen.findByRole('combobox', { name: /Choose a unit/ });
    fireEvent.change(picker, { target: { value: 'unit-a' } });
    const section = await screen.findByRole('region', { name: /What applies to a unit/ });
    const simValue = within(section)
      .getAllByRole('spinbutton')
      .find((el) => (el as HTMLInputElement).value === '85') as HTMLInputElement;
    fireEvent.change(simValue, { target: { value: '72' } });
    fireEvent.click(within(section).getByRole('button', { name: 'Simulate' }));
    await waitFor(() => expect(mockedSimulate).toHaveBeenCalledWith('unit-a', 'cpu', 72));
    const answer = await screen.findByRole('status');
    expect(answer).toHaveTextContent('CPU usage elevated: 72.0% (threshold: 60%)');
    expect(answer).toHaveTextContent(/Answered by Tag prod/);
  });

  it('states a value that fires nothing as nothing, not as silence', async () => {
    mockedSimulate.mockResolvedValueOnce({
      data: simulation({ would_fire: false, severity: '', message: 'CPU at 42.0% is under 80%', threshold: 80 }),
    } as never);
    renderPage();
    const picker = await screen.findByRole('combobox', { name: /Choose a unit/ });
    fireEvent.change(picker, { target: { value: 'unit-a' } });
    const section = await screen.findByRole('region', { name: /What applies to a unit/ });
    fireEvent.click(within(section).getByRole('button', { name: 'Simulate' }));
    const answer = await screen.findByRole('status');
    expect(answer).toHaveTextContent('No alert');
  });

  it('names what a refused preview answers, rather than showing an empty panel', async () => {
    mockedPreview.mockRejectedValueOnce(new Error('device not found'));
    renderPage();
    const picker = await screen.findByRole('combobox', { name: /Choose a unit/ });
    fireEvent.change(picker, { target: { value: 'unit-a' } });
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('device not found');
  });
});
