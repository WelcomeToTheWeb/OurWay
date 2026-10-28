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

// jsdom ships no ResizeObserver. react-window measures its viewport with one,
// and the card grid (2.3-5's 10k-unit budget) is the component that needs it:
// without a shim the grid throws on mount and every fleet page that renders it
// fails its own test. A no-op observer is the honest stub — the window's size
// comes from the box the page sets, not from a layout pass jsdom cannot do.
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof globalThis.ResizeObserver;
}

afterEach(() => {
  cleanup();
  localStorage.clear();
});
