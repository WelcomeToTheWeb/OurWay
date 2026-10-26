import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';
import '../i18n';

// jsdom ships no matchMedia; the theme store reads prefers-color-scheme.
window.matchMedia =
  window.matchMedia ||
  ((query: string) =>
    ({
      matches: false,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList);

afterEach(() => {
  cleanup();
  localStorage.clear();
});
