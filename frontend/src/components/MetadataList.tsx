import { Fragment, type ReactNode } from 'react';
import { cn } from '../lib/utils';

export type MetadataItem = {
  label: string;
  value: ReactNode;
  tone?: '' | 'warning' | 'error' | 'inactive';
  mono?: boolean;
};

type MetadataListProps = {
  items: MetadataItem[];
  className?: string;
  columns?: 1 | 2 | 3;
};

export function MetadataList({ items, className, columns = 2 }: MetadataListProps) {
  return (
    <div className={cn('metadata-list', `columns-${columns}`, className)}>
      {items.map((item) => (
        <Fragment key={item.label}>
          <div className="metadata-label">{item.label}</div>
          <div className={cn('metadata-value', item.mono && 'mono', item.tone && `tone-${item.tone}`)}>
            {item.value}
          </div>
        </Fragment>
      ))}
    </div>
  );
}
