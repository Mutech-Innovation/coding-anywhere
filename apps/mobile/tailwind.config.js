/** @type {import('tailwindcss').Config} */
//
// The palette/spacing/radii below mirror `packages/tokens` (the cross-platform
// design-token source of truth). They are inlined here because a CommonJS Tailwind
// config cannot `require()` the ESM/TypeScript-source tokens package. Keep these in
// sync with packages/tokens/src/index.ts; at runtime, import the tokens package
// directly instead of duplicating values.
//
// Semantic colors are exposed twice — a base name (light) and a `*-dark` alias —
// so components theme via the NativeWind `dark:` variant, e.g.
//   className="bg-surface dark:bg-surface-dark text-text dark:text-text-dark"
module.exports = {
  content: ['./src/**/*.{js,jsx,ts,tsx}'],
  presets: [require('nativewind/preset')],
  // 'class' = manual control via nativewind's colorScheme API (see src/lib/theme.ts).
  // The default pref is 'system', so it follows the device unless the user overrides.
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#eef1ff',
          100: '#e0e6ff',
          200: '#c0ccff',
          300: '#94a6ff',
          400: '#6379ff',
          500: '#3d54ee',
          600: '#2a3fd4',
          700: '#1f31b3',
          800: '#1a3dbf',
          900: '#152a8f',
        },
        ink: {
          50: '#f7f8fa',
          100: '#eef0f4',
          200: '#e4e7ec',
          300: '#d1d6de',
          400: '#9aa1ac',
          500: '#6b7280',
          600: '#4b5563',
          700: '#272a31',
          800: '#16181d',
          900: '#0e0e10',
        },
        // Light semantic tokens.
        surface: '#ffffff',
        'surface-muted': '#fafafa',
        'surface-elevated': '#f5f6f8',
        text: '#0e0e10',
        'text-muted': '#6b7280',
        border: '#e5e7eb',
        'border-strong': '#0e0e10',
        accent: '#1a3dbf',
        'accent-soft': '#e9eefe',
        // Dark semantic tokens (used via the `dark:` variant).
        'surface-dark': '#0b0f17',
        'surface-muted-dark': '#11161f',
        'surface-elevated-dark': '#171d27',
        'text-dark': '#e7ecf3',
        'text-muted-dark': '#8a93a3',
        'border-dark': '#222a36',
        'border-strong-dark': '#e7ecf3',
        'accent-dark': '#94a6ff',
        'accent-soft-dark': '#1a2148',
        success: '#16a34a',
        warning: '#d97706',
        danger: '#dc2626',
      },
      borderRadius: {
        xs: 4,
        sm: 6,
        md: 8,
        lg: 12,
        xl: 20,
      },
      spacing: {
        xs: 4,
        sm: 8,
        md: 12,
        lg: 16,
        xl: 24,
        xxl: 32,
      },
      fontSize: {
        xs: 12,
        sm: 13,
        md: 14,
        lg: 16,
        xl: 18,
        xxl: 22,
      },
    },
  },
  plugins: [],
};
