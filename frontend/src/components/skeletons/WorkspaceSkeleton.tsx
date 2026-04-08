import { Skeleton, SkeletonSectionHeader } from '../Skeleton';

/**
 * Loading skeleton for the WorkspaceAdmin / Settings page.
 * Matches: profile row (span 2 + span 2), tab bar, tab content panel.
 */
export function WorkspaceSkeleton() {
  return (
    <div className="workspace-page animate-fade-in">
      {/* Hero row: profile (span 2) + quick stats (span 2) */}
      <div className="grid-row workspace-hero-row">
        <div className="cell workspace-profile-cell" style={{ gridColumn: 'span 2', animation: 'fade-in 0.4s ease-out 0s both' }}>
          <Skeleton width="10rem" height="0.65rem" />
          <div style={{ marginTop: '1rem' }}>
            <Skeleton width="50%" height="2rem" />
          </div>
          <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem' }}>
            <Skeleton width="4rem" height="1.2rem" />
            <Skeleton width="5rem" height="1.2rem" />
            <Skeleton width="6rem" height="1.2rem" />
          </div>
          <div style={{ marginTop: '1.1rem', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
            {[0, 1, 2, 3].map((i) => (
              <div key={i}>
                <Skeleton width="55%" height="0.6rem" />
                <div style={{ marginTop: '0.4rem' }}>
                  <Skeleton width="35%" height="1rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)', animation: 'fade-in 0.4s ease-out 0.08s both' }}>
          <SkeletonSectionHeader delay={0.08} />
          <div style={{ marginTop: '1rem' }}>
            <Skeleton width="90%" height="0.75rem" />
          </div>
          <div style={{ marginTop: '0.3rem' }}>
            <Skeleton width="70%" height="0.75rem" />
          </div>
        </div>
      </div>

      {/* Tab bar */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.15s both' }}>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div style={{ display: 'flex', gap: '1.25rem', borderBottom: '1px solid var(--border-color)', paddingBottom: '0.75rem' }}>
            {['USAGE', 'PROVIDERS', 'SERVICE ACCTS', 'MEMBERS', 'INVITES'].map((tab, i) => (
              <Skeleton key={tab} width={`${tab.length * 6 + 16}px`} height="0.65rem" delay={0.16 + i * 0.03} />
            ))}
          </div>
        </div>
      </div>

      {/* Tab content panel */}
      <div className="grid-row" style={{ animation: 'fade-in 0.4s ease-out 0.25s both' }}>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <SkeletonSectionHeader delay={0.25} />
          <div style={{ marginTop: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
            {[0, 1, 2].map((i) => (
              <div key={i}>
                <Skeleton width="30%" height="0.6rem" />
                <div style={{ marginTop: '0.5rem' }}>
                  <Skeleton width="100%" height="2rem" />
                </div>
              </div>
            ))}
            <Skeleton width="10rem" height="2.4rem" delay={0.35} />
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <SkeletonSectionHeader delay={0.3} />
          <div style={{ marginTop: '1rem' }}>
            <Skeleton width="95%" height="0.75rem" />
          </div>
          <div style={{ marginTop: '0.3rem' }}>
            <Skeleton width="80%" height="0.75rem" />
          </div>
          <div style={{ marginTop: '1.25rem', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
            {[0, 1, 2, 3].map((i) => (
              <div key={i}>
                <Skeleton width="50%" height="0.6rem" />
                <div style={{ marginTop: '0.4rem' }}>
                  <Skeleton width="35%" height="1rem" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
