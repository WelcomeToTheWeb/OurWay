import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ClientRollups } from './ClientRollups';
import { useFleetStore } from '../stores/fleet';
import type { ClientRollup } from '../api/clients';

/**
 * 2.3-5: the dashboard's per-client rows are the Clients ledger's rows.
 *
 * The agreement is not a coincidence and it is not recomputed here: both
 * surfaces subscribe to the `clientRollups` collection, which the server builds
 * with `rollupOf` from one set of scoped reads. What this test protects is the
 * console's half of that promise — the row prints the figures it was given, in
 * the units the ledger prints them in, and links into the tenant's filtered
 * sheet rather than a page the operator has to navigate to.
 */

const rollup = (over: Partial<ClientRollup> = {}): ClientRollup => ({
  client_id: 'c-1',
  name: 'Alpha Co',
  slug: 'alpha-co',
  disabled: false,
  units: 40,
  units_online: 12,
  alerts_open: 3,
  alerts_critical: 1,
  alerts_warning: 2,
  alerts_info: 0,
  patch_units: 40,
  patch_compliant: 37,
  patch_pending: 5,
  patch_critical: 2,
  patch_reboot_pending: 1,
  technicians: 2,
  ...over,
});

function renderRows(rollups: ClientRollup[]) {
  useFleetStore.getState().setCollection('clientRollups', rollups);
  return render(
    <MemoryRouter>
      <ClientRollups rollups={rollups} />
    </MemoryRouter>,
  );
}

describe('ClientRollups (dashboard per-client rows)', () => {
  beforeEach(() => {
    useFleetStore.getState().resetFleet();
  });

  it('prints the server figures without recomputing them', () => {
    renderRows([
      rollup(),
      rollup({
        client_id: 'c-2',
        name: 'Beta Co',
        slug: 'beta-co',
        units: 4,
        units_online: 4,
        alerts_open: 0,
        alerts_critical: 0,
        patch_units: 4,
        patch_compliant: 4,
        patch_pending: 0,
        patch_critical: 0,
      }),
    ]);

    const alpha = screen.getByText('Alpha Co').closest('tr');
    expect(alpha?.textContent).toContain('12/40');
    expect(alpha?.textContent).toContain('1 crit');
    // 37 of 40 compliant: the ledger's percentage, not a bar and not a count.
    expect(alpha?.textContent).toContain('93%');

    const beta = screen.getByText('Beta Co').closest('tr');
    expect(beta?.textContent).toContain('4/4');
    expect(beta?.textContent).toContain('100%');
  });

  it('links a row into the tenant filtered sheet', () => {
    renderRows([rollup()]);
    const link = screen.getByLabelText('Open the Alpha Co fleet') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/devices?client=alpha-co');
  });

  it('marks a paused tenant, because its units are visible and its fleet is not growing', () => {
    renderRows([rollup({ disabled: true })]);
    expect(screen.getByText('paused')).toBeInTheDocument();
  });

  it('says so when the installation has no tenants, rather than printing an empty table', () => {
    renderRows([]);
    expect(screen.getByText(/No clients yet/i)).toBeInTheDocument();
  });

  it('renders no raw locale key path', () => {
    renderRows([rollup()]);
    expect(document.body.textContent).not.toMatch(/\b(clients|devices|layout|common|sheet)\.[a-zA-Z.]+\b/);
  });
});
