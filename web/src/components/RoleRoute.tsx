import { Navigate } from 'react-router-dom';
import { useAuth } from '../auth/context';

export function RoleRoute({
  roles,
  children,
}: {
  roles: string[];
  children: React.ReactNode;
}) {
  const { hasAnyRole } = useAuth();
  if (!hasAnyRole(roles)) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
}
