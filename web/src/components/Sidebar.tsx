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
} from 'lucide-react';
import { useAuth } from '../auth/context';
import { useThemeStore, type Theme } from '../stores/themeStore';

/**
 * The nav rail is the console's plate — the one committed field of colour.
 * Navigation itself is the category standard: labelled groups, text links,
 * an obvious active item.
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
      { to: '/files', label: 'Files', icon: FolderUp, go: 'g f', roles: ['admin', 'manager', 'technician'] },
      { to: '/patches', label: 'Patches', icon: Package, go: 'g p', roles: ['admin', 'manager', 'technician'] },
      { to: '/patch-policies', label: 'Policies', icon: Shield, go: 'g e', roles: ['admin', 'manager'] },
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
      {/* Brand plate */}
      <div className="border-b px-5 py-4" style={{ borderColor: 'rgba(232,228,216,0.18)' }}>
        <div className="flex items-center gap-2.5">
          <span
            className="flex h-8 w-8 items-center justify-center border"
            style={{ borderColor: 'rgba(232,228,216,0.4)' }}
          >
            <MonitorSmartphone className="h-4 w-4" style={{ color: 'var(--ink-on-plate)' }} aria-hidden="true" />
          </span>
          <span>
            <span className="ow-display block text-[19px] leading-none" style={{ color: 'var(--ink-on-plate)' }}>
              OurWay
            </span>
            <span className="cap-sm cap mt-1 block" style={{ color: 'var(--ink-on-plate-muted)' }}>
              RMM · self-hosted
            </span>
          </span>
        </div>
      </div>

      <nav className="flex flex-1 flex-col gap-5 overflow-y-auto px-3 py-4" aria-label="Main navigation">
        {shown.map((group) => (
          <div key={group.heading} role="group" aria-label={group.heading} className="flex flex-col">
            <div className="flex items-center gap-2 px-1 pb-1.5">
              <span className="cap-sm cap" style={{ color: 'var(--ink-on-plate-muted)' }}>
                {group.heading}
              </span>
              <span className="h-px flex-1" style={{ background: 'rgba(232,228,216,0.16)' }} />
            </div>
            <div className="flex flex-col">
              {group.items.map((item) => {
                const Icon = item.icon;
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    className={({ isActive }) =>
                      [
                        'group flex items-center gap-2.5 border-b py-2 pl-3 pr-2 text-[13px] no-underline transition-[background-color,color] duration-400 ease-[var(--ease-press)]',
                        isActive
                          ? 'font-bold'
                          : 'font-medium',
                      ].join(' ')
                    }
                    style={({ isActive }) => ({
                      borderColor: 'rgba(232,228,216,0.10)',
                      color: isActive ? 'var(--ink)' : 'var(--ink-on-plate)',
                      background: isActive ? 'var(--ink-on-plate)' : 'transparent',
                    })}
                  >
                    {({ isActive }) => (
                      <>
                        <Icon className="h-[15px] w-[15px] shrink-0" aria-hidden="true" />
                        <span className="flex-1">{item.label}</span>
                        {item.go && (
                          <span
                            className="ow-num text-[9.5px] opacity-60"
                            style={{ color: isActive ? 'var(--ink)' : 'var(--ink-on-plate-muted)' }}
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
          </div>
        ))}
      </nav>

      {/* Light control: the shop's own lamp */}
      <div
        className="flex items-center gap-1 border-t px-3 py-3"
        style={{ borderColor: 'rgba(232,228,216,0.18)' }}
      >
        <span className="cap-sm cap mr-1" style={{ color: 'var(--ink-on-plate-muted)' }}>
          Light
        </span>
        <div className="flex flex-1" role="group" aria-label="Colour scheme">
          {THEMES.map((t) => (
            <button
              key={t.value}
              type="button"
              onClick={() => setTheme(t.value)}
              aria-pressed={theme === t.value}
              className="cap cap-sm flex-1 border px-1 py-1 transition-colors duration-400"
              style={{
                borderColor: 'rgba(232,228,216,0.22)',
                color: theme === t.value ? 'var(--ink)' : 'var(--ink-on-plate)',
                background: theme === t.value ? 'var(--ink-on-plate)' : 'transparent',
                marginLeft: t.value === 'light' ? 0 : -1,
              }}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>
    </aside>
  );
}
