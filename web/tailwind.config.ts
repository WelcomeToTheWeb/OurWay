import type { Config } from 'tailwindcss';

const config: Config = {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          DEFAULT: 'var(--bg-primary)',
          secondary: 'var(--bg-secondary)',
          card: 'var(--bg-card)',
          border: 'var(--bg-border)',
        },
        text: {
          primary: 'var(--text-primary)',
          secondary: 'var(--text-secondary)',
          muted: 'var(--text-muted)',
        },
        /* action ink and the nav rail */
        ink: {
          DEFAULT: 'var(--ink)',
          plate: 'var(--rail-bg)',
          onPlate: 'var(--rail-text)',
        },
        rail: {
          DEFAULT: 'var(--rail-bg)',
          raised: 'var(--rail-bg-raised)',
          text: 'var(--rail-text)',
          muted: 'var(--rail-text-muted)',
          line: 'var(--rail-line)',
          active: 'var(--rail-active-bg)',
          activeText: 'var(--rail-active-text)',
        },
        accent: {
          DEFAULT: 'var(--service)',
          light: 'var(--service)',
          dark: 'var(--service)',
        },
        status: {
          online: 'var(--online)',
          offline: 'var(--offline)',
          warning: 'var(--warning)',
          error: 'var(--error)',
        },
        stamp: 'var(--error)',
        service: 'var(--service)',
        due: 'var(--due)',
        rule: {
          DEFAULT: 'var(--rule)',
          strong: 'var(--rule-strong)',
        },
      },
      fontFamily: {
        sans: ['"Public Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"Overpass Mono"', 'ui-monospace', 'monospace'],
        cap: ['"Public Sans"', 'ui-sans-serif', 'sans-serif'],
      },
      /* Console geometry: comfortable radii, never near-square, never a pill. */
      borderRadius: {
        none: '0',
        sm: '4px',
        DEFAULT: '6px',
        md: '6px',
        lg: '8px',
        xl: '10px',
        '2xl': '12px',
        '3xl': '16px',
        full: '9999px',
      },
      boxShadow: {
        elev1: 'var(--elev-1)',
        elev2: 'var(--elev-2)',
        elev3: 'var(--elev-3)',
      },
      letterSpacing: {
        cap: '0.04em',
        stamp: '0em',
      },
      transitionTimingFunction: {
        press: 'cubic-bezier(0.2, 0, 0, 1)',
        overshoot: 'cubic-bezier(0.2, 0, 0, 1)',
      },
    },
  },
  plugins: [],
};

export default config;
