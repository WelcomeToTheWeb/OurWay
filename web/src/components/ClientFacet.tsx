import { useTranslation } from 'react-i18next';
import type { Client } from '../api/clients';
import { clientOptions, UNASSIGNED_SLUG } from '../utils/clientFilter';

/**
 * The tenant facet (2.3-5): a select in the filter row a ledger already has.
 *
 * It is a facet and not a page. The plan's density rule is the reason: an MSP
 * operator works a filtered ledger, they do not want a "client page" between
 * them and the alert they were called about. So this renders beside the search
 * box and the status select, and it writes `?client=<slug>` into the URL the
 * ledger already uses.
 *
 * `includeUnassigned` is the admin's answer to "which of these have I not
 * placed yet": a scoped caller has no unassigned units, and a facet option
 * that can only ever answer empty is a control that teaches people the console
 * is lying to them.
 *
 * Tenants are listed only when there is more than one to choose between: a
 * single-tenant installation, and every installation before this release, has
 * one fleet, and a facet with one option is noise on the filter row.
 */
export function ClientFacet({
  clients,
  value,
  onChange,
  includeUnassigned = false,
  label,
}: {
  clients: Client[];
  value: string;
  onChange: (slug: string) => void;
  includeUnassigned?: boolean;
  /** Overrides the aria-label when a page's filter row needs its own words. */
  label?: string;
}) {
  const { t } = useTranslation();
  const options = clientOptions(clients, includeUnassigned);
  if (options.length <= 1) return null;

  return (
    <select
      aria-label={label ?? t('clients.facetLabel')}
      className="select"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">{t('clients.facetAll')}</option>
      {options.map((o) => (
        <option key={o.slug} value={o.slug}>
          {o.slug === UNASSIGNED_SLUG ? t('clients.facetUnassigned') : o.name}
        </option>
      ))}
    </select>
  );
}
