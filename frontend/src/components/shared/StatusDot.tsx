import type { CSSProperties } from 'react';
import { cn } from '../../lib/utils';

export type StatusDotTone = 'success' | 'warning' | 'error' | 'inactive' | 'neutral' | 'info';

const toneClass: Record<StatusDotTone, string> = {
  success: '',
  warning: 'warning',
  error: 'error',
  inactive: 'inactive',
  neutral: 'neutral',
  info: 'info',
};

export interface StatusDotProps {
  tone?: StatusDotTone;
  /** Override size in px — default 8 */
  size?: number;
  /** Pulse animation for active/running states */
  pulse?: boolean;
  className?: string;
  style?: CSSProperties;
}

export function StatusDot({
  tone = 'success',
  size,
  pulse,
  className,
  style,
}: StatusDotProps) {
  const overrides: CSSProperties = {};
  if (size) {
    overrides.width = size;
    overrides.height = size;
  }
  if (pulse) {
    overrides.animation = 'status-pulse 2s ease-in-out infinite';
  }

  return (
    <span
      className={cn('status-dot', toneClass[tone], className)}
      style={{ ...overrides, ...style }}
      aria-hidden="true"
    />
  );
}
