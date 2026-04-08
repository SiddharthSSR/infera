import { Skeleton, SkeletonMetricBlock } from './Skeleton';

/**
 * Rich loading skeleton for the Dashboard page.
 * Staggered fade-in for each row to convey progressive loading.
 */

function SkeletonServingCell({ delay }: { delay: number }) {
  return (
    <div className="cell dashboard-serving-cell" style={{ animation: `fade-in 0.4s ease-out ${delay}s both` }}>
      <Skeleton width="55%" height="0.65rem" />
      <div style={{ marginTop: '0.85rem' }}>
        <Skeleton width="30%" height="1.5rem" />
      </div>
      <div style={{ marginTop: '0.75rem' }}>
        <Skeleton width="90%" height="0.7rem" />
      </div>
      <div style={{ marginTop: '0.3rem' }}>
        <Skeleton width="65%" height="0.7rem" />
      </div>
      <div style={{ marginTop: '1rem' }}>
        <Skeleton width="45%" height="1.6rem" />
      </div>
    </div>
  );
}

function SkeletonAlertItem({ delay }: { delay: number }) {
  return (
    <div
      className="dashboard-alert-item"
      style={{
        animation: `fade-in 0.4s ease-out ${delay}s both`,
        border: '1px solid var(--border-color)',
        borderRadius: 4,
        padding: '1rem',
      }}
    >
      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
        <Skeleton width="4.5rem" height="1.2rem" />
        <Skeleton width="60%" height="0.9rem" />
      </div>
      <div style={{ marginTop: '0.6rem' }}>
        <Skeleton width="95%" height="0.7rem" />
      </div>
      <div style={{ marginTop: '0.3rem' }}>
        <Skeleton width="70%" height="0.7rem" />
      </div>
    </div>
  );
}

export function DashboardSkeleton() {
  return (
    <div className="dashboard-page animate-fade-in">
      {/* Top header row */}
      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 2', animation: 'fade-in 0.4s ease-out 0s both' }}>
          <Skeleton width="30%" height="0.65rem" />
          <div style={{ marginTop: '0.75rem' }}>
            <Skeleton width="55%" height="1.2rem" />
          </div>
          <div style={{ marginTop: '0.5rem' }}>
            <Skeleton width="80%" height="0.8rem" />
          </div>
          <div style={{ marginTop: '1.25rem', display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '1rem' }}>
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <div key={i}>
                <Skeleton width="60%" height="0.6rem" />
                <div style={{ marginTop: '0.5rem' }}>
                  <Skeleton width="40%" height="1rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)', animation: 'fade-in 0.4s ease-out 0.1s both' }}>
          <Skeleton width="25%" height="0.65rem" />
          <div style={{ marginTop: '0.75rem' }}>
            <Skeleton width="65%" height="1.2rem" />
          </div>
          <div style={{ marginTop: '0.5rem' }}>
            <Skeleton width="85%" height="0.8rem" />
          </div>
          <div style={{ marginTop: '1.25rem', display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: '1rem' }}>
            {[0, 1, 2, 3].map((i) => (
              <div key={i}>
                <Skeleton width="55%" height="0.6rem" />
                <div style={{ marginTop: '0.5rem' }}>
                  <Skeleton width="35%" height="1rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Metrics row */}
      <div className="grid-row dashboard-metrics-row">
        <SkeletonMetricBlock delay={0.15} />
        <SkeletonMetricBlock delay={0.22} />
        <SkeletonMetricBlock delay={0.29} />
        <SkeletonMetricBlock delay={0.36} />
      </div>

      {/* Serving status row */}
      <div className="grid-row dashboard-serving-row">
        <SkeletonServingCell delay={0.4} />
        <SkeletonServingCell delay={0.46} />
        <SkeletonServingCell delay={0.52} />
        <SkeletonServingCell delay={0.58} />
      </div>

      {/* Attention queue skeleton */}
      <div className="grid-row dashboard-alerts-row">
        <div className="cell dashboard-alerts-cell" style={{ gridColumn: 'span 4', animation: 'fade-in 0.4s ease-out 0.6s both' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '1.4rem' }}>
            <Skeleton width="25%" height="0.65rem" />
            <Skeleton width="5rem" height="1.2rem" />
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <SkeletonAlertItem delay={0.65} />
            <SkeletonAlertItem delay={0.72} />
          </div>
        </div>
      </div>
    </div>
  );
}
