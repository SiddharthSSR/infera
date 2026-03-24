import type { ReactNode } from 'react';
import { cn } from '../lib/utils';

type SectionHeaderProps = {
  eyebrow?: string;
  title: ReactNode;
  description?: ReactNode;
  badge?: ReactNode;
  actions?: ReactNode;
  className?: string;
  compact?: boolean;
};

export function SectionHeader({
  eyebrow,
  title,
  description,
  badge,
  actions,
  className,
  compact = false,
}: SectionHeaderProps) {
  return (
    <div className={cn('section-header', compact && 'compact', className)}>
      <div className="section-header-copy">
        {eyebrow ? <div className="label-text">{eyebrow}</div> : null}
        <div className="section-header-title-row">
          <div className="section-header-title">{title}</div>
          {badge ? <div className="section-header-badge">{badge}</div> : null}
        </div>
        {description ? <div className="section-header-description">{description}</div> : null}
      </div>
      {actions ? <div className="section-header-actions">{actions}</div> : null}
    </div>
  );
}
