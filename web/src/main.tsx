import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from './App';
import { AuthProvider } from './auth/context';
import './i18n';
import './index.css';

// Apply theme before rendering to avoid flash
import { useThemeStore } from './stores/themeStore';
useThemeStore.getState().resolveTheme();

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <AuthProvider>
      <App />
    </AuthProvider>
  </React.StrictMode>
);
