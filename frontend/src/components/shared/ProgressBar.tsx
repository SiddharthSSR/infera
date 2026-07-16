import type { CSSProperties } from 'react';
import { cn } from '../../lib/utils';

export interface ProgressBarProps {
  /** Percentage 0–100 */
  value: number;
  /** Override track height in px — default 4 */
  height?: number;
  /** Override fill color — default var(--text-primary) */
  fillColor?: string;
  /** Warning threshold — fill turns warning color above this */
  warnAt?: number;
  /** Error threshold — fill turns error color above this */
  errorAt?: number;
  /** Animate width on mount / value change */
  animate?: boolean;
  className?: string;
  style?: CSSProperties;
}

export function ProgressBar({
  value,
  height,
  fillColor,
  warnAt,
  errorAt,
  animate = true,
  className,
  style,
}: ProgressBarProps) {
  const clamped = Math.max(0, Math.min(100, value));

  let color = fillColor;
  if (!color) {
    if (errorAt != null && clamped >= errorAt) {
      color = 'var(--color-error)';
    } else if (warnAt != null && clamped >= warnAt) {
      color = 'var(--color-warning)';
    }
  }

  const trackStyle: CSSProperties = {};
  if (height) trackStyle.height = height;

  const fillStyle: CSSProperties = {
    width: `${clamped}%`,
  };
  if (color) fillStyle.background = color;
  if (animate) fillStyle.transition = 'width 0.6s ease';

  return (
    <div
      className={cn('progress-track', className)}
      style={{ ...trackStyle, ...style }}
      role="progressbar"
      aria-valuenow={clamped}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div className="progress-fill" style={fillStyle} />
    </div>
  );
}
