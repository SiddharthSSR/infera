import type { CSSProperties } from 'react';

/* ------------------------------------------------------------------ */
/*  Primitive                                                          */
/* ------------------------------------------------------------------ */

interface SkeletonProps {
  width?: string;
  height?: string;
  /** Extra delay (seconds) for stagger effect */
  delay?: number;
  style?: CSSProperties;
}

export function Skeleton({ width = '100%', height = '1rem', delay, style }: SkeletonProps) {
  return (
    <div
      style={{
        width,
        height,
        backgroundColor: 'var(--bg-accent)',
        borderRadius: 2,
        animation: 'skeleton-pulse 1.5s ease-in-out infinite',
        ...(delay != null ? { animationDelay: `${delay}s` } : {}),
        ...style,
      }}
    />
  );
}

/* ------------------------------------------------------------------ */
/*  Composites                                                         */
/* ------------------------------------------------------------------ */

/** Generic grid cell skeleton with label + value + bar */
export function SkeletonCell({ delay = 0 }: { delay?: number }) {
  return (
    <div className="cell" style={{ minHeight: 140, animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <Skeleton width="40%" height="0.65rem" />
      <div style={{ marginTop: '1rem' }}>
        <Skeleton width="60%" height="1.5rem" />
      </div>
      <div style={{ marginTop: '1rem' }}>
        <Skeleton width="100%" height="40px" />
      </div>
    </div>
  );
}

/** Table row skeleton — renders N cell placeholders in a flex row */
export function SkeletonTableRow({ columns = 5, delay = 0 }: { columns?: number; delay?: number }) {
  return (
    <tr style={{ animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      {Array.from({ length: columns }).map((_, i) => (
        <td key={i} style={{ padding: '0.85rem 1rem' }}>
          <Skeleton
            width={i === 0 ? '70%' : i === columns - 1 ? '50%' : '60%'}
            height="0.85rem"
          />
        </td>
      ))}
    </tr>
  );
}

/** Table header skeleton */
export function SkeletonTableHeader({ columns = 5 }: { columns?: number }) {
  return (
    <thead>
      <tr>
        {Array.from({ length: columns }).map((_, i) => (
          <th key={i} className="label-text" style={{ padding: '0.6rem 1rem', borderBottom: '1px solid var(--text-primary)' }}>
            <Skeleton width={`${40 + (i % 3) * 15}%`} height="0.6rem" />
          </th>
        ))}
      </tr>
    </thead>
  );
}

/** Full table skeleton with header + rows */
export function SkeletonTable({ columns = 5, rows = 5, delay = 0 }: { columns?: number; rows?: number; delay?: number }) {
  return (
    <div style={{ overflowX: 'auto', animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <SkeletonTableHeader columns={columns} />
        <tbody>
          {Array.from({ length: rows }).map((_, i) => (
            <SkeletonTableRow key={i} columns={columns} delay={delay + 0.05 * i} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Section header skeleton — eyebrow + title + description */
export function SkeletonSectionHeader({ delay = 0 }: { delay?: number }) {
  return (
    <div style={{ animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <Skeleton width="25%" height="0.65rem" />
      <div style={{ marginTop: '0.75rem' }}>
        <Skeleton width="55%" height="1.2rem" />
      </div>
      <div style={{ marginTop: '0.5rem' }}>
        <Skeleton width="80%" height="0.8rem" />
      </div>
    </div>
  );
}

/** Sidebar panel skeleton — label + stacked controls */
export function SkeletonSidebar({ items = 6, delay = 0 }: { items?: number; delay?: number }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      {Array.from({ length: items }).map((_, i) => (
        <div key={i}>
          <Skeleton width="45%" height="0.6rem" />
          <div style={{ marginTop: '0.6rem' }}>
            <Skeleton width="100%" height="2rem" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** Metric block skeleton matching Dashboard metric cards */
export function SkeletonMetricBlock({ delay = 0 }: { delay?: number }) {
  return (
    <div className="cell" style={{ animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <Skeleton width="35%" height="0.65rem" />
      <div style={{ marginTop: '1rem' }}>
        <Skeleton width="50%" height="1.8rem" />
      </div>
      <div style={{ marginTop: '1.2rem', display: 'flex', gap: 4, alignItems: 'flex-end', height: 48 }}>
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton
            key={i}
            width="100%"
            height={`${20 + ((i * 17 + 31) % 60)}%`}
            delay={i * 0.1}
            style={{ flex: 1, borderRadius: 1 }}
          />
        ))}
      </div>
      <div style={{ marginTop: '0.8rem' }}>
        <Skeleton width="80%" height="0.7rem" />
      </div>
    </div>
  );
}

/** Toolbar skeleton — search bar + badges */
export function SkeletonToolbar({ delay = 0 }: { delay?: number }) {
  return (
    <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap', animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <Skeleton width="16rem" height="2rem" />
      <Skeleton width="8rem" height="1.4rem" />
      <Skeleton width="6rem" height="1.4rem" />
      <Skeleton width="9rem" height="1.4rem" />
    </div>
  );
}
