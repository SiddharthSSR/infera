import { type ReactNode } from 'react';
import { useCountUp } from '../hooks/useCountUp';

/* ------------------------------------------------------------------ */
/*  Shared sub-components                                              */
/* ------------------------------------------------------------------ */

function normalizeMetricSamples(samples: number[], minimumBars = 6) {
  const normalized = samples
    .filter((sample) => Number.isFinite(sample))
    .slice(-minimumBars)
    .map((sample) => (sample > 0 ? sample : 0));

  while (normalized.length < minimumBars) normalized.unshift(0);
  return normalized;
}

/**
 * Bar chart sparkline with grow-up animation.
 * Each bar transitions from 0% to its final height with a staggered delay.
 *
 * @param baseDelay – ms offset before the first bar starts growing (lets us
 *                    stagger the whole chart relative to the parent card).
 */
function ChartBars({ samples, baseDelay = 0 }: { samples: number[]; baseDelay?: number }) {
  const normalized = normalizeMetricSamples(samples);
  const max = Math.max(...normalized, 0);
  const activeIndex = normalized.reduce(
    (lastPositiveIndex, sample, index) => (sample > 0 ? index : lastPositiveIndex),
    -1,
  );
  const heights = normalized.map((sample) => {
    if (max <= 0) return 18;
    if (sample <= 0) return 16;
    return Math.max(22, Math.round((sample / max) * 100));
  });

  return (
    <div className="metric-chart">
      {heights.map((h, i) => (
        <div
          key={i}
          className={`chart-bar chart-bar-animated ${i === (activeIndex >= 0 ? activeIndex : heights.length - 1) ? 'active' : ''}`}
          style={{
            '--bar-height': `${h}%`,
            '--bar-delay': `${baseDelay + i * 60}ms`,
          } as React.CSSProperties}
        />
      ))}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Animated numeric value                                             */
/* ------------------------------------------------------------------ */

function AnimatedValue({ value, format = 'integer', suffix = '', delay = 0 }: {
  value: number;
  format?: 'integer' | 'decimal' | 'compact';
  suffix?: string;
  delay?: number;
}) {
  const animated = useCountUp(value, 1200, delay);

  let displayed: string;
  switch (format) {
    case 'decimal':
      displayed = animated.toFixed(1);
      break;
    case 'compact':
      if (animated >= 1_000_000) displayed = `${(animated / 1_000_000).toFixed(1)}M`;
      else if (animated >= 1_000) displayed = `${(animated / 1_000).toFixed(1)}K`;
      else displayed = String(Math.round(animated));
      break;
    default:
      displayed = String(Math.round(animated));
  }

  return <>{displayed}{suffix}</>;
}

/* ------------------------------------------------------------------ */
/*  MetricCard                                                         */
/* ------------------------------------------------------------------ */

export type MetricCardProps = {
  icon: ReactNode;
  label: string;
  /** Raw numeric value for count-up animation */
  value: number;
  /** How to format the animated number */
  format?: 'integer' | 'decimal' | 'compact';
  /** Suffix appended after the number (e.g. "ms", " r/s") */
  suffix?: string;
  /** Override the value display entirely (for compound values like "3 / 5") */
  displayOverride?: string;
  /** Samples array for the sparkline chart */
  samples?: number[];
  /** Optional progress bar instead of chart (0-1 range) */
  progress?: number;
  /** Status dot + inline label below the metric */
  statusIndicator?: { active: boolean; label: string };
  /** Contextual note below the chart */
  note?: string;
  /**
   * Index (0-based) used to stagger the card's entrance.
   * Each increment adds 80ms of animation-delay to the fade-in,
   * count-up start, and chart bar grow animation.
   */
  staggerIndex?: number;
};

export function MetricCard({
  icon,
  label,
  value,
  format = 'integer',
  suffix = '',
  displayOverride,
  samples,
  progress,
  statusIndicator,
  note,
  staggerIndex = 0,
}: MetricCardProps) {
  const staggerDelayMs = staggerIndex * 80;

  return (
    <div
      className="cell dashboard-metric-card"
      style={{
        animation: `fade-in 0.4s ease-out ${staggerDelayMs}ms both`,
      }}
    >
      <div className="label-text">
        {icon}
        {label}
      </div>
      <div className="value-text">
        {displayOverride ?? <AnimatedValue value={value} format={format} suffix={suffix} delay={staggerDelayMs} />}
      </div>
      {samples && <ChartBars samples={samples} baseDelay={staggerDelayMs + 200} />}
      {progress != null && (
        <div className="progress-track" aria-hidden="true">
          <div
            className="progress-fill"
            style={{ width: `${Math.round(progress * 100)}%`, transition: 'width 0.6s ease-out' }}
          />
        </div>
      )}
      {statusIndicator && (
        <div className="dashboard-metric-inline">
          <span className={`status-dot ${statusIndicator.active ? '' : 'inactive'}`} />
          <span>{statusIndicator.label}</span>
        </div>
      )}
      {note && <div className="dashboard-metric-note">{note}</div>}
    </div>
  );
}
