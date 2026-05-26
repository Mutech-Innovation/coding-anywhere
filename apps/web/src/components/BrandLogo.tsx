import { Box } from '@mantine/core';

import logoUrl from '../assets/logo.png';

/**
 * The Coding Anywhere mark. The asset is a square PNG (logo centered on a
 * transparent canvas), so passing just `height` gives a 1:1 square with
 * the mark vertically and horizontally centered. For square contexts that
 * need an opaque background (favicon, app icons, push badge) use the
 * pre-rendered assets in `apps/web/public/` instead.
 */
export function BrandLogo({
  height = 28,
  alt = 'Coding Anywhere',
  style,
}: {
  height?: number;
  alt?: string;
  style?: React.CSSProperties;
}) {
  return (
    <Box
      component="img"
      src={logoUrl}
      alt={alt}
      style={{
        height,
        width: 'auto',
        display: 'block',
        userSelect: 'none',
        pointerEvents: 'none',
        ...style,
      }}
    />
  );
}
