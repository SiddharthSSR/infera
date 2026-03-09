export function Skeleton({ width = '100%', height = '1rem' }: { width?: string; height?: string }) {
  return (
    <div
      style={{
        width,
        height,
        backgroundColor: 'var(--bg-accent)',
        borderRadius: 2,
        animation: 'skeleton-pulse 1.5s ease-in-out infinite',
      }}
    />
  );
}

export function SkeletonCell() {
  return (
    <div className="cell" style={{ minHeight: 140 }}>
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
