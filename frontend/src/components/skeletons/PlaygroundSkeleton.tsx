import { Skeleton, SkeletonSidebar } from '../Skeleton';

/**
 * Loading skeleton for the Playground page.
 * Matches: 3-column layout — settings sidebar | prompt/response editor | history sidebar.
 */
export function PlaygroundSkeleton() {
  return (
    <div className="animate-fade-in" style={{
      display: 'grid',
      gridTemplateColumns: '252px minmax(0, 1fr) 236px',
      minHeight: '70vh',
      borderTop: 'var(--grid-line)',
    }}>
      {/* Left sidebar — settings */}
      <div style={{
        padding: '1.5rem 1.25rem',
        borderRight: 'var(--grid-line)',
        animation: 'fade-in 0.4s ease-out 0s both',
      }}>
        <Skeleton width="60%" height="0.65rem" />
        <div style={{ marginTop: '1rem' }}>
          <Skeleton width="100%" height="2.2rem" />
        </div>
        <div style={{ marginTop: '1.5rem' }}>
          <SkeletonSidebar items={5} delay={0.05} />
        </div>
      </div>

      {/* Center — prompt + response */}
      <div style={{
        padding: '1.5rem 2rem',
        display: 'flex',
        flexDirection: 'column',
        gap: '1.5rem',
        animation: 'fade-in 0.4s ease-out 0.1s both',
      }}>
        {/* Toolbar */}
        <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
          <Skeleton width="6rem" height="0.65rem" />
          <div style={{ flex: 1 }} />
          <Skeleton width="5rem" height="1.8rem" />
          <Skeleton width="5rem" height="1.8rem" />
        </div>
        {/* Prompt area */}
        <Skeleton width="100%" height="8rem" delay={0.15} />
        {/* Response area */}
        <div style={{ flex: 1 }}>
          <Skeleton width="12%" height="0.65rem" delay={0.18} />
          <div style={{ marginTop: '0.75rem' }}>
            <Skeleton width="100%" height="12rem" delay={0.2} />
          </div>
        </div>
        {/* Token usage bar */}
        <div style={{ display: 'flex', gap: '1.5rem' }}>
          <Skeleton width="6rem" height="0.6rem" delay={0.25} />
          <Skeleton width="4rem" height="0.6rem" delay={0.27} />
          <Skeleton width="5rem" height="0.6rem" delay={0.29} />
        </div>
      </div>

      {/* Right sidebar — history */}
      <div style={{
        padding: '1.5rem 1.25rem',
        borderLeft: 'var(--grid-line)',
        animation: 'fade-in 0.4s ease-out 0.2s both',
      }}>
        <Skeleton width="50%" height="0.65rem" />
        <div style={{ marginTop: '1.25rem', display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} style={{ padding: '0.75rem', border: '1px solid var(--border-color)', borderRadius: 4 }}>
              <Skeleton width="80%" height="0.75rem" delay={0.25 + i * 0.04} />
              <div style={{ marginTop: '0.5rem' }}>
                <Skeleton width="50%" height="0.6rem" delay={0.27 + i * 0.04} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
