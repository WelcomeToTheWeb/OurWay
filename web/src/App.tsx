import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './auth/context';
import { RoleRoute } from './components/RoleRoute';
import { Layout } from './components/Layout';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';
import { Devices } from './pages/Devices';
import { DeviceDetail } from './pages/DeviceDetail';
import { Alerts } from './pages/Alerts';
import { Settings } from './pages/Settings';
import { Users } from './pages/Users';
import { Patches } from './pages/Patches';
import { PatchPolicies } from './pages/PatchPolicies';
import { FileTransfer } from './pages/FileTransfer';
import { Sessions } from './pages/Sessions';
import { SSO } from './pages/SSO';
import { Webhooks } from './pages/Webhooks';

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
          <Route path="devices" element={<Devices />} />
          <Route path="devices/:id" element={<DeviceDetail />} />
          <Route path="alerts" element={<Alerts />} />
          <Route
            path="sessions"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <Sessions />
              </RoleRoute>
            }
          />
          <Route
            path="files"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <FileTransfer />
              </RoleRoute>
            }
          />
          <Route
            path="patches"
            element={
              <RoleRoute roles={['admin', 'manager', 'technician']}>
                <Patches />
              </RoleRoute>
            }
          />
          <Route
            path="patch-policies"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <PatchPolicies />
              </RoleRoute>
            }
          />
          <Route
            path="users"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Users />
              </RoleRoute>
            }
          />
          <Route
            path="settings"
            element={
              <RoleRoute roles={['admin']}>
                <Settings />
              </RoleRoute>
            }
          />
          <Route
            path="sso"
            element={
              <RoleRoute roles={['admin']}>
                <SSO />
              </RoleRoute>
            }
          />
          <Route
            path="webhooks"
            element={
              <RoleRoute roles={['admin', 'manager']}>
                <Webhooks />
              </RoleRoute>
            }
          />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
