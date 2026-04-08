import { Skeleton, SkeletonSectionHeader, SkeletonTableRow } from '../Skeleton';

/**
 * Loading skeleton for the API Keys page.
 * Matches: session info (span 2) + summary (span 2), keys table (span 3) + create form (span 1).
 */
export function ApiKeysSkeleton() {
  return (
    <div className="animate-fade-in api-keys-page">
      {/* Session row: info (span 2) + summary (span 2, bg-accent) */}
      <div className="grid-row api-keys-session-row">
        <div className="cell api-keys-session-cell" style={{ gridColumn: 'span 2', animation: 'fade-in 0.4s ease-out 0s both' }}>
          <SkeletonSectionHeader />
          <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem' }}>
            <Skeleton width="5rem" height="1.2rem" />
            <Skeleton width="7rem" height="1.2rem" />
          </div>
        </div>
        <div className="cell api-keys-summary-cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)', animation: 'fade-in 0.4s ease-out 0.08s both' }}>
          <SkeletonSectionHeader delay={0.08} />
          <div style={{ marginTop: '1.2rem', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
            {[0, 1, 2, 3].map((i) => (
              <div key={i}>
                <Skeleton width="60%" height="0.6rem" />
                <div style={{ marginTop: '0.4rem' }}>
                  <Skeleton width="30%" height="1rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Keys table (span 3) + create form (span 1) */}
      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 3', animation: 'fade-in 0.4s ease-out 0.15s both' }}>
          <SkeletonSectionHeader delay={0.15} />
          <div style={{ marginTop: '1.5rem', overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  {['NAME', 'PREFIX', 'ROLE', 'SCOPE', 'USAGE', 'CREATED', 'ACTIONS'].map((col) => (
                    <th key={col} className="label-text" style={{ padding: '0.6rem 1rem', borderBottom: '1px solid var(--text-primary)', textAlign: 'left' }}>
                      <Skeleton width={`${col.length * 7 + 10}px`} height="0.6rem" />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Array.from({ length: 5 }).map((_, i) => (
                  <SkeletonTableRow key={i} columns={7} delay={0.2 + i * 0.04} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 1', backgroundColor: 'var(--bg-accent)', animation: 'fade-in 0.4s ease-out 0.2s both' }}>
          <SkeletonSectionHeader delay={0.2} />
          <div style={{ marginTop: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
            <div>
              <Skeleton width="30%" height="0.6rem" />
              <div style={{ marginTop: '0.5rem' }}>
                <Skeleton width="100%" height="2rem" />
              </div>
            </div>
            <div>
              <Skeleton width="35%" height="0.6rem" />
              <div style={{ marginTop: '0.5rem' }}>
                <Skeleton width="100%" height="2rem" />
              </div>
            </div>
            <div>
              <Skeleton width="40%" height="0.6rem" />
              <div style={{ marginTop: '0.5rem' }}>
                <Skeleton width="100%" height="2rem" />
              </div>
            </div>
            <Skeleton width="100%" height="2.4rem" delay={0.3} />
          </div>
        </div>
      </div>
    </div>
  );
}
