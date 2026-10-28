import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { Download, ScrollText } from 'lucide-react';

import {
  auditQueryFromSearch,
  downloadAuditCSV,
  getAuditActions,
  getAuditLog,
  outcomeKey,
  scopeHeader,
  targetLabel,
  withAuditParams,
  type AuditLog,
} from '../api/audit';
import { clientFromSearch, getClients, withClientParam } from '../api/clients';
import { useAuth } from '../auth/context';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { Stamp, type StampTone } from '../components/Stamp';

/**
 * The outcome stamp's tone. `ok` is the quiet one; everything else is printed
 * in the colour the rest of the console uses for "this needs attention", so a
 * scanned page leads the eye to the rows that are not successes.
 */
function outcomeTone(outcome: string): StampTone {
  if (outcome === 'ok') return 'online';
  if (outcome === 'error') return 'alert';
  return 'stamp';
}

/**
 * 2.3-6 — the audit ledger.
 *
 * The page answers three questions in this order: **who, to what, and did it
 * work**. So the row leads with the actor and the verb, the target follows,
 * and the outcome is the last column before the timestamp — the one a reader
 * scans for when they are looking for the event that went wrong.
 *
 * Three things about the design are deliberate:
 *
 * **Refusals are shown, not hidden.** The default view includes them, and the
 * outcome column is a filter. A log that only shows successes is a log of what
 * the installation allowed, and the interesting events are the ones it did not.
 *
 * **Filters are URL state** (2.3-5's rule): `?action=device.delete&client=alpha`
 * is a link an MSP pastes into a ticket, and a support thread can name the
 * exact view. Client-side-only filters would be a page nobody can share.
 *
 * **Retention is printed beside the rows.** The window, the last sweep, and the
 * rows it removed. A log whose expiry is invisible is read as a complete
 * history, and the operators who discover otherwise stop trusting it.
 *
 * The page shows the rows the server sent. It does not filter them: a manager's
 * page is already scoped by the server, and a console-side filter over a
 * response the server truncated reads as "the log is short".
 */
