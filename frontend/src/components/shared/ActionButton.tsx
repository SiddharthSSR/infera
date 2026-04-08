import type { ButtonHTMLAttributes, CSSProperties } from 'react';
import { cn } from '../../lib/utils';

export type ActionButtonVariant =
  | 'action'       // underline-style (default)
  | 'primary'      // filled dark
  | 'secondary'    // outlined
  | 'link'         // text underline link
  | 'destructive'  // action-btn with error color
  | 'link-muted'   // muted text link
  | 'link-danger'; // danger text link

const variantClass: Record<ActionButtonVariant, string> = {
  action: 'action-btn',
  primary: 'btn-primary',
  secondary: 'btn-secondary',
  link: 'action-link',
  destructive: 'action-btn destructive',
  'link-muted': 'action-link muted',
  'link-danger': 'action-link danger',
};

export interface ActionButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ActionButtonVariant;
  /** Minimum height for touch targets on mobile */
  minHeight?: number;
  className?: string;
  style?: CSSProperties;
}

export function ActionButton({
  variant = 'action',
  minHeight,
  className,
  style,
  children,
  ...rest
}: ActionButtonProps) {
  const overrides: CSSProperties = {};
  if (minHeight) overrides.minHeight = minHeight;

  return (
    <button
      type="button"
      className={cn(variantClass[variant], className)}
      style={{ ...overrides, ...style }}
      {...rest}
    >
      {children}
    </button>
  );
}
