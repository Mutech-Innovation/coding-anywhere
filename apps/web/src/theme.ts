import { type MantineColorsTuple, createTheme, rem } from '@mantine/core';
import { colors, fonts, fontSizes, radii } from '@coding-anywhere/tokens';

const brand: MantineColorsTuple = [
  colors.brand[50],
  colors.brand[100],
  colors.brand[200],
  colors.brand[300],
  colors.brand[400],
  colors.brand[500],
  colors.brand[600],
  colors.brand[700],
  colors.brand[800],
  colors.brand[900],
];

const ink: MantineColorsTuple = [
  colors.ink[50],
  colors.ink[100],
  colors.ink[200],
  colors.ink[300],
  colors.ink[400],
  colors.ink[500],
  colors.ink[600],
  colors.ink[700],
  colors.ink[800],
  colors.ink[900],
];

export const theme = createTheme({
  primaryColor: 'brand',
  primaryShade: { light: 8, dark: 3 },
  colors: {
    brand,
    ink,
  },
  white: '#ffffff',
  black: colors.ink[900],
  fontFamily: fonts.sans,
  fontFamilyMonospace: fonts.mono,
  headings: {
    fontFamily: fonts.sans,
    fontWeight: '700',
    sizes: {
      h1: { fontSize: rem(56), lineHeight: '1.05', fontWeight: '800' },
      h2: { fontSize: rem(36), lineHeight: '1.15', fontWeight: '700' },
      h3: { fontSize: rem(24), lineHeight: '1.25', fontWeight: '700' },
      h4: { fontSize: rem(18), lineHeight: '1.35', fontWeight: '600' },
    },
  },
  defaultRadius: radii.md,
  fontSizes: {
    xs: `${fontSizes.xs}px`,
    sm: `${fontSizes.sm}px`,
    md: `${fontSizes.md}px`,
    lg: `${fontSizes.lg}px`,
    xl: `${fontSizes.xl}px`,
  },
  components: {
    Button: {
      defaultProps: {
        radius: radii.sm,
      },
      styles: {
        root: { fontWeight: 600, letterSpacing: 0.1 },
      },
    },
    Badge: {
      defaultProps: {
        radius: radii.xs,
        fw: 600,
      },
    },
    Paper: {
      defaultProps: {
        radius: radii.md,
      },
    },
    Tabs: {
      defaultProps: {
        variant: 'default',
      },
    },
  },
});
