import type { CSSProperties, ReactNode } from 'react';
import { cn } from '../../lib/utils';

export interface LabelTextProps {
  children: ReactNode;
  /** Render as a different element — defaults to 'span' */
  as?: 'span' | 'div' | 'label';
  /** Optional leading SVG icon (14×14 expected) */
  icon?: ReactNode;
  className?: string;
  style?: CSSProperties;
  htmlFor?: string;
}

export function LabelText({
  children,
  as: Tag = 'span',
  icon,
  className,
  style,
  htmlFor,
}: LabelTextProps) {
  return (
    <Tag
      className={cn('label-text', className)}
      style={style}
      {...(Tag === 'label' && htmlFor ? { htmlFor } : {})}
    >
      {icon}
      {children}
    </Tag>
  );
}
