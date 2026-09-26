import { useState } from 'react';
import { Outlet } from 'react-router-dom';
import { useAuth } from '../auth/context';
import { Sidebar } from './Sidebar';
import { LogOut, UserCircle, Menu } from 'lucide-react';
import { useGlobalShortcuts } from '../hooks/useKeyboardShortcuts';

export function Layout() {
  const { user, logout } = useAuth();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  useGlobalShortcuts();

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Skip to content link for accessibility */}
      <a href="#main-content" className="skip-link">
        Skip to main content
      </a>

      {/* Mobile sidebar overlay */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/50 md:hidden"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}

      {/* Sidebar */}
      <div
        className={`fixed inset-y-0 left-0 z-40 w-64 transform transition-transform duration-200 ease-in-out md:static md:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'
        }`}
      >
        <Sidebar />
      </div>

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-16 items-center justify-between border-b border-bg-border bg-bg px-4 md:px-6">
          <button
            className="mr-4 rounded-lg p-2 text-text-secondary transition-colors hover:bg-bg-secondary hover:text-text-primary md:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="Open navigation menu"
          >
            <Menu className="h-5 w-5" />
          </button>
          <div />
          <div className="flex items-center gap-4">
            {user && (
              <div className="flex items-center gap-2">
                <UserCircle className="h-5 w-5 text-text-secondary" />
                <span className="hidden text-sm text-text-secondary sm:inline">{user.username}</span>
              </div>
            )}
            <button
              onClick={logout}
              className="flex items-center gap-2 rounded-lg bg-bg-secondary px-3 py-1.5 text-xs font-medium text-text-secondary transition-colors hover:bg-bg hover:text-text-primary"
              aria-label="Sign out"
            >
              <LogOut className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">Sign out</span>
            </button>
          </div>
        </header>

        <main
          id="main-content"
          className="flex-1 overflow-y-auto p-4 md:p-6"
          tabIndex={-1}
        >
          <Outlet />
        </main>
      </div>
    </div>
  );
}
