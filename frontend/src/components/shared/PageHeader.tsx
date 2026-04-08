import { cn } from '../../lib/utils';

export interface PageHeaderProps {
  eyebrow: string;
  title: string;
  description: string;
  className?: string;
}

/**
 * Standard page header with eyebrow, title, and description.
 * Used on authenticated pages (except playground and docs routes).
 */
export function PageHeader({ eyebrow, title, description, className }: PageHeaderProps) {
  return (
    <header className={cn('page-header', className)}>
      <div className="page-header-eyebrow">{eyebrow}</div>
      <div className="page-header-title">{title}</div>
      <p className="page-header-description">{description}</p>
    </header>
  );
}
