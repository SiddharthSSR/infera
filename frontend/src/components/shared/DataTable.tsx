import type { CSSProperties, ReactNode, TdHTMLAttributes, ThHTMLAttributes } from 'react';
import { cn } from '../../lib/utils';

/* ── DataTable wrapper ── */

export interface DataTableProps {
  children: ReactNode;
  /** Minimum width before horizontal scroll kicks in — default 760px */
  minWidth?: number;
  className?: string;
  style?: CSSProperties;
}

export function DataTable({
  children,
  minWidth = 760,
  className,
  style,
}: DataTableProps) {
  return (
    <div className="responsive-scroll-x">
      <table
        className={cn('data-table responsive-scroll-x-content', className)}
        style={{ minWidth, ...style }}
      >
        {children}
      </table>
    </div>
  );
}

/* ── Th — header cell with label-text styling ── */

export interface ThProps extends ThHTMLAttributes<HTMLTableCellElement> {
  children?: ReactNode;
  /** Right-align content */
  alignRight?: boolean;
}

export function Th({ children, alignRight, style, ...rest }: ThProps) {
  const overrides: CSSProperties = {};
  if (alignRight) overrides.textAlign = 'right';
  return (
    <th style={{ ...overrides, ...style }} {...rest}>
      {children}
    </th>
  );
}

/* ── Td — body cell ── */

export interface TdProps extends TdHTMLAttributes<HTMLTableCellElement> {
  children?: ReactNode;
  /** Use monospace font */
  mono?: boolean;
  /** Right-align content */
  alignRight?: boolean;
}

export function Td({ children, mono, alignRight, className, style, ...rest }: TdProps) {
  const overrides: CSSProperties = {};
  if (alignRight) overrides.textAlign = 'right';
  return (
    <td
      className={cn(mono && 'mono', className)}
      style={{ ...overrides, ...style }}
      {...rest}
    >
      {children}
    </td>
  );
}
