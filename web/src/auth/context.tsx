import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react';
import client from '../api/client';
import type { AuthContextType, User } from './types';

const AuthContext = createContext<AuthContextType | null>(null);

function loadUser(): User | null {
  const raw = localStorage.getItem('ourway_user');
  if (!raw) return null;
  try {
    return JSON.parse(raw) as User;
  } catch {
    return null;
  }
}

function loadRoles(): string[] {
  const raw = localStorage.getItem('ourway_roles');
  if (!raw) return [];
  try {
    return JSON.parse(raw) as string[];
  } catch {
    return [];
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(loadUser);
  const [roles, setRoles] = useState<string[]>(loadRoles);
  const [accessToken, setAccessToken] = useState<string | null>(
    localStorage.getItem('ourway_access_token')
  );
  const [refreshToken, setRefreshToken] = useState<string | null>(
    localStorage.getItem('ourway_refresh_token')
  );
  const [isAuthenticated, setIsAuthenticated] = useState(!!accessToken);

  const login = useCallback(async (username: string, password: string) => {
    const { data } = await client.post<{
      access_token: string;
      refresh_token: string;
      user: User;
      roles: string[];
    }>('/auth/login', { username, password });
    setAccessToken(data.access_token);
    setRefreshToken(data.refresh_token);
    setUser(data.user);
    setRoles(data.roles);
    setIsAuthenticated(true);
    localStorage.setItem('ourway_access_token', data.access_token);
    localStorage.setItem('ourway_refresh_token', data.refresh_token);
    localStorage.setItem('ourway_user', JSON.stringify(data.user));
    localStorage.setItem('ourway_roles', JSON.stringify(data.roles));
  }, []);

  const register = useCallback(async (username: string, email: string, password: string) => {
    await client.post('/auth/register', { username, email, password });
    // Auto-login after registration
    await login(username, password);
  }, [login]);

  const logout = useCallback(() => {
    setAccessToken(null);
    setRefreshToken(null);
    setUser(null);
    setRoles([]);
    setIsAuthenticated(false);
    localStorage.removeItem('ourway_access_token');
    localStorage.removeItem('ourway_refresh_token');
    localStorage.removeItem('ourway_user');
    localStorage.removeItem('ourway_roles');
  }, []);

  const refresh = useCallback(async () => {
    const token = localStorage.getItem('ourway_refresh_token');
    if (!token) throw new Error('No refresh token');
    const { data } = await client.post<{ access_token: string }>('/auth/refresh', {
      refresh_token: token,
    });
    setAccessToken(data.access_token);
    localStorage.setItem('ourway_access_token', data.access_token);
  }, []);

  const setToken = useCallback((token: string, refreshToken?: string) => {
    setAccessToken(token);
    setIsAuthenticated(true);
    localStorage.setItem('ourway_access_token', token);
    // Store the refresh token (SSO flow) so the session can be renewed
    // instead of dying when the access token expires.
    if (refreshToken) {
      setRefreshToken(refreshToken);
      localStorage.setItem('ourway_refresh_token', refreshToken);
    }
    // Fetch user profile with new token
    client.get('/auth/profile').then((res) => {
      setUser(res.data.user);
      setRoles(res.data.roles);
      localStorage.setItem('ourway_user', JSON.stringify(res.data.user));
      localStorage.setItem('ourway_roles', JSON.stringify(res.data.roles));
    });
  }, []);

  const hasRole = useCallback((role: string): boolean => {
    return roles.includes(role);
  }, [roles]);

  const hasAnyRole = useCallback((requiredRoles: string[]): boolean => {
    return requiredRoles.some((r) => roles.includes(r));
  }, [roles]);

  useEffect(() => {
    if (!accessToken) return;
    const id = setInterval(() => {
      refresh().catch(() => logout());
    }, 5 * 60 * 1000);
    return () => clearInterval(id);
  }, [accessToken, refresh, logout]);

  return (
    <AuthContext.Provider
      value={{
        user,
        roles,
        accessToken,
        refreshToken,
        isAuthenticated,
        login,
        register,
        logout,
        refresh,
        setToken,
        hasRole,
        hasAnyRole,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextType {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
