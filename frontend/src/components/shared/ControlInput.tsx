import { forwardRef, type CSSProperties, type InputHTMLAttributes, type SelectHTMLAttributes, type ReactNode } from 'react';
import { cn } from '../../lib/utils';

/* ── Text / Number Input ── */

export interface ControlInputProps extends InputHTMLAttributes<HTMLInputElement> {
  className?: string;
  style?: CSSProperties;
}

export const ControlInput = forwardRef<HTMLInputElement, ControlInputProps>(
  ({ className, ...rest }, ref) => (
    <input
      ref={ref}
      className={cn('control-input', className)}
      {...rest}
    />
  ),
);
ControlInput.displayName = 'ControlInput';

/* ── Select ── */

export interface ControlSelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
}

export const ControlSelect = forwardRef<HTMLSelectElement, ControlSelectProps>(
  ({ className, children, ...rest }, ref) => (
    <select
      ref={ref}
      className={cn('control-input', className)}
      style={{ cursor: 'pointer', ...rest.style }}
      {...rest}
    >
      {children}
    </select>
  ),
);
ControlSelect.displayName = 'ControlSelect';
