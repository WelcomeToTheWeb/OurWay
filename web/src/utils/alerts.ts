import type { Alert } from '../types/alert';

/**
 * What "open" means, in one place (2.2-4).
 *
 * The finding: the dashboard tile counted units whose status is `alert`, the
 * ledger counted the page it had just fetched with the server's filters
 * applied, and both were labelled "open alerts". An acknowledged alert is
 * open — it is unresolved, and a viewer's badge ignored it.
 *
 * Open is **unresolved**, acknowledged or not. Acknowledging is a sign-off on
 * a signal a human has looked at; resolving is what closes it.
 */
export const isAlertOpen = (alert: Alert) => !alert.resolved;

/** Open alerts, counted once, the way every surface must count them. */
export const openAlertCount = (alerts: readonly Alert[]) => alerts.filter(isAlertOpen).length;

/**
 * The ledger's population after its filters. Exported because the count chips
 * and the rows must come from the same call — the chips are not a second
 * measurement of the fleet.
 */
export function filterAlerts(
  alerts: readonly Alert[],
  filters: { severity?: string; status?: string; search?: string; deviceIds?: Set<string> }
): Alert[] {
  const { severity = 'all', status = 'all', search = '', deviceIds } = filters;
  const q = search.trim().toLowerCase();
  return alerts.filter((a) => {
    // 2.3-5: the tenant facet. An alert names a device, so scoping it is a
    // lookup on the tenant's unit set - never a request per row.
    if (deviceIds && !deviceIds.has(a.device_id)) return false;
    if (severity !== 'all' && a.severity !== severity) return false;
    if (status === 'unresolved' && a.resolved) return false;
    if (status === 'resolved' && !a.resolved) return false;
    if (q) {
      const haystack = `${a.device_name} ${a.message} ${a.metric}`.toLowerCase();
      if (!haystack.includes(q)) return false;
    }
    return true;
  });
}
