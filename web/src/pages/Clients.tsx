import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';

import {
  assignClientDevices,
  clientFromSearch,
  createClient,
  deleteClient,
  getClients,
  getClientRollup,
  getClientUsers,
  setClientUsers,
  updateClient,
  withClientParam,
  type Client,
  type ClientRollup,
} from '../api/clients';
import { useAuth } from '../auth/context';
import { getDevices } from '../api/devices';
import { getUsers } from '../api/users';
import type { UserWithRoles } from '../auth/types';
import type { Device } from '../types/device';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { Stamp } from '../components/Stamp';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';

/**
 * 2.3-4 — the Clients ledger.
 *
 * The row is the question an MSP operator asks about a tenant: how many units,
 * how many awake, what is hurting them, what is unpatched, who can see this
 * fleet, when was any of it last seen. Every number comes from
 * `GET /api/clients/:id/rollup`, which the server computes from the same scoped
 * store reads the device, alert and patch ledgers run — so a rollup a
 * technician cannot reproduce by filtering the fleet to that client is a bug.
 *
 * Creating, assigning and deleting are admin acts on the server (`2.3-4`), and
 * the page asks for nothing a caller cannot perform: a manager reads the
 * clients they are granted, and sees no controls.
 */
export function Clients() {
  const { t, i18n } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const [clients, setClients] = useState<Client[]>([]);
  const [rollups, setRollups] = useState<Record<string, ClientRollup>>({});
  const [devices, setDevices] = useState<Device[]>([]);
  const [users, setUsers] = useState<UserWithRoles[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [newForm, setNewForm] = useState({ name: '', slug: '', notes: '' });
  const [openClient, setOpenClient] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const guide = useGuideWords(clients.length);

  const clientFilter = clientFromSearch(searchParams.toString());

  const load = useCallback(async () => {
    setError('');
    try {
      const clientRows = await getClients();
      setClients(clientRows);
      // The rollups are fetched per client, in parallel: the ledger row is the
      // rollup, so a ledger that renders before its numbers arrives is showing
      // a fleet count the operator cannot act on.
      const entries = await Promise.all(
        clientRows.map(async (c) => [c.id, await getClientRollup(c.id)] as const)
      );
      setRollups(Object.fromEntries(entries));
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
    // The assign panels need the fleet and the accounts; both are scoped reads,
    // so a technician's panel offers only units and colleagues they can see.
    void Promise.all([getDevices(), getUsers().catch(() => [])]).then(([d, u]) => {
      setDevices(d);
      setUsers(u as UserWithRoles[]);
    });
  }, [load]);

  const visible = useMemo(
    () => (clientFilter ? clients.filter((c) => c.slug === clientFilter) : clients),
    [clients, clientFilter]
  );

  const unitsOf = useCallback(
    (clientId: string) => devices.filter((d) => (d as Device & { client_id?: string }).client_id === clientId),
    [devices]
  );

  const handleCreate = async () => {
    if (!newForm.name) {
      setError(t('clients.needName'));
      return;
    }
    try {
      await createClient(newForm);
      setShowCreate(false);
      setNewForm({ name: '', slug: '', notes: '' });
      setError('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const handleToggleDisabled = async (client: Client) => {
    try {
      await updateClient(client.id, { name: client.name, slug: client.slug, notes: client.notes, disabled: !client.disabled });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const runDelete = async (client: Client) => {
    try {
      await deleteClient(client.id);
      setOpenClient(null);
      await load();
      // The fleet moved, and the operator needs the number, not a toast.
      setNotice(t('clients.deletedNotice', { name: client.name }));
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const handleAssignDevices = async (client: Client, deviceIds: string[]) => {
    try {
      await assignClientDevices(client.id, deviceIds);
      const [d, rows] = await Promise.all([getDevices(), getClients()]);
      setDevices(d);
      setClients(rows);
      const entries = await Promise.all(rows.map(async (c) => [c.id, await getClientRollup(c.id)] as const));
      setRollups(Object.fromEntries(entries));
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const handleAssignUsers = async (client: Client, userIds: string[]) => {
    try {
      await setClientUsers(client.id, userIds);
      const usersForClient = await getClientUsers(client.id).catch(() => []);
      setOpenUsers(client.id, usersForClient.map((u) => u.id));
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const { hasRole } = useAuth();
  const isAdmin = hasRole('admin');
  const [notice, setNotice] = useState('');
  const [assignedUsers, setAssignedUsers] = useState<Record<string, string[]>>({});

  function setOpenUsers(clientId: string, ids: string[]) {
    setAssignedUsers((prev) => ({ ...prev, [clientId]: ids }));
  }

  const formatWhen = (iso?: string | null) => {
    if (!iso) return t('clients.neverSeen');
    const when = new Date(iso);
    if (Number.isNaN(when.getTime())) return t('clients.neverSeen');
    return when.toLocaleString(i18n.language, { hour12: false });
  };

  if (loading) {
    return (
      <section aria-label="Clients" className="flex min-h-[40vh] items-center justify-center">
        <span className="cap cap-sm text-text-muted">{t('common.loading')}</span>
      </section>
    );
  }

  const open = openClient ? clients.find((c) => c.id === openClient) ?? null : null;
  const openRollup = open ? rollups[open.id] : undefined;
  const openUnits = open ? unitsOf(open.id) : [];

  return (
    <section aria-label="Clients" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-2xl font-semibold text-text-primary">{t('clients.title')}</h1>
        <span className="cap cap-sm text-text-muted">{t('clients.subtitle')}</span>
        <span className="flex-1" />
        {isAdmin && (
          <button onClick={() => setShowCreate((v) => !v)} className="btn btn-primary">
            {t('clients.newClient')}
          </button>
        )}
      </div>

      {notice && (
        <div role="status" className="panel px-3 py-2">
          <span className="cap cap-sm text-text-secondary">{notice}</span>
        </div>
      )}
      {error && (
        <div role="alert" className="panel px-3 py-2">
          <span className="cap cap-sm text-[var(--stamp)]">{error}</span>
        </div>
      )}

      {showCreate && (
        <div className="panel p-4">
          <h2 className="panel-head mb-3">{t('clients.newClient')}</h2>
          <div className="flex flex-wrap gap-3">
            <input
              className="input"
              aria-label={t('clients.name')}
              placeholder={t('clients.name')}
              value={newForm.name}
              onChange={(e) => setNewForm({ ...newForm, name: e.target.value })}
            />
            <input
              className="input"
              aria-label={t('clients.slug')}
              placeholder={t('clients.slugPlaceholder')}
              value={newForm.slug}
              onChange={(e) => setNewForm({ ...newForm, slug: e.target.value })}
            />
            <input
              className="input min-w-[16rem] flex-1"
              aria-label={t('clients.notes')}
              placeholder={t('clients.notes')}
              value={newForm.notes}
              onChange={(e) => setNewForm({ ...newForm, notes: e.target.value })}
            />
            <button onClick={handleCreate} className="btn btn-primary">
              {t('clients.create')}
            </button>
          </div>
          <p className="cap cap-sm mt-2 text-text-muted">{t('clients.slugHint')}</p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-1.5">
        <span className="cap cap-sm text-text-muted">{t('clients.filterLabel')}</span>
        <button
          onClick={() => setSearchParams(withClientParam(searchParams.toString(), ''))}
          className={`btn btn-quiet ${clientFilter === '' ? 'btn-primary' : ''}`}
        >
          {t('clients.filterAll')}
        </button>
        {clients.map((c) => (
          <button
            key={c.id}
            onClick={() => setSearchParams(withClientParam(searchParams.toString(), c.slug))}
            className={`btn btn-quiet ${clientFilter === c.slug ? 'btn-primary' : ''}`}
          >
            {c.name}
          </button>
        ))}
      </div>

      <section className="panel p-3">
        <GuideWordsHead
          total={visible.length}
          unit={t('sheet.inView')}
          first={visible[guide.first]?.name}
          last={visible[guide.last]?.name}
        />
        <div ref={guide.ref} className="max-h-[34rem] overflow-y-auto overflow-x-auto">
          <table className="ledger min-w-[880px]">
            <thead>
              <tr>
                <th>{t('clients.name')}</th>
                <th className="w-20 text-right">{t('clients.units')}</th>
                <th className="w-20 text-right">{t('clients.unitsOnline')}</th>
                <th className="w-24 text-right">{t('clients.alerts')}</th>
                <th className="w-28 text-right">{t('clients.patch')}</th>
                <th className="w-24 text-right">{t('clients.technicians')}</th>
                <th className="w-36">{t('clients.lastSeen')}</th>
                <th className="w-40 text-right">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((client) => {
                const r = rollups[client.id];
                return (
                  <tr key={client.id} data-guide title={client.name}>
                    <td>
                      {/* The row and the filter chip above carry the same client
                      name, so the row says what pressing it does: a keyboard or
                      screen-reader user choosing "Alpha Co" is choosing between
                      filtering the fleet and opening one tenant's detail, and
                      the accessible name is the only thing telling them apart. */}
                      <button
                        onClick={() => setOpenClient(openClient === client.id ? null : client.id)}
                        className="cap cap-sm text-left text-text-primary"
                        aria-expanded={openClient === client.id}
                        aria-label={t('clients.openRow', { name: client.name })}
                      >
                        {client.name}
                      </button>
                      <div className="flex flex-wrap gap-1.5">
                        <span className="ow-num text-[11px] text-text-muted">{client.slug}</span>
                        {client.disabled && <Stamp tone="service">{t('clients.disabled')}</Stamp>}
                      </div>
                    </td>
                    <td className="text-right">
                      <span className="ow-num text-[12px]">{r?.units ?? 0}</span>
                    </td>
                    <td className="text-right">
                      <span className="ow-num text-[12px] text-text-secondary">{r?.units_online ?? 0}</span>
                    </td>
                    <td className="text-right">
                      <span className="ow-num text-[12px]">{r?.alerts_open ?? 0}</span>
                      {(r?.alerts_critical ?? 0) > 0 && (
                        <Stamp tone="alert">{r?.alerts_critical}</Stamp>
                      )}
                    </td>
                    <td className="text-right">
                      <span className="ow-num text-[12px]">
                        {r ? `${r.patch_compliant}/${r.patch_units}` : '0/0'}
                      </span>
                      {(r?.patch_critical ?? 0) > 0 && (
                        <Stamp tone="alert">{r?.patch_critical}</Stamp>
                      )}
                    </td>
                    <td className="text-right">
                      <span className="ow-num text-[12px] text-text-secondary">{r?.technicians ?? 0}</span>
                    </td>
                    <td>
                      <span className="ow-num text-[11px] text-text-secondary">
                        {formatWhen(r?.last_seen)}
                      </span>
                    </td>
                    <td className="text-right">
                      <div className="flex justify-end gap-1.5">
                        {isAdmin && (
                          <>
                            <button onClick={() => void handleToggleDisabled(client)} className="btn-quiet btn">
                              {client.disabled ? t('clients.enable') : t('clients.disable')}
                            </button>
                            <button
                              onClick={() =>
                                setPending(
                                  pendingConfirm(t, 'deleteClient', () => void runDelete(client), {
                                    name: client.name,
                                    units: String(r?.units ?? 0),
                                  })
                                )
                              }
                              className="btn-quiet btn text-[var(--stamp)]"
                            >
                              {t('common.delete')}
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
              {visible.length === 0 && (
                <tr>
                  <td colSpan={8} className="text-center">
                    <span className="cap cap-sm text-text-muted">{t('clients.none')}</span>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {open && (
        <ClientPanel
          client={open}
          rollup={openRollup}
          units={openUnits}
          devices={devices}
          users={users}
          assignedUsers={assignedUsers[open.id] ?? null}
          onLoadedUsers={(ids) => setOpenUsers(open.id, ids)}
          onAssignDevices={(ids) => void handleAssignDevices(open, ids)}
          onAssignUsers={(ids) => void handleAssignUsers(open, ids)}
        />
      )}

      {pending && (
        <ConfirmSheet
          request={pending.request}
          onCancel={() => setPending(null)}
          onConfirm={() => {
            const run = pending.run;
            setPending(null);
            run();
          }}
        />
      )}
    </section>
  );
}

/**
 * The client detail panel: its units, the fleet that can be moved in (units the
 * caller can see, including unassigned ones for admin), and the accounts
 * granted this client.
 *
 * The assign list is the server's fleet, not the installation's: a manager
 * cannot be offered a unit they cannot see, and the server would refuse it.
 */
function ClientPanel({
  client,
  rollup,
  units,
  devices,
  users,
  assignedUsers,
  onLoadedUsers,
  onAssignDevices,
  onAssignUsers,
}: {
  client: Client;
  rollup?: ClientRollup;
  units: Device[];
  devices: Device[];
  users: UserWithRoles[];
  assignedUsers: string[] | null;
  onLoadedUsers: (ids: string[]) => void;
  onAssignDevices: (ids: string[]) => void;
  onAssignUsers: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string[]>(units.map((d) => d.id));
  const [grants, setGrants] = useState<string[]>(assignedUsers ?? []);

  useEffect(() => {
    if (assignedUsers === null) {
      void getClientUsers(client.id)
        .then((rows) => {
          setGrants(rows.map((u) => u.id));
          onLoadedUsers(rows.map((u) => u.id));
        })
        .catch(() => setGrants([]));
    }
  }, [assignedUsers, client.id, onLoadedUsers]);

  const candidates = devices.filter((d) => {
    const cid = (d as Device & { client_id?: string }).client_id;
    return cid === client.id || !cid;
  });

  return (
    <section className="panel p-4" aria-label={`${client.name} detail`}>
      <div className="mb-3 flex flex-wrap items-baseline gap-2">
        <h2 className="panel-head">{client.name}</h2>
        <span className="ow-num text-[11px] text-text-muted">{client.slug}</span>
        {client.notes && <span className="cap cap-sm text-text-secondary">{client.notes}</span>}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <div>
          <h3 className="cap cap-sm mb-2 text-text-muted">{t('clients.unitsTitle')}</h3>
          <div className="flex max-h-56 flex-col gap-1 overflow-y-auto">
            {candidates.map((d) => (
              <label key={d.id} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={selected.includes(d.id)}
                  onChange={(e) =>
                    setSelected((prev) => (e.target.checked ? [...prev, d.id] : prev.filter((x) => x !== d.id)))
                  }
                />
                <span className="cap cap-sm text-text-primary">{d.name}</span>
                <span className="ow-num text-[11px] text-text-muted">{d.hostname}</span>
              </label>
            ))}
            {candidates.length === 0 && (
              <span className="cap cap-sm text-text-muted">{t('clients.noUnits')}</span>
            )}
          </div>
          <button onClick={() => onAssignDevices(selected)} className="btn btn-primary mt-2">
            {t('clients.saveUnits')}
          </button>
          <p className="cap cap-sm mt-1 text-text-muted">
            {t('clients.assignHint', { count: selected.length })}
          </p>
        </div>

        <div>
          <h3 className="cap cap-sm mb-2 text-text-muted">{t('clients.accessTitle')}</h3>
          <div className="flex max-h-56 flex-col gap-1 overflow-y-auto">
            {users
              .filter((u) => !u.roles.includes('admin'))
              .map((u) => (
                <label key={u.id} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={grants.includes(u.id)}
                    onChange={(e) =>
                      setGrants((prev) => (e.target.checked ? [...prev, u.id] : prev.filter((x) => x !== u.id)))
                    }
                  />
                  <span className="cap cap-sm text-text-primary">{u.username}</span>
                  <span className="cap cap-sm text-text-muted">{u.roles.join(', ')}</span>
                </label>
              ))}
            {users.filter((u) => !u.roles.includes('admin')).length === 0 && (
              <span className="cap cap-sm text-text-muted">{t('clients.noUsers')}</span>
            )}
          </div>
          <button onClick={() => onAssignUsers(grants)} className="btn btn-primary mt-2">
            {t('clients.saveAccess')}
          </button>
          <p className="cap cap-sm mt-1 text-text-muted">{t('clients.accessHint')}</p>
        </div>
      </div>

      {rollup && (
        <div className="mt-4 flex flex-wrap gap-3">
          <Stamp tone="service">
            {t('clients.units')}: {rollup.units} ({rollup.units_online} {t('clients.online')})
          </Stamp>
          <Stamp tone={rollup.alerts_critical > 0 ? 'alert' : 'service'}>
            {t('clients.alerts')}: {rollup.alerts_open}
          </Stamp>
          <Stamp tone={rollup.patch_critical > 0 ? 'alert' : 'service'}>
            {t('clients.patch')}: {rollup.patch_compliant}/{rollup.patch_units}
          </Stamp>
          {rollup.patch_reboot_pending > 0 && <Stamp tone="service">{t('clients.rebootPending')}: {rollup.patch_reboot_pending}</Stamp>}
        </div>
      )}
    </section>
  );
}

export default Clients;
