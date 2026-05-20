// Design tokens shared across web (Mantine theme) and mobile (NativeWind config).
//
// Keep values primitive (numbers, strings) so both consumers can map them to
// their own theming system without runtime overhead.

export const colors = {
  brand: {
    50: '#eef6ff',
    100: '#d9eaff',
    200: '#b7d6ff',
    300: '#8bbbff',
    400: '#5896ff',
    500: '#306eff',
    600: '#1f4eea',
    700: '#1a3dbf',
    800: '#1a3597',
    900: '#1c3279',
  },
  // semantic tokens — overlaid by each platform's theme
  surface: '#0b0f17',
  surfaceMuted: '#11161f',
  surfaceElevated: '#171d27',
  text: '#e7ecf3',
  textMuted: '#8a93a3',
  border: '#222a36',
  success: '#22c55e',
  warning: '#f59e0b',
  danger: '#ef4444',
} as const;

export const radii = {
  xs: 4,
  sm: 6,
  md: 10,
  lg: 14,
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
  sans: 'Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
  mono: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Consolas, monospace',
} as const;

export const fontSizes = {
  xs: 12,
  sm: 13,
  md: 14,
  lg: 16,
  xl: 18,
  xxl: 22,
} as const;
