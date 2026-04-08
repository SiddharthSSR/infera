import type { CSSProperties, ReactNode } from 'react';
import { cn } from '../../lib/utils';

/* ── GridRow ── */

export interface GridRowProps {
  children: ReactNode;
  /** Override the default 4-column template */
  columns?: number | string;
  /** Remove the bottom border */
  noBorder?: boolean;
  className?: string;
  style?: CSSProperties;
}

export function GridRow({
  children,
  columns,
  noBorder,
  className,
  style,
}: GridRowProps) {
  const overrides: CSSProperties = {};
  if (typeof columns === 'number') {
    overrides.gridTemplateColumns = `repeat(${columns}, 1fr)`;
  } else if (columns) {
    overrides.gridTemplateColumns = columns;
  }
  if (noBorder) overrides.borderBottom = 'none';

  return (
    <div className={cn('grid-row', className)} style={{ ...overrides, ...style }}>
      {children}
    </div>
  );
}

/* ── Cell ── */

export interface CellProps {
  children?: ReactNode;
  /** Column span (1–4) */
  span?: 1 | 2 | 3 | 4;
  /** Remove the right border */
  noBorder?: boolean;
  /** Background color override */
  bg?: string;
  className?: string;
  style?: CSSProperties;
}

export function Cell({
  children,
  span,
  noBorder,
  bg,
  className,
  style,
}: CellProps) {
  const overrides: CSSProperties = {};
  if (span && span > 1) overrides.gridColumn = `span ${span}`;
  if (noBorder) overrides.borderRight = 'none';
  if (bg) overrides.backgroundColor = bg;

  return (
    <div className={cn('cell', className)} style={{ ...overrides, ...style }}>
      {children}
    </div>
  );
}
