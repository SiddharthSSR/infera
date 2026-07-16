import { Skeleton, SkeletonTableRow } from '../Skeleton';

/**
 * Loading skeleton for the Logs page.
 * Matches: filter bar, log table with level/timestamp/source/message columns, footer.
 */
export function LogsSkeleton() {
  return (
    <div className="animate-fade-in" style={{ display: 'flex', flexDirection: 'column', minHeight: '60vh' }}>
      {/* Filter bar */}
      <div style={{
        padding: '1rem 2rem',
        borderBottom: 'var(--grid-line)',
        display: 'flex',
        gap: '0.75rem',
        alignItems: 'center',
        flexWrap: 'wrap',
        animation: 'fade-in 0.4s ease-out 0s both',
      }}>
        <Skeleton width="14rem" height="2rem" />
        <Skeleton width="8rem" height="2rem" />
        <Skeleton width="8rem" height="2rem" />
        <div style={{ flex: 1 }} />
        <Skeleton width="6rem" height="0.65rem" />
      </div>

      {/* Log table */}
      <div style={{ flex: 1, overflow: 'hidden', animation: 'fade-in 0.4s ease-out 0.1s both' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              {['LEVEL', 'TIMESTAMP', 'SOURCE', 'MESSAGE'].map((col) => (
                <th key={col} className="label-text" style={{ padding: '0.6rem 1rem', borderBottom: '1px solid var(--text-primary)', textAlign: 'left' }}>
                  <Skeleton width={`${col.length * 7 + 10}px`} height="0.6rem" />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: 12 }).map((_, i) => (
              <SkeletonTableRow key={i} columns={4} delay={0.15 + i * 0.03} />
            ))}
          </tbody>
        </table>
      </div>

      {/* Footer */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.5s both' }}>
        <div className="cell" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <Skeleton width="8px" height="8px" style={{ borderRadius: '50%' }} />
          <Skeleton width="8rem" height="0.65rem" />
        </div>
        <div className="cell">
          <Skeleton width="6rem" height="0.65rem" />
        </div>
        <div className="cell">
          <Skeleton width="5rem" height="0.65rem" />
        </div>
        <div className="cell">
          <Skeleton width="7rem" height="0.65rem" />
        </div>
      </div>
    </div>
  );
}
