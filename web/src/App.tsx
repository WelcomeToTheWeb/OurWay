import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './auth/context';
import { RoleRoute } from './components/RoleRoute';
import { Layout } from './components/Layout';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';

const Devices = lazy(() => import('./pages/Devices').then((m) => ({ default: m.Devices })));
const DeviceDetail = lazy(() => import('./pages/DeviceDetail').then((m) => ({ default: m.DeviceDetail })));
const Alerts = lazy(() => import('./pages/Alerts').then((m) => ({ default: m.Alerts })));
const Settings = lazy(() => import('./pages/Settings').then((m) => ({ default: m.Settings })));
const Users = lazy(() => import('./pages/Users').then((m) => ({ default: m.Users })));
const Patches = lazy(() => import('./pages/PatchHub').then((m) => ({ default: m.PatchHub })));
const PatchPolicies = lazy(() => import('./pages/PatchPolicies').then((m) => ({ default: m.PatchPolicies })));
const FileTransfer = lazy(() => import('./pages/FileTransfer').then((m) => ({ default: m.FileTransfer })));
const Sessions = lazy(() => import('./pages/Sessions').then((m) => ({ default: m.Sessions })));
const SSO = lazy(() => import('./pages/SSO').then((m) => ({ default: m.SSO })));
const Webhooks = lazy(() => import('./pages/Webhooks').then((m) => ({ default: m.Webhooks })));
const Clients = lazy(() => import('./pages/Clients').then((m) => ({ default: m.Clients })));
const Audit = lazy(() => import('./pages/Audit').then((m) => ({ default: m.Audit })));
// 2.4-5: the threshold ledger (admin, manager)
const Thresholds = lazy(() => import('./pages/Thresholds').then((m) => ({ default: m.Thresholds })));

function RouteFallback() {
  return (
    <div className="flex h-64 flex-col items-center justify-center gap-3">
      <span
        className="h-8 w-8 animate-spin border-2"
        style={{ borderColor: 'var(--rule-strong)', borderTopColor: 'var(--service)' }}
        aria-hidden="true"
      />
      <span className="cap cap-sm text-text-muted">Loading this page…</span>
    </div>
  );
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth();
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <Layout />
            </ProtectedRoute>
          }
        >
          <Route index element={<Dashboard />} />
          <Route
            path="devices"
            element={
              <Suspense fallback={<RouteFallback />}>
                <Devices />
              </Suspense>
            }
          />
          <Route
            path="devices/:id"
            element={
              <Suspense fallback={<RouteFallback />}>
                <DeviceDetail />
              </Suspense>
            }
          />
          <Route
            path="alerts"
            element={
              <Suspense fallback={<RouteFallback />}>
                <Alerts />
              </Suspense>
            }
          />
          <Route
            path="sessions"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <Suspense fallback={<RouteFallback />}>
                  <Sessions />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="files"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <Suspense fallback={<RouteFallback />}>
                  <FileTransfer />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="patches"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <Suspense fallback={<RouteFallback />}>
                  <Patches />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="patch-policies"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Suspense fallback={<RouteFallback />}>
                  <PatchPolicies />
                </Suspense>
              </RoleRoute>
            }
          />
          {/* 2.3-4: every role reads the clients its scope names; the server
              answers the ledger, and admin-only writes are refused there. */}
          <Route
            path="clients"
            element={
              <Suspense fallback={<RouteFallback />}>
                <Clients />
              </Suspense>
            }
          />
          {/* 2.3-6: the audit ledger. Admin reads the installation, managers
              read their tenants, and the route itself refuses a viewer — the
              page asks for nothing a caller cannot perform. */}
          <Route
            path="audit"
            element={
              <Suspense fallback={<RouteFallback />}>
                <Audit />
              </Suspense>
            }
          />
          <Route
            path="users"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Suspense fallback={<RouteFallback />}>
                  <Users />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="settings"
            element={
              <RoleRoute roles={['admin']}>
                <Suspense fallback={<RouteFallback />}>
                  <Settings />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="sso"
            element={
              <RoleRoute roles={['admin']}>
                <Suspense fallback={<RouteFallback />}>
                  <SSO />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="thresholds"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Suspense fallback={<RouteFallback />}>
                  <Thresholds />
                </Suspense>
              </RoleRoute>
            }
          />
          <Route
            path="webhooks"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Suspense fallback={<RouteFallback />}>
                  <Webhooks />
                </Suspense>
              </RoleRoute>
            }
          />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
