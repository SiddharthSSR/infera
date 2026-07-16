import { Skeleton, SkeletonToolbar, SkeletonSectionHeader, SkeletonTableRow } from '../Skeleton';

/**
 * Loading skeleton for the Models / Registry page.
 * Matches: toolbar with search + badges, status guide row, recommended cards, table.
 */
export function ModelsSkeleton() {
  return (
    <div className="models-page animate-fade-in">
      {/* Toolbar: search + badge strip */}
      <div className="models-toolbar" style={{ animation: 'fade-in 0.4s ease-out 0s both' }}>
        <div className="models-toolbar-copy">
          <SkeletonToolbar />
        </div>
        <Skeleton width="8rem" height="2rem" delay={0.05} />
      </div>

      {/* Status guide callout */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.1s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div className="help-callout" style={{ padding: '1rem 1.25rem' }}>
            <SkeletonSectionHeader delay={0.12} />
          </div>
        </div>
      </div>

      {/* Recommended models row */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.2s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <Skeleton width="20%" height="0.65rem" />
          <div style={{ marginTop: '1rem', display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: '1rem' }}>
            {[0, 1, 2, 3].map((i) => (
              <div key={i} style={{ padding: '1.25rem', border: '1px solid var(--border-color)', borderRadius: 4 }}>
                <Skeleton width="60%" height="1rem" delay={0.25 + i * 0.05} />
                <div style={{ marginTop: '0.75rem' }}>
                  <Skeleton width="90%" height="0.7rem" delay={0.27 + i * 0.05} />
                </div>
                <div style={{ marginTop: '0.35rem' }}>
                  <Skeleton width="70%" height="0.7rem" delay={0.29 + i * 0.05} />
                </div>
                <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.5rem' }}>
                  <Skeleton width="4rem" height="1.2rem" />
                  <Skeleton width="5rem" height="1.2rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Model table */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.35s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4', overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['MODEL', 'PROVIDER', 'STATUS', 'QUANT', 'CONTEXT', 'ACTIONS'].map((col) => (
                  <th key={col} className="label-text" style={{ padding: '0.6rem 1rem', borderBottom: '1px solid var(--text-primary)', textAlign: 'left' }}>
                    <Skeleton width={`${col.length * 8 + 10}px`} height="0.6rem" />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 8 }).map((_, i) => (
                <SkeletonTableRow key={i} columns={6} delay={0.4 + i * 0.04} />
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
