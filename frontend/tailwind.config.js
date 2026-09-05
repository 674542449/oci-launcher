/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        ground: 'var(--c-bg)',
        surface: {
          DEFAULT: 'var(--c-surface)',
          2: 'var(--c-surface-2)',
        },
        line: {
          DEFAULT: 'var(--c-line)',
          strong: 'var(--c-line-strong)',
        },
        ink: {
          DEFAULT: 'var(--c-ink)',
          2: 'var(--c-ink-2)',
          3: 'var(--c-ink-3)',
        },
        brand: {
          DEFAULT: 'var(--c-brand)',
          hover: 'var(--c-brand-hover)',
          pressed: 'var(--c-brand-pressed)',
          soft: 'var(--c-brand-soft)',
        },
        ok: { DEFAULT: 'var(--c-ok)', soft: 'var(--c-ok-soft)' },
        warn: { DEFAULT: 'var(--c-warn)', soft: 'var(--c-warn-soft)' },
        danger: { DEFAULT: 'var(--c-danger)', soft: 'var(--c-danger-soft)' },
        info: { DEFAULT: 'var(--c-info)', soft: 'var(--c-info-soft)' },
        side: {
          DEFAULT: 'var(--c-side)',
          2: 'var(--c-side-2)',
          3: 'var(--c-side-3)',
          ink: 'var(--c-side-ink)',
          muted: 'var(--c-side-muted)',
        },
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
        mono: ['var(--font-mono)'],
      },
      boxShadow: {
        card: '0 1px 2px rgba(28, 25, 23, 0.04), 0 1px 3px rgba(28, 25, 23, 0.06)',
        pop: '0 8px 24px rgba(28, 25, 23, 0.14)',
      },
    },
  },
  plugins: [],
}
