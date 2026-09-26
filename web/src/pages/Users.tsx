import { useEffect, useState } from 'react';
import { getUsers, getRoles, updateUserRoles } from '../api/users';
import type { Role, UserWithRoles } from '../auth/types';
import { useTranslation } from 'react-i18next';

export function Users() {
  const { t } = useTranslation();
  const [users, setUsers] = useState<UserWithRoles[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingUser, setEditingUser] = useState<string | null>(null);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);

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
    await updateUserRoles(editingUser, selectedRoles);
    setEditingUser(null);
    await load();
  };

  const toggleRole = (roleName: string) => {
    setSelectedRoles((prev) =>
      prev.includes(roleName)
        ? prev.filter((r) => r !== roleName)
        : [...prev, roleName]
    );
  };

  if (loading) {
    return <div className="p-6 text-text-secondary">{t('common.loading')}</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-text-primary">{t('users.title')}</h1>
        <p className="text-sm text-text-secondary">{users.length} {t('users.title').toLowerCase()}</p>
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
                        <span className="text-xs text-text-muted">No roles</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-sm text-text-secondary">
                    {new Date(user.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => openEdit(user)}
                      className="text-sm text-accent hover:text-accent-dark transition-colors"
                    >
                      {t('common.edit')} Roles
                    </button>
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
    </div>
  );
}
