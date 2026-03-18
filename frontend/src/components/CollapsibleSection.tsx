import { useState, type ReactNode } from 'react';
import { cn } from '../lib/utils';

type CollapsibleSectionProps = {
  title: ReactNode;
  description?: ReactNode;
  badge?: ReactNode;
  defaultExpanded?: boolean;
  children: ReactNode;
  className?: string;
  summary?: ReactNode;
};

export function CollapsibleSection({
  title,
  description,
  badge,
  defaultExpanded = false,
  children,
  className,
  summary,
}: CollapsibleSectionProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  return (
    <section className={cn('collapsible-section', expanded && 'expanded', className)}>
      <button
        type="button"
        className="collapsible-section-toggle"
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
      >
        <div className="collapsible-section-head">
          <div className="collapsible-section-copy">
            <div className="section-header-title-row">
              <div className="section-header-title">{title}</div>
              {badge ? <div className="section-header-badge">{badge}</div> : null}
            </div>
            {description ? <div className="section-header-description">{description}</div> : null}
            {!expanded && summary ? <div className="collapsible-section-summary">{summary}</div> : null}
          </div>
          <span className="collapsible-section-state">{expanded ? 'HIDE' : 'SHOW'}</span>
        </div>
      </button>
      {expanded ? <div className="collapsible-section-body">{children}</div> : null}
    </section>
  );
}
