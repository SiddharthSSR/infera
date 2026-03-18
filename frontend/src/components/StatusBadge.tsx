import { cn } from '../lib/utils';

export type StatusTone = 'success' | 'warning' | 'error' | 'neutral' | 'info';

export function toneFromStatus(status: string): StatusTone {
  const s = status.toLowerCase();
  if (['healthy', 'active', 'online', 'verified', 'serving_verified', 'connected', 'ready'].includes(s)) return 'success';
  if (['degraded', 'warning', 'slow', 'partial', 'serving_unverified', 'setup_in_progress'].includes(s)) return 'warning';
  if (['error', 'failed', 'offline', 'unreachable', 'dead'].includes(s)) return 'error';
  if (['info', 'pending', 'provisioning', 'starting'].includes(s)) return 'info';
  return 'neutral';
}

interface StatusBadgeProps {
  label: string;
  tone?: StatusTone;
  status?: string; // derive tone automatically if provided
  dot?: boolean;
  className?: string;
}

const toneStyles: Record<StatusTone, { dot: string; badge: string }> = {
  success: {
    dot: 'background-color: var(--color-success)',
    badge: 'color: var(--color-success)',
  },
  warning: {
    dot: 'background-color: var(--color-warning)',
    badge: 'color: var(--color-warning)',
  },
  error: {
    dot: 'background-color: var(--color-error)',
    badge: 'color: var(--color-error)',
  },
  info: {
    dot: 'background-color: var(--color-info)',
    badge: 'color: var(--color-info)',
  },
  neutral: {
    dot: 'background-color: var(--text-secondary)',
    badge: 'color: var(--text-secondary)',
  },
};

const toneClasses: Record<StatusTone, { dot: string; text: string }> = {
  success: { dot: 'status-dot', text: 'text-success' },
  warning: { dot: 'status-dot warning', text: 'text-warning' },
  error: { dot: 'status-dot inactive', text: 'text-error' },
  info: { dot: 'status-dot info', text: 'text-info' },
  neutral: { dot: 'status-dot neutral', text: 'text-neutral' },
};

export function StatusBadge({ label, tone, status, dot = true, className }: StatusBadgeProps) {
  const resolvedTone: StatusTone = tone ?? (status ? toneFromStatus(status) : 'neutral');
  const tc = toneClasses[resolvedTone];
  const ts = toneStyles[resolvedTone];

  return (
    <span
      className={cn('status-badge', className)}
      style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
    >
      {dot && (
        <span
          className={tc.dot}
          style={{ flexShrink: 0 }}
        />
      )}
      <span
        className="label-text"
        style={{ ...parseStyleString(ts.badge), fontSize: '0.65rem' }}
      >
        {label.toUpperCase()}
      </span>
    </span>
  );
}

// Helper to convert inline style string to React style object
function parseStyleString(styleStr: string): React.CSSProperties {
  const result: Record<string, string> = {};
  styleStr.split(';').forEach((part) => {
    const [prop, val] = part.split(':').map((s) => s.trim());
    if (prop && val) {
      const camel = prop.replace(/-([a-z])/g, (_, c: string) => c.toUpperCase());
      result[camel] = val;
    }
  });
  return result as React.CSSProperties;
}
