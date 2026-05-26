// Design tokens shared across web (Mantine theme) and mobile (NativeWind config).
//
// Keep values primitive (numbers, strings) so both consumers can map them to
// their own theming system without runtime overhead.
//
// Palette is tuned to match the CodeWhale (codewhale.net/zh) visual language:
// near-black ink on white surfaces, deep indigo accent, ultra-light tinted
// highlight, and 1px hairline borders. Dark mode keeps the same hues but
// inverts surfaces.

export const colors = {
  // Primary accent — deep indigo (CodeWhale-style link / CTA).
  // 6/7 are the "ink-blue" used for headings and links.
  brand: {
    50: '#eef1ff',
    100: '#e0e6ff',
    200: '#c0ccff',
    300: '#94a6ff',
    400: '#6379ff',
    500: '#3d54ee',
    600: '#2a3fd4',
    700: '#1f31b3',
    800: '#1a3dbf', // CodeWhale link/heading blue
    900: '#152a8f',
  },

  // Ink scale — near-black through cool greys. Use these on light surfaces.
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

  // Light theme semantic tokens (CodeWhale default look).
  light: {
    surface: '#ffffff',
    surfaceMuted: '#fafafa',
    surfaceElevated: '#f5f6f8',
    text: '#0e0e10',
    textMuted: '#6b7280',
    border: '#e5e7eb',
    borderStrong: '#0e0e10',
    accent: '#1a3dbf',
    accentSoft: '#e9eefe', // pale blue tag/badge background
    inverse: '#0e0e10', // dark CTA region background
    inverseText: '#ffffff',
  },

  // Dark theme semantic tokens.
  dark: {
    surface: '#0b0f17',
    surfaceMuted: '#11161f',
    surfaceElevated: '#171d27',
    text: '#e7ecf3',
    textMuted: '#8a93a3',
    border: '#222a36',
    borderStrong: '#e7ecf3',
    accent: '#94a6ff',
    accentSoft: '#1a2148',
    inverse: '#ffffff',
    inverseText: '#0e0e10',
  },

  // Legacy aliases kept for backwards-compatibility with existing consumers.
  surface: '#ffffff',
  surfaceMuted: '#fafafa',
  surfaceElevated: '#f5f6f8',
  text: '#0e0e10',
  textMuted: '#6b7280',
  border: '#e5e7eb',
  success: '#16a34a',
  warning: '#d97706',
  danger: '#dc2626',
} as const;

export const radii = {
  xs: 4,
  sm: 6,
  md: 8,
  lg: 12,
  xl: 20,
} as const;

export const space = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
} as const;

export const fonts = {
  // Match CodeWhale: tight Inter for Latin + PingFang / Source Han for CJK.
  sans: '"Inter", "PingFang SC", "Source Han Sans SC", "Noto Sans SC", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
  mono: '"JetBrains Mono", "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
} as const;

export const fontSizes = {
  xs: 12,
  sm: 13,
  md: 14,
  lg: 16,
  xl: 18,
  xxl: 22,
} as const;
