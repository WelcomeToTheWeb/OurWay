import client from './client';
import { scopeHeader } from './clients';

/**
 * 2.3-6 — the audit ledger's console surface.
 *
 * Two things shape this file.
 *
 * **The server decides scope; the console asks for it.** The page never filters
 * rows client-side, because the server's answer for a manager already excludes
 * every row for a client they are not granted, and a console-side filter over
 * a page the server truncated would read as "the log is short". `scopeHeader()`
 * from 2.3-4's client module says which scope the server applied, so the page
 * can state it.
 *
 * **Retention travels with the rows.** The response carries the window and the
 * last sweep's counters, and the page prints them: a log that quietly expires
 * is read as a complete history, and the operators who find out otherwise are
 * the ones who stop trusting it.
 */

export interface AuditLog {
  id: string;
  actor_user_id: string;
  actor_username: string;
  actor_roles: string;
  actor_provider: string;
  action: string;
  target_type: string;
  target_id: string;
  client_id: string;
  outcome: string;
  details: string;
  source_ip: string;
  created_at: string;
}

export interface AuditPage {
  logs: AuditLog[];
  total: number;
  scope: string;
  retention_days: number;
  limit: number;
  offset: number;
  retention: {
    days: number;
    rows_pruned: number;
    last_run: string;
    sweep_errored: boolean;
  };
}

export interface AuditQuery {
  action?: string;
  clientId?: string;
  actor?: string;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
  /** "The record of this unit" — the ledger's deep link from a unit view. */
  target?: string;
}

function params(q: AuditQuery): URLSearchParams {
  const p = new URLSearchParams();
  if (q.action) p.set('action', q.action);
  if (q.clientId) p.set('client_id', q.clientId);
  if (q.actor) p.set('actor', q.actor);
  if (q.target) p.set('target', q.target);
  if (q.from) p.set('from', q.from);
  if (q.to) p.set('to', q.to);
  p.set('limit', String(q.limit ?? 200));
  if (q.offset && q.offset > 0) p.set('offset', String(q.offset));
  return p;
}

export const getAuditLog = (q: AuditQuery = {}) =>
  client.get<AuditPage>(`/audit?${params(q).toString()}`).then((r) => r.data);

/** The verbs the server records, so the filter list is the server's vocabulary. */
export const getAuditActions = () =>
  client.get<{ actions: string[]; retention_days: number }>('/audit/actions').then((r) => r.data);

/**
 * The CSV export.
 *
 * A plain link is the obvious shape — `href="/api/audit?format=csv"` and let
 * the browser download it — and it is the one shape that cannot work here:
 * the ledger is authenticated, and a link carries no `Authorization` header.
 * The alternative that does work, `?access_token=<token>` in the URL, puts a
 * live bearer token in the browser history, the proxy access log, the
 * `Referer` header and any toolbar that previews URLs. The audit log is the
 * most sensitive table in the product; it is not going to be reached through a
 * URL that leaks the session.
 *
 * So the export is an authenticated request that resolves to a Blob, and the
 * download is created from the response. The cost is that the whole export is
 * buffered before the operator sees a file, which is the right trade at a page
 * of 1 000 rows, and the reason the server caps `limit` at 1 000: the export
 * is bounded by design, not by patience.
 */
export async function downloadAuditCSV(q: AuditQuery = {}): Promise<void> {
  const response = await client.get(`/audit?${params(q).toString()}`, {
    params: { format: 'csv' },
    responseType: 'blob',
  });
  const blob = new Blob([response.data], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = 'ourway-audit-log.csv';
  document.body.appendChild(link);
  link.click();
  link.remove();
  // The object URL outlives the click in every browser, so an export left
  // un-revoked accumulates a copy of the ledger in memory for the life of the
  // tab.
  URL.revokeObjectURL(url);
}

export { scopeHeader };

/** URL state for the ledger's filters (2.3-5's rule: a view is a link). */
export function auditQueryFromSearch(search: string): AuditQuery {
  const p = new URLSearchParams(search);
  return {
    action: p.get('action') ?? '',
    clientId: p.get('client') ?? '',
    from: p.get('from') ?? '',
    to: p.get('to') ?? '',
    // The deep link (2.3-6's follow-up): `?target=<unit id>` is "the record of
    // this unit", so the link from a unit view is a link the operator can paste
    // into a ticket, and a reload keeps it. Same rule as every other filter in
    // this release — the view is URL state, not component state.
    target: p.get('target') ?? '',
    // The deep link from the operator's own profile: "my acts in the ledger".
    actor: p.get('actor') ?? '',
  };
}

export function withAuditParams(search: string, q: AuditQuery): string {
  const p = new URLSearchParams(search);
  const set = (key: string, value?: string) => {
    if (value) p.set(key, value);
    else p.delete(key);
  };
  set('action', q.action);
  set('client', q.clientId);
  set('from', q.from);
  set('to', q.to);
  set('target', q.target);
  set('actor', q.actor);
  const out = p.toString();
  return out ? `?${out}` : '';
}

/** The outcome column, in the words the page uses. */
export function outcomeKey(outcome: string): string {
  switch (outcome) {
    case 'ok':
      return 'audit.outcomes.ok';
    case 'refused':
      return 'audit.outcomes.refused';
    case 'not_found':
      return 'audit.outcomes.notFound';
    case 'error':
      return 'audit.outcomes.error';
    default:
      return 'audit.outcomes.unknown';
  }
}

/**
 * The record's target, in one readable phrase.
 *
 * The server stores a type and an id; a raw uuid in a ledger row is not
 * readable, and the page's job is to make the row answerable at a glance. The
 * id stays in the `title` attribute so the exact value is reachable.
 */
export function targetLabel(row: AuditLog): string {
  if (!row.target_id) return row.target_type;
  return `${row.target_type} ${row.target_id.slice(0, 8)}`;
}
