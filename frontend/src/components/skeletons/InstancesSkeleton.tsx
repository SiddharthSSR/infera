import { Skeleton, SkeletonSectionHeader, SkeletonTableRow } from '../Skeleton';

/**
 * Loading skeleton for the Instances / Nodes page.
 * Matches: latest deployment banner, status guide, instance table.
 */
export function InstancesSkeleton() {
  return (
    <div className="instances-page animate-fade-in">
      {/* Latest deployment banner */}
      <div style={{ padding: '1.25rem 2rem', borderBottom: 'var(--grid-line)', animation: 'fade-in 0.4s ease-out 0s both' }}>
        <Skeleton width="10rem" height="0.65rem" />
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', marginTop: '0.5rem' }}>
          <div style={{ flex: 1 }}>
            <Skeleton width="40%" height="1rem" />
            <div style={{ marginTop: '0.6rem' }}>
              <Skeleton width="75%" height="0.8rem" />
            </div>
            <div style={{ marginTop: '0.3rem' }}>
              <Skeleton width="55%" height="0.75rem" />
            </div>
            {/* Timeline dots */}
            <div style={{ marginTop: '1rem', display: 'flex', gap: '1.5rem', alignItems: 'center' }}>
              {[0, 1, 2, 3].map((i) => (
                <div key={i} style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                  <Skeleton width="10px" height="10px" style={{ borderRadius: '50%' }} />
                  <Skeleton width="4rem" height="0.6rem" />
                </div>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <Skeleton width="6rem" height="1.4rem" />
            <Skeleton width="7rem" height="1.4rem" />
          </div>
        </div>
      </div>

      {/* Status guide callout */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.1s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div className="help-callout" style={{ padding: '1rem 1.25rem' }}>
            <SkeletonSectionHeader delay={0.12} />
            <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem' }}>
              <Skeleton width="8rem" height="1rem" />
              <Skeleton width="10rem" height="1rem" />
            </div>
          </div>
        </div>
      </div>

      {/* Instance table */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.2s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          {/* Filter bar */}
          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', marginBottom: '1.5rem', flexWrap: 'wrap' }}>
            <Skeleton width="12rem" height="2rem" />
            <Skeleton width="8rem" height="1.4rem" />
            <Skeleton width="6rem" height="1.4rem" />
            <div style={{ flex: 1 }} />
            <Skeleton width="10rem" height="2.2rem" />
          </div>

          {/* Table */}
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  {['NODE', 'STATUS', 'GPU', 'MODEL', 'GPU UTIL', 'MEMORY', 'UPTIME', 'ACTIONS'].map((col) => (
                    <th key={col} className="label-text" style={{ padding: '0.6rem 1rem', borderBottom: '1px solid var(--text-primary)', textAlign: 'left' }}>
                      <Skeleton width={`${col.length * 7 + 10}px`} height="0.6rem" />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Array.from({ length: 6 }).map((_, i) => (
                  <SkeletonTableRow key={i} columns={8} delay={0.25 + i * 0.05} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
