import type { ReactNode } from 'react';
import { cn } from '../lib/utils';

type ActionGroupProps = {
  children: ReactNode;
  className?: string;
  compact?: boolean;
};

export function ActionGroup({ children, className, compact = false }: ActionGroupProps) {
  return (
    <div className={cn('action-group', compact && 'compact', className)}>
      {children}
    </div>
  );
}
