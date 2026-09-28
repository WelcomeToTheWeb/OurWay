import axios from 'axios';

const client = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
});

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('ourway_access_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

client.interceptors.response.use(
  (response) => response,
  async (error) => {
    const original = error.config;
    if (error.response?.status === 401 && !original?._retried) {
      // Try to renew the session with the refresh token before forcing a
      // logout — otherwise a single expired access token kills the session
      // even when a valid refresh token exists (e.g. SSO logins).
      const refreshToken = localStorage.getItem('ourway_refresh_token');
      if (refreshToken) {
        try {
          const { data } = await axios.post('/api/auth/refresh', {
            refresh_token: refreshToken,
          });
          localStorage.setItem('ourway_access_token', data.access_token);
          original._retried = true;
          original.headers.Authorization = `Bearer ${data.access_token}`;
          return client(original);
        } catch {
          // Refresh failed — fall through to logout.
        }
      }
    }
    if (error.response?.status === 401) {
      localStorage.removeItem('ourway_access_token');
      localStorage.removeItem('ourway_refresh_token');
      localStorage.removeItem('ourway_user');
      if (window.location.pathname !== '/login') {
        window.location.replace('/login');
      }
    }
    return Promise.reject(error);
  }
);

export default client;
