import { useEffect, useState } from 'react';
import { getUsers, getRoles, updateUserRoles, createUser, deleteUser, getPendingIdentities, approveIdentity, mergeIdentity, type PendingIdentity } from '../api/users';
import type { Role, UserWithRoles } from '../auth/types';
import { useTranslation } from 'react-i18next';
import { Stamp } from '../components/Stamp';
import { ConfirmSheet } from '../components/ConfirmSheet';
import { GuideWordsHead, useGuideWords } from '../components/GuideWords';
import { pendingConfirm, type PendingConfirm } from '../utils/confirmSheet';
import { useAuth } from '../auth/context';

export function Users() {
  const { t } = useTranslation();
  const [users, setUsers] = useState<UserWithRoles[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingUser, setEditingUser] = useState<string | null>(null);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [newForm, setNewForm] = useState({ username: '', email: '', password: '' });
  const [newRoles, setNewRoles] = useState<string[]>([]);
  const [formError, setFormError] = useState('');
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const [identities, setIdentities] = useState<PendingIdentity[]>([]);
  const { hasRole } = useAuth();
  const isAdmin = hasRole('admin');
  const guide = useGuideWords(users.length);

  const load = async () => {
    try {
      const [usersData, rolesData] = await Promise.all([getUsers(), getRoles()]);
      setUsers(usersData);
      setRoles(rolesData);
      // 2.4-3: the identities an SSO provider asserted that point at an account that
      // already exists. Admin-only endpoint, and the read is admin-only too — a
      // tenant's manager has no business seeing someone else's account being claimed.
      if (isAdmin) {
        try {
          setIdentities(await getPendingIdentities());
        } catch {
          setIdentities([]);
        }
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const openEdit = (user: UserWithRoles) => {
    setEditingUser(user.id);
    setSelectedRoles(user.roles);
  };

  const saveRoles = async () => {
    if (!editingUser) return;
    try {
      await updateUserRoles(editingUser, selectedRoles);
      setEditingUser(null);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const toggleRole = (roleName: string) => {
    setSelectedRoles((prev) =>
      prev.includes(roleName)
        ? prev.filter((r) => r !== roleName)
        : [...prev, roleName]
    );
  };

  const toggleNewRole = (roleName: string) => {
    setNewRoles((prev) =>
      prev.includes(roleName)
        ? prev.filter((r) => r !== roleName)
        : [...prev, roleName]
    );
  };

  const handleCreate = async () => {
    if (!newForm.username || !newForm.email || !newForm.password) {
      setFormError(t('common.error'));
      return;
    }
    try {
      await createUser({ ...newForm, roles: newRoles });
      setShowCreate(false);
      setNewForm({ username: '', email: '', password: '' });
      setNewRoles([]);
      setFormError('');
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  const handleDelete = async (user: UserWithRoles) => {
    try {
      await deleteUser(user.id);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setPending(null);
    }
  };

  const handleApprove = async (identity: PendingIdentity) => {
    try {
      await approveIdentity(identity.id);
      setPending(null);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setPending(null);
    }
  };

  const handleMerge = async (identity: PendingIdentity) => {
    try {
      await mergeIdentity(identity.id);
      setPending(null);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
      setPending(null);
    }
  };

  if (loading) {
    return <div className="cap cap-sm p-6 text-text-muted">{t('common.loading')}</div>;
  }

  return (
    <div className="space-y-6">
      {/* A refusal is stated at page level. `formError` used to render only inside
          the modals, so a failed delete — or a rejected approve — left the page
          looking like it had done nothing, which is the silent failure the
          conventions refuse. */}
      {formError && (
        <div role="alert" className="panel px-3 py-2">
          <span className="cap cap-sm text-[var(--stamp)]">{formError}</span>
        </div>
      )}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px]">{t('users.title')}</h1>
          <p className="cap cap-sm mt-1 text-text-muted">
            {users.length} account{users.length === 1 ? '' : 's'} · roles decide what each console session may do
          </p>
        </div>
        <button
          onClick={() => { setShowCreate(true); setFormError(''); }}
          className="btn-primary"
        >
          {t('users.addUser')}
        </button>
      </div>

      {/* 2.4-3 · the identities an identity provider asserted that OurWay will not
          link by itself. Admin-only, stated plainly, with the account each assertion
          points at named beside it — an admin deciding "is this person my ops-admin"
          needs both sides of the claim on one line. */}
      {isAdmin && (
        <section className="panel overflow-hidden" aria-label={t('users.pendingTitle')}>
          <div className="border-b border-rule px-4 py-3">
            <h2 className="panel-head">{t('users.pendingTitle')}</h2>
            <p className="cap cap-sm mt-1 text-text-muted">{t('users.pendingBody')}</p>
          </div>
          {identities.length === 0 ? (
            <div className="cap cap-sm px-4 py-3 text-text-muted">{t('users.pendingNone')}</div>
          ) : (
            <div className="max-h-[18rem] overflow-y-auto overflow-x-auto">
              <table className="ledger min-w-[660px]">
                <thead>
                  <tr>
                    <th>{t('users.pendingAsserted')}</th>
                    <th>{t('users.pendingProvider')}</th>
                    <th>{t('users.pendingSubject')}</th>
                    <th>{t('users.pendingTarget')}</th>
                    <th className="w-52 text-right">{t('common.actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {identities.map((identity) => (
                    <tr key={identity.id} data-guide>
                      <td>
                        <span className="cap cap-sm text-text-primary">{identity.email}</span>
                        <span
                          className={`cap cap-sm block ${identity.email_verified ? 'text-[var(--online)]' : 'text-[var(--due)]'}`}
                        >
                          {identity.email_verified
                            ? t('users.pendingVerified')
                            : t('users.pendingUnverified')}
                        </span>
                      </td>
                      <td>
                        <span className="cap cap-sm">{identity.provider}</span>
                      </td>
                      <td>
                        <span className="ow-num block truncate text-[12px]" title={identity.subject}>
                          {identity.subject}
                        </span>
                      </td>
                      <td>
                        <span className="cap cap-sm text-text-primary">
                          {identity.link_target_username}
                        </span>
                        <div className="mt-1 flex flex-wrap gap-1.5">
                          {(identity.link_target_roles ?? []).map((role) => (
                            <Stamp key={role} tone="service">
                              {role}
                            </Stamp>
                          ))}
                        </div>
                      </td>
                      <td className="text-right">
                        <div className="flex justify-end gap-1.5">
                          <button
                            onClick={() =>
                              setPending(
                                pendingConfirm(t, 'approveIdentity', () => void handleApprove(identity), {
                                  email: identity.email,
                                })
                              )
                            }
                            className="btn-quiet btn"
                          >
                            {t('users.approve')}
                          </button>
                          <button
                            onClick={() =>
                              setPending(
                                pendingConfirm(t, 'mergeIdentity', () => void handleMerge(identity), {
                                  subject: identity.subject,
                                  name: identity.link_target_username ?? '',
                                })
                              )
                            }
                            className="btn-quiet btn text-[var(--service)]"
                          >
                            {t('users.merge')}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* Users Table - responsive: scroll on mobile, full table on desktop */}
      <section className="panel overflow-hidden" aria-label={t('users.title')}>
        <GuideWordsHead
          total={users.length}
          unit={t('sheet.inView')}
          first={users[guide.first]?.username}
          last={users[guide.last]?.username}
        />
        <div ref={guide.ref} className="max-h-[32rem] overflow-y-auto overflow-x-auto">
          <table className="ledger min-w-[600px]">
            <thead>
              <tr>
                <th>{t('users.username')}</th>
                <th>{t('users.email')}</th>
                <th>{t('users.role')}</th>
                <th className="w-28">{t('common.created')}</th>
                <th className="w-36 text-right">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <tr key={user.id} data-guide title={user.username}>
                  <td>
                    <span className="cap cap-sm text-text-primary">{user.username}</span>
                  </td>
                  <td>
                    <span className="ow-num block truncate text-[12px]" title={user.email}>
                      {user.email}
                    </span>
                  </td>
                  <td>
                    <div className="flex flex-wrap gap-1.5">
                      {user.roles.map((role) => (
                        <Stamp key={role} tone="service">
                          {role}
                        </Stamp>
                      ))}
                      {user.roles.length === 0 && (
                        <span className="cap cap-sm text-text-muted">{t('users.noRoles')}</span>
                      )}
                    </div>
                  </td>
                  <td>
                    <span className="ow-num text-[12px] text-text-secondary">
                      {new Date(user.created_at).toISOString().slice(0, 10)}
                    </span>
                  </td>
                  <td className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <button onClick={() => openEdit(user)} className="btn-quiet btn">
                        {t('common.edit')} roles
                      </button>
                      <button
                        onClick={() =>
                          setPending(pendingConfirm(t, 'deleteUser', () => void handleDelete(user), { name: user.username }))
                        }
                        className="btn-quiet btn text-[var(--stamp)]"
                      >
                        {t('common.delete')}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Edit Roles Modal */}
      {editingUser && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--scrim)] p-4"
          role="dialog"
          aria-modal="true"
          aria-label="Edit user roles"
          onClick={() => setEditingUser(null)}
        >
          <div
            className="w-full max-w-md panel p-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="panel-head mb-3">
              {t('common.edit')} roles
            </h3>
            <div className="space-y-3 mb-6">
              {roles.map((role) => (
                <label
                  key={role.id}
                  className="flex cursor-pointer items-center gap-3 border border-rule bg-[var(--field)] px-3 py-2.5 transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={selectedRoles.includes(role.name)}
                    onChange={() => toggleRole(role.name)}
                    className="h-4 w-4"
                    style={{ accentColor: 'var(--service)' }}
                  />
                  <div className="min-w-0">
                    <span className="cap cap-sm text-text-primary">{role.name}</span>
                    <span className="block truncate text-xs text-text-muted">{role.description}</span>
                  </div>
                </label>
              ))}
            </div>
            <div className="flex gap-3">
              <button onClick={saveRoles} className="btn-primary flex-1">
                {t('common.save')}
              </button>
              <button onClick={() => setEditingUser(null)} className="btn flex-1">
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* Create User Modal */}
      {showCreate && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--scrim)] p-4"
          role="dialog"
          aria-modal="true"
          aria-label="Add user"
          onClick={() => setShowCreate(false)}
        >
          <div
            className="w-full max-w-md panel p-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="panel-head mb-3">{t('users.addUser')}</h3>
            {formError && (
              <p className="cap cap-sm mb-3 text-[var(--error)]">{formError}</p>
            )}
            <div className="space-y-3 mb-4">
              <label className="block">
                <span className="field-label">{t('users.username')}</span>
                <input
                  type="text"
                  value={newForm.username}
                  onChange={(e) => setNewForm({ ...newForm, username: e.target.value })}
                  className="input mt-1 w-full"
                />
              </label>
              <label className="block">
                <span className="field-label">{t('users.email')}</span>
                <input
                  type="email"
                  value={newForm.email}
                  onChange={(e) => setNewForm({ ...newForm, email: e.target.value })}
                  className="input mt-1 w-full"
                />
              </label>
              <label className="block">
                <span className="field-label">{t('auth.password')}</span>
                <input
                  type="password"
                  value={newForm.password}
                  onChange={(e) => setNewForm({ ...newForm, password: e.target.value })}
                  className="input mt-1 w-full"
                />
              </label>
            </div>
            <div className="mb-5 space-y-2">
              <span className="field-label">{t('users.role')}</span>
              {roles.map((role) => (
                <label
                  key={role.id}
                  className="flex cursor-pointer items-center gap-3 border border-rule bg-[var(--field)] px-3 py-2.5 transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={newRoles.includes(role.name)}
                    onChange={() => toggleNewRole(role.name)}
                    className="h-4 w-4"
                    style={{ accentColor: 'var(--service)' }}
                  />
                  <span className="cap cap-sm text-text-primary">{role.name}</span>
                </label>
              ))}
            </div>
            <div className="flex gap-3">
              <button onClick={handleCreate} className="btn-primary flex-1">
                {t('common.save')}
              </button>
              <button onClick={() => setShowCreate(false)} className="btn flex-1">
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmSheet
        request={pending?.request ?? null}
        onCancel={() => setPending(null)}
        onConfirm={() => pending?.run()}
      />
    </div>
  );
}
