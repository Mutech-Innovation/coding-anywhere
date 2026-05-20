import { type MantineColorsTuple, createTheme } from '@mantine/core';
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

export const theme = createTheme({
  primaryColor: 'brand',
  colors: {
    brand,
  },
  fontFamily: fonts.sans,
  fontFamilyMonospace: fonts.mono,
  defaultRadius: radii.md,
  fontSizes: {
    xs: `${fontSizes.xs}px`,
    sm: `${fontSizes.sm}px`,
    md: `${fontSizes.md}px`,
    lg: `${fontSizes.lg}px`,
    xl: `${fontSizes.xl}px`,
  },
});