export function Audit() {
  const { t, i18n } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const [rows, setRows] = useState<AuditLog[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [clients, setClients] = useState<{ id: string; name: string; slug: string }[]>([]);
  const [meta, setMeta] = useState({ retentionDays: 0, rowsPruned: 0, lastRun: '', errored: false });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [exporting, setExporting] = useState(false);
  const guide = useGuideWords(rows.length);

  const query = useMemo(() => auditQueryFromSearch(searchParams.toString()), [searchParams]);

  const { hasRole } = useAuth();
  // 2.3-6: the ledger is an operator surface — admin reads the installation,
  // managers read their tenants, and a viewer is refused by the route. The page
  // asks for nothing a caller cannot perform.
  const canRead = hasRole('admin') || hasRole('manager');

  const load = useCallback(async () => {
    if (!canRead) {
      setLoading(false);
      return;
    }
    setError('');
    try {
      const page = await getAuditLog({ ...query, limit: 200 });
      setRows(page.logs ?? []);
      setMeta({
        retentionDays: page.retention_days,
        rowsPruned: page.retention?.rows_pruned ?? 0,
        lastRun: page.retention?.last_run ?? '',
        errored: page.retention?.sweep_errored ?? false,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setLoading(false);
    }
  }, [canRead, query, t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!canRead) return;
    // The verb list comes from the server, so the filter menu is the vocabulary
    // the writers actually use, not a hand-written list that falls behind it.
    void getAuditActions()
      .then((r) => setActions([...r.actions].sort()))
      .catch(() => setActions([]));
    void getClients()
      .then((rows) => setClients(rows.map((c) => ({ id: c.id, name: c.name, slug: c.slug }))))
      .catch(() => setClients([]));
  }, [canRead]);

  const formatWhen = (iso: string) => {
    const when = new Date(iso);
    if (Number.isNaN(when.getTime())) return '—';
    return when.toLocaleString(i18n.language, { hour12: false });
  };

  if (loading) {
    return (
      <section aria-label="Audit" className="flex min-h-[40vh] items-center justify-center">
        <span className="cap cap-sm text-text-muted">{t('common.loading')}</span>
      </section>
    );
  }

  if (!canRead) {
    return (
      <section aria-label="Audit" className="panel p-4">
        <h1 className="panel-head mb-2">{t('audit.title')}</h1>
        <p className="cap text-text-secondary">{t('audit.noAccess')}</p>
      </section>
    );
  }

  const exportCSV = async () => {
    setExporting(true);
    setError('');
    try {
      await downloadAuditCSV(query);
    } catch (e) {
      setError(e instanceof Error ? e.message : t('audit.exportFailed'));
    } finally {
      setExporting(false);
    }
  };

  const clientFilter = clientFromSearch(searchParams.toString());
  const scope = scopeHeader();

  return (
    <section aria-label="Audit" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-2xl font-semibold text-text-primary">{t('audit.title')}</h1>
        <span className="cap cap-sm text-text-muted">{t('audit.subtitle')}</span>
        <span className="flex-1" />
        <button onClick={exportCSV} disabled={exporting} className="btn btn-quiet">
          <Download size={14} aria-hidden />
          {exporting ? t('audit.exporting') : t('audit.export')}
        </button>
      </div>

      {error && (
        <div role="alert" className="panel px-3 py-2">
          <span className="cap cap-sm text-[var(--stamp)]">{error}</span>
        </div>
      )}

      {/* The scope the server said it applied, and the retention the page is
      subject to, printed together: "you are reading the client's fleet, and
      rows older than this are gone". Both are the server's answer, read off
      the same response, so neither is a claim the page makes up. */}
      <div className="panel flex flex-wrap items-center gap-3 px-3 py-2">
        <span className="cap cap-sm text-text-muted">{t('audit.scope', { scope })}</span>
        <span className="cap cap-sm text-text-muted">
          {t('audit.retention', { days: meta.retentionDays })}
        </span>
        {meta.lastRun && meta.lastRun !== '0001-01-01T00:00:00Z' ? (
          <span className="cap cap-sm text-text-muted">
            {t('audit.lastSweep', { when: formatWhen(meta.lastRun), rows: meta.rowsPruned })}
          </span>
        ) : (
          <span className="cap cap-sm text-text-muted">{t('audit.sweepNever')}</span>
        )}
        {meta.errored && (
          <span role="status" className="cap cap-sm text-[var(--stamp)]">
            {t('audit.sweepFailed')}
          </span>
        )}
      </div>

      <div className="panel flex flex-wrap items-center gap-2 px-3 py-2">
        <label className="cap cap-sm text-text-muted" htmlFor="audit-action">
          {t('audit.filterAction')}
        </label>
        <select
          id="audit-action"
          className="input w-auto"
          value={query.action ?? ''}
          onChange={(e) =>
            setSearchParams(withAuditParams(searchParams.toString(), { ...query, action: e.target.value }))
          }
        >
          <option value="">{t('audit.filterActionAll')}</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>

        <label className="cap cap-sm text-text-muted" htmlFor="audit-client">
          {t('audit.filterClient')}
        </label>
        <select
          id="audit-client"
          className="input w-auto"
          value={clientFilter}
          onChange={(e) => setSearchParams(withClientParam(searchParams.toString(), e.target.value))}
        >
          <option value="">{t('audit.filterClientAll')}</option>
          {clients.map((c) => (
            <option key={c.id} value={c.slug}>
              {c.name}
            </option>
          ))}
        </select>

        <label className="cap cap-sm text-text-muted" htmlFor="audit-from">
          {t('audit.filterFrom')}
        </label>
        <input
          id="audit-from"
          type="date"
          className="input w-auto"
          value={query.from ? query.from.slice(0, 10) : ''}
          onChange={(e) =>
            setSearchParams(
              withAuditParams(searchParams.toString(), {
                ...query,
                from: e.target.value ? `${e.target.value}T00:00:00Z` : '',
              })
            )
          }
        />

        {/* The deep link (2.3-6's follow-up): `?target=<unit id>` is "the record
        of this unit", arriving from a unit view. It is stated on the page — a
        filter that is invisible in the URL only reads as "the log is short". */}
        {query.target && (
          <span className="flex items-center gap-1.5">
            <Stamp tone="service" title={query.target}>
              {t('audit.filterTargetFor', { target: `device ${query.target.slice(0, 8)}` })}
            </Stamp>
            <button
              onClick={() => setSearchParams(withAuditParams(searchParams.toString(), { ...query, target: '' }))}
              className="btn btn-quiet"
            >
              {t('audit.filterTargetClear')}
            </button>
          </span>
        )}

        {query.actor && (
          <span className="flex items-center gap-1.5">
            <Stamp tone="service" title={query.actor}>
              {t('audit.filterActorFor', {
                actor: rows[0]?.actor_username || `user ${query.actor.slice(0, 8)}`,
              })}
            </Stamp>
            <button
              onClick={() => setSearchParams(withAuditParams(searchParams.toString(), { ...query, actor: '' }))}
              className="btn btn-quiet"
            >
              {t('audit.filterActorClear')}
            </button>
          </span>
        )}

        {(query.action || query.clientId || query.from || query.target || query.actor) && (
          <button
            onClick={() => setSearchParams(withAuditParams(searchParams.toString(), {}))}
            className="btn btn-quiet"
          >
            {t('audit.clearFilters')}
          </button>
        )}
      </div>

      <section className="panel p-3">
        <GuideWordsHead
          total={rows.length}
          unit={t('sheet.inView')}
          first={rows[guide.first]?.action}
          last={rows[guide.last]?.action}
        />
        <div ref={guide.ref} className="max-h-[36rem] overflow-y-auto overflow-x-auto">
          <table className="ledger min-w-[960px]">
            <thead>
              <tr>
                <th className="w-40">{t('audit.col.actor')}</th>
                <th className="w-40">{t('audit.col.action')}</th>
                <th className="w-40">{t('audit.col.target')}</th>
                <th className="w-28 text-right">{t('audit.col.outcome')}</th>
                <th className="w-32">{t('audit.col.source')}</th>
                <th className="w-44 text-right">{t('audit.col.when')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} data-guide title={row.action}>
                  <td>
                    {/* The username is stored on the row, not joined from the
                    account: an operator reading a six-month-old entry wants the
                    person who acted then, and accounts are deleted. */}
                    <span className="cap cap-sm text-text-primary">
                      {row.actor_username || t('audit.noActor')}
                    </span>
                    {row.actor_roles && (
                      <span className="cap cap-sm block text-text-muted">{row.actor_roles}</span>
                    )}
                  </td>
                  <td className="cap cap-sm">{row.action}</td>
                  <td className="cap cap-sm" title={row.target_id}>
                    {targetLabel(row)}
                  </td>
                  <td className="text-right">
                    {/* The outcome column is why the ledger is readable: the
                    same row answers "did it work". */}
                    <Stamp tone={outcomeTone(row.outcome)} title={row.outcome}>
                      {t(outcomeKey(row.outcome))}
                    </Stamp>
                  </td>
                  <td className="cap cap-sm text-text-muted">{row.source_ip || '—'}</td>
                  <td className="cap cap-sm text-right text-text-muted">{formatWhen(row.created_at)}</td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="py-6 text-center">
                    <span className="cap cap-sm text-text-muted">{t('audit.empty')}</span>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <p className="cap cap-sm mt-2 flex items-center gap-2 text-text-muted">
          <ScrollText size={13} aria-hidden />
          {t('audit.footnote', { days: meta.retentionDays })}
        </p>
      </section>
    </section>
  );
}
