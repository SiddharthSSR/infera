import { cn } from '../lib/utils';

interface EmptyStateProps {
  title: string;
  description?: string;
  action?: {
    label: string;
    onClick: () => void;
  };
  className?: string;
}

export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn('empty-state', className)}
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '3rem 2rem',
        textAlign: 'center',
        gap: '0.75rem',
      }}
    >
      <div
        className="label-text"
        style={{ color: 'var(--text-tertiary, var(--text-secondary))' }}
      >
        {title.toUpperCase()}
      </div>
      {description && (
        <div
          style={{
            fontSize: '0.85rem',
            color: 'var(--text-secondary)',
            lineHeight: 1.65,
            maxWidth: '36ch',
          }}
        >
          {description}
        </div>
      )}
      {action && (
        <button
          className="btn-quiet"
          onClick={action.onClick}
          type="button"
          style={{ marginTop: '0.5rem' }}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}
