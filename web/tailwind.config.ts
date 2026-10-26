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
        /* the world's ink roles */
        ink: {
          DEFAULT: 'var(--ink)',
          plate: 'var(--ink-plate)',
          onPlate: 'var(--ink-on-plate)',
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
        stamp: 'var(--stamp)',
        service: 'var(--service)',
        due: 'var(--due)',
        rule: {
          DEFAULT: 'var(--rule)',
          strong: 'var(--rule-strong)',
        },
      },
      fontFamily: {
        sans: ['Barlow', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
        cap: ['"Archivo Narrow"', 'Barlow', 'sans-serif'],
      },
      /* Letterpress corners: rules meet at right angles, never balloons. */
      borderRadius: {
        none: '0',
        sm: '2px',
        DEFAULT: '2px',
        md: '2px',
        lg: '3px',
        xl: '3px',
        '2xl': '4px',
        '3xl': '6px',
        full: '9999px',
      },
      letterSpacing: {
        cap: '0.09em',
        stamp: '0.14em',
      },
      transitionTimingFunction: {
        overshoot: 'cubic-bezier(0.34, 1.56, 0.64, 1)',
      },
    },
  },
  plugins: [],
};

export default config;
