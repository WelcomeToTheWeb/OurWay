import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import type { ClientRollup } from '../api/clients';
import { Stamp } from '../components/Stamp';

/**
 * Per-client rows on the dashboard (2.3-5).
 *
 * The rule this section exists to obey is 2.2-4's: one figure, one source. The
 * numbers come from `GET /api/clients/rollups`, which is the same read the
 * Clients ledger renders, so a tenant's unit count, alert count and patch
 * compliance on the front page cannot disagree with the ledger an operator
 * acknowledges from. The arithmetic lives on the server, in `rollupOf`, and
 * nothing here recomputes it.
 *
 * Rows link into the tenant's filtered sheet (`/devices?client=<slug>`), so
 * the row is a way to *work* a tenant, not a poster of it.
 */
export function ClientRollups({ rollups }: { rollups: ClientRollup[] }) {
  const { t } = useTranslation();

  return (
    <section className="panel overflow-hidden" aria-label={t('clients.dashboardTitle')}>
      <div className="panel-head">
        <h2 className="cap cap-lg text-text-muted">{t('clients.dashboardTitle')}</h2>
        <Link to="/clients" className="cap-sm cap text-[var(--service)] hover:underline">
          {t('clients.dashboardOpen')}
        </Link>
      </div>
      {rollups.length === 0 ? (
        <p className="px-3 py-3 text-[13px] text-text-secondary">{t('clients.none')}</p>
      ) : (
        <table className="ledger">
          <thead>
            <tr>
              <th>{t('clients.colTenant')}</th>
              <th className="text-right">{t('clients.colUnits')}</th>
              <th className="text-right">{t('clients.colAlerts')}</th>
              <th className="text-right">{t('clients.colPatch')}</th>
            </tr>
          </thead>
          <tbody>
            {rollups.map((r) => {
              const pct = r.patch_units > 0 ? Math.round((r.patch_compliant / r.patch_units) * 100) : 100;
              return (
                <tr key={r.client_id} className="text-[13px]">
                  <td>
                    <Link
                      to={`/devices?client=${r.slug}`}
                      className="font-semibold text-[var(--service)] hover:underline"
                      aria-label={t('clients.dashboardRowAria', { name: r.name })}
                    >
                      {r.name}
                    </Link>
                    {r.disabled && (
                      <Stamp tone="offline" title={t('clients.disabledTitle')}>
                        {t('clients.disabled')}
                      </Stamp>
                    )}
                  </td>
                  <td className="ow-num text-right text-text-primary">
                    {r.units_online}
                    <span className="text-text-muted">/{r.units}</span>
                  </td>
                  <td className="ow-num text-right">
                    {r.alerts_critical > 0 ? (
                      <span className="text-[var(--error)]">{r.alerts_critical} crit</span>
                    ) : r.alerts_open > 0 ? (
                      <span className="text-[var(--due)]">{r.alerts_open}</span>
                    ) : (
                      <span className="text-text-muted">0</span>
                    )}
                  </td>
                  <td className="ow-num text-right">
                    {r.patch_critical > 0 ? (
                      <span className="text-[var(--error)]">{pct}%</span>
                    ) : pct < 100 ? (
                      <span className="text-[var(--due)]">{pct}%</span>
                    ) : (
                      <span className="text-[var(--online)]">100%</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
