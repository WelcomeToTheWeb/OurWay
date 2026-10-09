import { NavLink } from 'react-router-dom';
import {
  Terminal,
  LayoutDashboard,
  Server,
  Bell,
  Settings,
  MonitorSmartphone,
  Users,
  Shield,
  Package,
  FolderUp,
  KeyRound,
  Webhook,
  Radio,
} from 'lucide-react';
import { useAuth } from '../auth/context';

export function Sidebar() {
  const { hasAnyRole, hasRole } = useAuth();

  const staff = hasAnyRole(['admin', 'manager', 'technician']);
  const managers = hasAnyRole(['admin', 'manager']);
  const isAdmin = hasRole('admin');

  // Grouped so twelve flat links read as three areas; a group with no
  // visible items is dropped for roles that can't use any of them.
  const groups: { heading: string; items: { to: string; label: string; icon: typeof LayoutDashboard }[] }[] = [
    {
      heading: 'Monitor',
      items: [
        { to: '/', label: 'Dashboard', icon: LayoutDashboard },
        { to: '/devices', label: 'Devices', icon: Server },
        { to: '/alerts', label: 'Alerts', icon: Bell },
      ],
    },
    {
      heading: 'Manage',
      items: [
        ...(staff ? [{ to: '/sessions', label: 'Sessions', icon: Radio }] : []),
        ...(staff ? [{ to: '/files', label: 'Files', icon: FolderUp }] : []),
        ...(staff ? [{ to: '/patches', label: 'Patches', icon: Package }] : []),
        ...(managers ? [{ to: '/patch-policies', label: 'Policies', icon: Shield }] : []),
        ...(managers ? [{ to: '/automation', label: 'Automation', icon: Terminal }] : []),
      ],
    },
    {
      heading: 'Admin',
      items: [
        ...(managers ? [{ to: '/users', label: 'Users', icon: Users }] : []),
        ...(managers ? [{ to: '/webhooks', label: 'Webhooks', icon: Webhook }] : []),
        ...(isAdmin ? [{ to: '/settings', label: 'Settings', icon: Settings }] : []),
        ...(isAdmin ? [{ to: '/sso', label: 'SSO', icon: KeyRound }] : []),
      ],
    },
  ].filter((g) => g.items.length > 0);

  return (
    <aside className="flex h-full w-64 flex-col border-r border-bg-border bg-bg-secondary">
      <div className="flex items-center gap-3 border-b border-bg-border px-5 py-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent">
          <MonitorSmartphone className="h-5 w-5 text-text-primary" />
        </div>
        <span className="text-lg font-semibold text-text-primary">OurWay</span>
      </div>

      <nav className="flex flex-1 flex-col gap-4 overflow-y-auto p-3" aria-label="Main navigation">
        {groups.map((group) => (
          <div key={group.heading} role="group" aria-label={group.heading} className="flex flex-col gap-1">
            <p className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wider text-text-muted">
              {group.heading}
            </p>
            {group.items.map((item) => {
              const Icon = item.icon;
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) =>
                    [
                      'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-accent text-white'
                        : 'text-text-secondary hover:bg-bg hover:text-text-primary',
                    ].join(' ')
                  }
                >
                  <Icon className="h-4 w-4" />
                  {item.label}
                </NavLink>
              );
            })}
          </div>
        ))}
      </nav>
    </aside>
  );
}
