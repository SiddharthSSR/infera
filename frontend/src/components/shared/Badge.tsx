import type { CSSProperties, ReactNode } from 'react';
import { cn } from '../../lib/utils';

export type BadgeTone = '' | 'warning' | 'error' | 'inactive';

export interface BadgeProps {
  children: ReactNode;
  /** Semantic tone — maps to status-warning, status-error, status-inactive CSS classes */
  tone?: BadgeTone;
  /** Use monospace font */
  mono?: boolean;
  className?: string;
  style?: CSSProperties;
}

export function Badge({
  children,
  tone,
  mono,
  className,
  style,
}: BadgeProps) {
  return (
    <span
      className={cn(
        'badge',
        tone && `status-${tone}`,
        mono && 'mono',
        className,
      )}
      style={style}
    >
      {children}
    </span>
  );
}
