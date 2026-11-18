import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Layout } from './Layout';
import { rememberScope, resetScopeHeader } from '../api/client';
import { resetFleetPoller, useFleetStore } from '../stores/fleet';
import type { Client } from '../api/clients';

/**
 * 2.3-5: the shift bar states the tenant scope the server applied.
 *
 * The scope is decided server-side on every read (2.3-1) and the console has no
 * independent way to know it — which is exactly why the support ticket this
 * phase is measured by ("the technician cannot find the unit") is so easy to
 * misdiagnose. The bar is mounted on every page, so this is the one place the
 * answer is always in view, and it is the server's statement, not the
 * console's guess: it is captured from `X-OurWay-Client-Scope` on the reads.
 */

let roles: string[] = ['admin'];
let rateLimited = false;
const clients: Client[] = [
  { id: 'c-a', name: 'Alpha Co', slug: 'alpha-co', notes: '', disabled: false, created_at: '', updated_at: '' },
  { id: 'c-b', name: 'Beta Co', slug: 'beta-co', notes: '', disabled: false, created_at: '', updated_at: '' },
];

vi.mock('../auth/context', () => ({
  useAuth: () => ({
    user: { id: 'u', username: 'ops', roles },
    roles,
    accessToken: 't',
    logout: vi.fn(),
    hasAnyRole: (r: string[]) => r.some((x) => roles.includes(x)),
    hasRole: (r: string) => roles.includes(r),
  }),
}));
vi.mock('../hooks/useWebSocket', () => ({ useWebSocket: () => ({ connected: true }) }));
vi.mock('../hooks/useRateLimit', () => ({ useRateLimit: () => ({ limited: rateLimited, secondsLeft: 12 }) }));
vi.mock('../api/system', () => ({
  getServerVersion: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('../api/patching', () => ({
  listPolicies: vi.fn(() => Promise.resolve([])),
  getPatchOverview: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('../api/clients', () => ({
  getClients: vi.fn(async () => clients),
  getClientRollups: vi.fn(() => Promise.resolve([])),
}));
vi.mock('../api/devices', () => ({ getDevices: vi.fn(() => Promise.resolve([])) }));

/**
 * Deliver a scope header the way a read delivers one, and let React commit the
 * resulting re-render before the assertion runs: the header arrives from the
 * network, not from a click, so it is not inside React's own batch.
 */
function applyScope(value: string) {
  act(() => rememberScope({ 'x-ourway-client-scope': value }));
}

function renderBar() {
  return render(
    <MemoryRouter>
      <Layout />
    </MemoryRouter>,
  );
}

describe('Layout shift bar - tenant scope (2.3-5)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    roles = ['admin'];
    rateLimited = false;
    resetFleetPoller();
    resetScopeHeader();
    useFleetStore.getState().setCollection('clients', clients);
    useFleetStore.getState().setCollection('patchPolicies', []);
  });

  it('names the tenants the server said this read covers', () => {
    renderBar();
    // The header is a server answer, so the test hands the console one the way
    // a read does: through the capture point the interceptor uses.
    applyScope('c-a,c-b');
    expect(screen.getByText('Alpha Co, Beta Co')).toBeInTheDocument();
  });

  it('says "all tenants" to an admin, in words that do not read like a filter', () => {
    renderBar();
    applyScope('all');
    expect(screen.getByText('all tenants')).toBeInTheDocument();
  });

  it('says "no tenants" to an account with no grants, which is not the same as an empty fleet', () => {
    renderBar();
    applyScope('none');
    const value = screen.getByText('no tenants');
    expect(value).toBeInTheDocument();
    // The sentence the operator reads when a unit is missing. `closest('[title]')`
    // is not enough here: the wrapper around the chip carries the same sentence,
    // so that assertion passed with the ellipsis declared on the pill itself —
    // which paints nothing — and this file's only guard on that contract was
    // satisfied by the very markup that broke it. Pin the three things that
    // matter, separately: the text is held by a block container that is allowed
    // to shrink, the pill owns the title and must not claim the ellipsis, and
    // the sentence is spelled out for assistive tech.
    expect(value.className.split(' ')).toEqual(expect.arrayContaining(['block', 'min-w-0', 'truncate']));
    const pill = value.closest('.stamp');
    expect(pill).not.toBeNull();
    expect(pill!.getAttribute('title')).toContain('A unit you cannot find is usually a tenant you are not granted');
    expect(pill!.className).not.toContain('truncate');
    expect(document.querySelector('.sr-only')?.textContent).toContain('A unit you cannot find');
  });

  it('stays off the bar before the server has said anything, so the bar never states a scope it does not know', () => {
    renderBar();
    expect(screen.queryByText(/tenants$/)).toBeNull();
  });

  it('renders no raw locale key path on the bar', () => {
    renderBar();
    applyScope('c-a');
    expect(document.body.textContent).not.toMatch(/\b(layout|clients|devices|common)\.[a-zA-Z.]+\b/);
  });
});

/**
 * Two promises the e2e suite cannot make on its own.
 *
 * The rate-limited state depends on the server refusing a request, which CI
 * usually avoids by raising the per-minute budget — so it cannot be driven from
 * a test that only clicks, even when the limit does trip. And what a bar
 * *draws* at 390px needs a real layout engine, which jsdom does not have. So
 * these assert the markup contract, and `e2e/responsive.spec.ts` measures the
 * rendering. Both halves are needed: this file cannot notice that a 1px sr-only
 * box is invisible to an eye, and that file cannot notice that a class list says
 * `xl:flex`.
 */
describe('Layout shift bar - what the bar must never shed (DESIGN.md §8, §9)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    roles = ['admin'];
    rateLimited = true;
    resetFleetPoller();
    resetScopeHeader();
    useFleetStore.getState().setCollection('clients', clients);
    useFleetStore.getState().setCollection('patchPolicies', []);
  });

  it('warns about resting polls at every width, in a form that has no words to cut', () => {
    renderBar();
    applyScope('all');
    const sentence = screen.getByText(/Rate limited — polling rests for 12s/);
    const pill = sentence.closest('.stamp');
    expect(pill).not.toBeNull();
    const cls = pill!.className.split(' ');
    // The condition this chip states is the one that makes every other figure on
    // the bar stale, while the link indicator beside it still reads "Live" — so
    // it is the one instrument on the bar that yields to no breakpoint, and it
    // carries no visible text that could be truncated into "Li…".
    expect(cls).toContain('shrink-0');
    expect(pill!.className).not.toMatch(/\bhidden\b/);
    expect(pill!.className).not.toMatch(/\b(sm|md|lg|xl|2xl):flex\b/);
    expect(pill!.className).not.toContain('truncate');
    expect(pill!.className).not.toContain('min-w-0');
    expect(sentence.className).toContain('sr-only');
    expect(pill!.querySelector('svg')).not.toBeNull();
  });

  it('keeps the operator identified below the point where the name stops fitting', () => {
    renderBar();
    applyScope('all');
    // Shedding is allowed; vanishing is not. The avatar is here at every width,
    // the name beside it is shed by breakpoint, and where it is shed it is still
    // spoken to assistive tech — otherwise a shared console at phone width lets
    // someone sign out of, or act inside, a fleet that is not theirs.
    const initials = screen.getByText('OP');
    expect(initials.className).not.toMatch(/\bhidden\b/);
    const spoken = Array.from(document.querySelectorAll('.sr-only')).find((el) => el.textContent === 'ops');
    expect(spoken, 'the username is spelled out when it is not drawn').toBeDefined();
    expect(spoken!.className).toContain('sm:hidden');
    const drawn = Array.from(document.querySelectorAll('header span')).find(
      (el) => el.textContent === 'ops' && !el.className.includes('sr-only'),
    );
    expect(drawn, 'the username is drawn somewhere on the bar').toBeDefined();
    const column = drawn!.parentElement!;
    expect(column.className).toContain('hidden');
    expect(column.className).toContain('sm:flex');
  });
});
