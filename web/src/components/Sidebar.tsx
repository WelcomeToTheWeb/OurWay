import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Server,
  Building2,
  ScrollText,
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
  Gauge,
  ScreenShare,
} from 'lucide-react';
import { useAuth } from '../auth/context';
import { useThemeStore, type Theme } from '../stores/themeStore';

/**
 * The nav rail is the console's one committed field of colour: a dark slate
 * plate that stays dark in both renditions. Navigation is the category
 * standard — labelled groups, icon + text rows, one obviously active item.
 */
type GroupItem = {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
  /** The g-then-key shortcut that jumps here. */
  go?: string;
  roles?: string[];
};

const groups: { heading: string; items: GroupItem[] }[] = [
  {
    heading: 'Monitor',
    items: [
      { to: '/', label: 'Dashboard', icon: LayoutDashboard, go: 'g h' },
      { to: '/devices', label: 'Devices', icon: Server, go: 'g d' },
      { to: '/clients', label: 'Clients', icon: Building2, go: 'g c' }, // 2.3-4: the tenant axis
      {
        to: '/audit',
        label: 'Audit',
        icon: ScrollText,
        go: 'g i', // 2.3-6: the ledger
        roles: ['admin', 'manager'], // 2.3-6: the ledger is an operator surface
      },
      { to: '/alerts', label: 'Alerts', icon: Bell, go: 'g a' },
    ],
  },
  {
    heading: 'Manage',
    items: [
      { to: '/sessions', label: 'Sessions', icon: Radio, go: 'g r', roles: ['admin', 'manager', 'technician'] },
      { to: '/recordings', label: 'Recordings', icon: ScreenShare, roles: ['admin', 'manager', 'technician'] }, // 2.4-8
      { to: '/files', label: 'Files', icon: FolderUp, go: 'g f', roles: ['admin', 'manager', 'technician'] },
      { to: '/patches', label: 'Patches', icon: Package, go: 'g p', roles: ['admin', 'manager', 'technician'] },
      { to: '/patch-policies', label: 'Policies', icon: Shield, go: 'g e', roles: ['admin', 'manager'] },
      { to: '/thresholds', label: 'Alert rules', icon: Gauge, roles: ['admin', 'manager'] }, // 2.4-5: threshold scopes
    ],
  },
  {
    heading: 'Admin',
    items: [
      { to: '/users', label: 'Users', icon: Users, go: 'g u', roles: ['admin', 'manager'] },
      { to: '/webhooks', label: 'Webhooks', icon: Webhook, roles: ['admin', 'manager'] },
      { to: '/settings', label: 'Settings', icon: Settings, go: 'g s', roles: ['admin'] },
      { to: '/sso', label: 'SSO', icon: KeyRound, roles: ['admin'] },
    ],
  },
];

const THEMES: { value: Theme; label: string }[] = [
  { value: 'light', label: 'Day' },
  { value: 'dark', label: 'Night' },
  { value: 'system', label: 'Auto' },
];

export function Sidebar() {
  const { hasAnyRole } = useAuth();
  const { theme, setTheme } = useThemeStore();

  const visible = (item: GroupItem) => !item.roles || hasAnyRole(item.roles);

  const shown = groups
    .map((g) => ({ ...g, items: g.items.filter(visible) }))
    .filter((g) => g.items.length > 0);

  return (
    <aside className="plate flex h-full w-60 flex-col" aria-label="Console navigation">
      {/* Brand */}
      <div className="flex items-center gap-2.5 border-b px-4 py-4" style={{ borderColor: 'var(--rail-line)' }}>
        <span
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded"
          style={{ background: 'var(--service)' }}
        >
          <MonitorSmartphone className="h-[17px] w-[17px] text-white" aria-hidden="true" />
        </span>
        <span className="min-w-0">
          <span className="ow-display block text-[16px] leading-tight" style={{ color: 'var(--rail-text)' }}>
            OurWay
          </span>
        </span>
      </div>

      <nav className="flex flex-1 flex-col gap-4 overflow-y-auto px-2.5 py-3" aria-label="Main navigation">
        {shown.map((group) => (
          <div key={group.heading} role="group" aria-label={group.heading} className="flex flex-col gap-0.5">
            <div className="px-2 pb-1">
              <span className="cap-sm cap" style={{ color: 'var(--rail-text-muted)' }}>
                {group.heading}
              </span>
            </div>
            {group.items.map((item) => {
              const Icon = item.icon;
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/'}
                  className={({ isActive }) =>
                    [
                      'flex items-center gap-2.5 rounded px-2.5 py-2 text-[13px] no-underline transition-colors duration-150',
                      isActive ? 'font-medium' : 'font-normal',
                    ].join(' ')
                  }
                  style={({ isActive }) => ({
                    color: isActive ? 'var(--rail-text)' : 'var(--rail-text-muted)',
                    background: isActive ? 'var(--rail-bg-raised)' : 'transparent',
                  })}
                >
                  {({ isActive }) => (
                    <>
                      <Icon
                        className="h-4 w-4 shrink-0"
                        style={{ color: isActive ? 'var(--rail-text)' : 'inherit' }}
                        aria-hidden="true"
                      />
                      <span className="min-w-0 flex-1 truncate" title={item.label}>
                        {item.label}
                      </span>
                      {item.go && (
                        <span
                          className="ow-num text-[10px]"
                          style={{ color: 'var(--rail-text-muted)', opacity: isActive ? 0.8 : 0.55 }}
                        >
                          {item.go}
                        </span>
                      )}
                    </>
                  )}
                </NavLink>
              );
            })}
          </div>
        ))}
      </nav>

      {/* Colour scheme */}
      <div className="border-t px-3 py-3" style={{ borderColor: 'var(--rail-line)' }}>
        <span className="cap-sm cap mb-1.5 block" style={{ color: 'var(--rail-text-muted)' }}>
          Appearance
        </span>
        <div
          className="segmented segmented--rail w-full"
          role="group"
          aria-label="Colour scheme"
        >
          {THEMES.map((t) => (
            <button
              key={t.value}
              type="button"
              onClick={() => setTheme(t.value)}
              aria-pressed={theme === t.value}
              className="segmented-item cap-sm flex-1 px-1 py-1"
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>
    </aside>
  );
}
