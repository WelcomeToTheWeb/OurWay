export interface User {
  id: string;
  username: string;
  email: string;
  created_at: string;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: string[];
  is_builtin: boolean;
  created_at: string;
  updated_at?: string;
}

export interface UserWithRoles {
  id: string;
  username: string;
  email: string;
  created_at: string;
  roles: string[];
}

export interface AuthState {
  user: User | null;
  roles: string[];
  accessToken: string | null;
  refreshToken: string | null;
  isAuthenticated: boolean;
}

export interface AuthContextType extends AuthState {
  login: (username: string, password: string) => Promise<void>;
  register: (username: string, email: string, password: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;
  setToken: (token: string) => void;
  hasRole: (role: string) => boolean;
  hasAnyRole: (roles: string[]) => boolean;
}

export interface AuthStatus {
  has_users: boolean;
}
