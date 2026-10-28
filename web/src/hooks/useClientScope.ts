import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useFleet } from './useFleet';
import { useAuth } from '../auth/context';
import type { Client } from '../api/clients';
import type { Device } from '../types/device';
import {
  clientSlugFromSearch,
  clientSlugOfDevice,
  deviceIdsForClient,
  withClientSlug,
} from '../utils/clientFilter';

/**
 * The tenant filter of a ledger (2.3-5), in one hook.
 *
 * Every ledger needs the same four things to scope its rows by tenant: the
 * tenant list (names, for the facet and the column), the unit list (because an
 * alert, a session, an update and a transfer name a *device*, not a tenant),
 * the slug in the URL, and a writer for it. All four come from the shared
 * collections, so a page adding a tenant filter adds no read of its own, and
 * the rows it drops are dropped by the same mapping every surface uses.
 *
 * `includeUnassigned` is the admin's: a caller with client grants has no
 * unassigned units at all, so offering them the option would promise a filter
 * that can only answer empty.
 */
export function useClientScope(): {
  slug: string;
  setSlug: (slug: string) => void;
  scopedDeviceIds: Set<string>;
  clients: Client[];
  devices: Device[];
  includeUnassigned: boolean;
  /** The slug plus `unassigned` for admin: what a device with no tenant shows. */
  slugOf: (deviceId: string | null | undefined) => string;
} {
  const [params, setParams] = useSearchParams();
  const { hasAnyRole } = useAuth();
  const { data: clients } = useFleet('clients');
  const { data: devices } = useFleet('devices');
  const includeUnassigned = hasAnyRole(['admin']);
  const slug = clientSlugFromSearch(params);

  const setSlug = useCallback(
    (next: string) => setParams(withClientSlug(params, next), { replace: true }),
    [params, setParams],
  );

  const scopedDeviceIds = useMemo(() => deviceIdsForClient(devices, slug, clients), [devices, slug, clients]);

  /**
   * The tenant slug of a unit, for a ledger whose rows name a device. An
   * unknown unit id is the sentinel: the row belongs to a unit this caller was
   * never shown, which is the same answer the server gave by omitting it.
   */
  const slugOf = useCallback(
    (deviceId: string | null | undefined) =>
      clientSlugOfDevice({ client_id: devices.find((d) => d.id === deviceId)?.client_id }, clients),
    [devices, clients],
  );

  return { slug, setSlug, scopedDeviceIds, clients, devices, includeUnassigned, slugOf };
}
