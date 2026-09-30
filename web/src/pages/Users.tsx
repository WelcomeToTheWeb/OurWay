import { useEffect, useState } from 'react';
import { getUsers, getRoles, updateUserRoles, createUser, deleteUser } from '../api/users';
import type { Role, UserWithRoles } from '../auth/types';
import { useTranslation } from 'react-i18next';

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

  const load = async () => {
    try {
      const [usersData, rolesData] = await Promise.all([getUsers(), getRoles()]);
      setUsers(usersData);
      setRoles(rolesData);
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
    if (!confirm(`${t('common.delete')} ${user.username}?`)) return;
    try {
      await deleteUser(user.id);
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : t('common.error'));
    }
  };

  if (loading) {
    return <div className="p-6 text-text-secondary">{t('common.loading')}</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{t('users.title')}</h1>
          <p className="text-sm text-text-secondary">{users.length} {t('users.title').toLowerCase()}</p>
        </div>
        <button
          onClick={() => { setShowCreate(true); setFormError(''); }}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent-dark"
        >
          {t('users.addUser')}
        </button>
      </div>

      {/* Users Table - responsive: scroll on mobile, full table on desktop */}
      <div className="rounded-xl border border-bg-border bg-bg-card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[600px]">
            <thead className="border-b border-bg-border">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wider">{t('users.username')}</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wider">{t('users.email')}</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wider">{t('users.role')}</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wider">{t('common.created')}</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-text-muted uppercase tracking-wider">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-bg-border">
              {users.map((user) => (
                <tr key={user.id} className="hover:bg-bg-secondary transition-colors">
                  <td className="px-4 py-3 text-sm font-medium text-text-primary">{user.username}</td>
                  <td className="px-4 py-3 text-sm text-text-secondary">{user.email}</td>
                  <td className="px-4 py-3 text-sm">
                    <div className="flex flex-wrap gap-1">
                      {user.roles.map((role) => (
                        <span
                          key={role}
                          className="inline-flex items-center rounded-full bg-accent/10 px-2 py-0.5 text-xs font-medium text-accent"
                        >
                          {role}
                        </span>
                      ))}
                      {user.roles.length === 0 && (
                        <span className="text-xs text-text-muted">{t('users.noRoles')}</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-sm text-text-secondary">
                    {new Date(user.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-3">
                      <button
                        onClick={() => openEdit(user)}
                        className="text-sm text-accent hover:text-accent-dark transition-colors"
                      >
                        {t('common.edit')} Roles
                      </button>
                      <button
                        onClick={() => handleDelete(user)}
                        className="text-sm text-red-500 hover:text-red-400 transition-colors"
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
      </div>

      {/* Edit Roles Modal */}
      {editingUser && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
          aria-modal="true"
          aria-label="Edit user roles"
          onClick={() => setEditingUser(null)}
        >
          <div
            className="w-full max-w-md rounded-xl border border-bg-border bg-bg-card p-6 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-lg font-semibold text-text-primary mb-4">
              {t('common.edit')} Roles
            </h3>
            <div className="space-y-3 mb-6">
              {roles.map((role) => (
                <label
                  key={role.id}
                  className="flex cursor-pointer items-center gap-3 rounded-lg border border-bg-border p-3 transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={selectedRoles.includes(role.name)}
                    onChange={() => toggleRole(role.name)}
                    className="h-4 w-4 rounded border-bg-border accent-accent"
                  />
                  <div>
                    <span className="text-sm font-medium text-text-primary">{role.name}</span>
                    <span className="block text-xs text-text-muted">{role.description}</span>
                  </div>
                </label>
              ))}
            </div>
            <div className="flex gap-3">
              <button
                onClick={saveRoles}
                className="flex-1 rounded-lg bg-accent py-2 text-sm font-medium text-white transition-colors hover:bg-accent-dark"
              >
                {t('common.save')}
              </button>
              <button
                onClick={() => setEditingUser(null)}
                className="flex-1 rounded-lg border border-bg-border bg-bg py-2 text-sm font-medium text-text-primary transition-colors hover:bg-bg-secondary"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* Create User Modal */}
      {showCreate && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
          aria-modal="true"
          aria-label="Add user"
          onClick={() => setShowCreate(false)}
        >
          <div
            className="w-full max-w-md rounded-xl border border-bg-border bg-bg-card p-6 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-lg font-semibold text-text-primary mb-4">
              {t('users.addUser')}
            </h3>
            {formError && (
              <p className="mb-3 text-sm text-red-500">{formError}</p>
            )}
            <div className="space-y-3 mb-4">
              <input
                type="text"
                placeholder={t('users.username')}
                value={newForm.username}
                onChange={(e) => setNewForm({ ...newForm, username: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
              <input
                type="email"
                placeholder={t('users.email')}
                value={newForm.email}
                onChange={(e) => setNewForm({ ...newForm, email: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
              <input
                type="password"
                placeholder={t('auth.password')}
                value={newForm.password}
                onChange={(e) => setNewForm({ ...newForm, password: e.target.value })}
                className="w-full rounded-lg border border-bg-border bg-bg px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
            </div>
            <div className="space-y-3 mb-6">
              <span className="text-xs font-medium text-text-muted uppercase">{t('users.role')}</span>
              {roles.map((role) => (
                <label
                  key={role.id}
                  className="flex cursor-pointer items-center gap-3 rounded-lg border border-bg-border p-3 transition-colors hover:bg-bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={newRoles.includes(role.name)}
                    onChange={() => toggleNewRole(role.name)}
                    className="h-4 w-4 rounded border-bg-border accent-accent"
                  />
                  <span className="text-sm font-medium text-text-primary">{role.name}</span>
                </label>
              ))}
            </div>
            <div className="flex gap-3">
              <button
                onClick={handleCreate}
                className="flex-1 rounded-lg bg-accent py-2 text-sm font-medium text-white transition-colors hover:bg-accent-dark"
              >
                {t('common.save')}
              </button>
              <button
                onClick={() => setShowCreate(false)}
                className="flex-1 rounded-lg border border-bg-border bg-bg py-2 text-sm font-medium text-text-primary transition-colors hover:bg-bg-secondary"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
