import { cn } from '../../lib/utils';

export type AppShellVariant = 'auth' | 'public' | 'bare';

export interface AppShellProps {
  /** Controls the shell width, borders, and container class */
  variant?: AppShellVariant;
  /** Override the default max-width for the shell */
  maxWidth?: string | number;
  className?: string;
  style?: React.CSSProperties;
  children: React.ReactNode;
}

const variantClasses: Record<AppShellVariant, string> = {
  auth: 'app-shell app-shell-auth',
  public: 'app-shell docs-shell',
  bare: 'app-shell',
};

/**
 * Unified layout wrapper that handles the max-width container,
 * border-left/right grid lines, border-radius, and responsive breakpoints.
 *
 * Every page should be wrapped in an AppShell instead of duplicating
 * the container div with `app-shell` / `app-shell-auth` / `docs-shell` classes.
 */
export function AppShell({
  variant = 'auth',
  maxWidth,
  className,
  style,
  children,
}: AppShellProps) {
  const shellStyle = maxWidth
    ? { ...style, maxWidth: typeof maxWidth === 'number' ? `${maxWidth}px` : maxWidth }
    : style;

  const inner = (
    <div
      className={cn(variantClasses[variant], className)}
      style={shellStyle}
    >
      {children}
    </div>
  );

  // Public variant wraps in docs-page for the background gradient
  if (variant === 'public') {
    return <div className="docs-page">{inner}</div>;
  }

  return inner;
}
