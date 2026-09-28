import { NavLink } from 'react-router-dom';
import {
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

  const navItems = [
    { to: '/', label: 'Dashboard', icon: LayoutDashboard },
    { to: '/devices', label: 'Devices', icon: Server },
    { to: '/alerts', label: 'Alerts', icon: Bell },
    ...(hasAnyRole(['admin', 'manager', 'technician'])
      ? [{ to: '/sessions', label: 'Sessions', icon: Radio }]
      : []),
    ...(hasAnyRole(['admin', 'manager', 'technician'])
      ? [{ to: '/files', label: 'Files', icon: FolderUp }]
      : []),
    ...(hasAnyRole(['admin', 'manager', 'technician'])
      ? [{ to: '/patches', label: 'Patches', icon: Package }]
      : []),
    ...(hasAnyRole(['admin', 'manager'])
      ? [{ to: '/patch-policies', label: 'Policies', icon: Shield }]
      : []),
    ...(hasAnyRole(['admin', 'manager'])
      ? [{ to: '/users', label: 'Users', icon: Users }]
      : []),
    ...(hasRole('admin')
      ? [{ to: '/settings', label: 'Settings', icon: Settings }]
      : []),
    ...(hasRole('admin')
      ? [{ to: '/sso', label: 'SSO', icon: KeyRound }]
      : []),
    ...(hasAnyRole(['admin', 'manager'])
      ? [{ to: '/webhooks', label: 'Webhooks', icon: Webhook }]
      : []),
  ];

  return (
    <aside className="flex h-full w-64 flex-col border-r border-bg-border bg-bg-secondary">
      <div className="flex items-center gap-3 border-b border-bg-border px-5 py-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent">
          <MonitorSmartphone className="h-5 w-5 text-text-primary" />
        </div>
        <span className="text-lg font-semibold text-text-primary">OurWay</span>
      </div>

      <nav className="flex flex-1 flex-col gap-1 p-3" aria-label="Main navigation">
        {navItems.map((item) => {
          const Icon = item.icon;
          return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className={({ isActive }) =>
                [
                  'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-accent text-text-primary'
                    : 'text-text-secondary hover:bg-bg hover:text-text-primary',
                ].join(' ')
              }
            >
              <Icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          );
        })}
      </nav>
    </aside>
  );
}
