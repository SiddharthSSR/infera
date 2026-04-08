import type { CSSProperties, ReactNode } from 'react';
import { cn } from '../../lib/utils';

export interface DisplayHeaderProps {
  children: ReactNode;
  /** Override the default clamp font size */
  fontSize?: string;
  /** Text alignment — defaults to 'center' per design system */
  align?: 'left' | 'center' | 'right';
  /** Hide the bottom border */
  noBorder?: boolean;
  /** Override padding */
  padding?: string;
  className?: string;
  style?: CSSProperties;
}

export function DisplayHeader({
  children,
  fontSize,
  align,
  noBorder,
  padding,
  className,
  style,
}: DisplayHeaderProps) {
  const overrides: CSSProperties = {};
  if (fontSize) overrides.fontSize = fontSize;
  if (align) overrides.textAlign = align;
  if (noBorder) overrides.borderBottom = 'none';
  if (padding) overrides.padding = padding;

  return (
    <header
      className={cn('display-text', className)}
      style={{ ...overrides, ...style }}
    >
      {children}
    </header>
  );
}
