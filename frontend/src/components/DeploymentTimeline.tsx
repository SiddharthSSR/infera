import type { DeploymentTimelineStep } from '../lib/deploymentHistory';
import { timelineTone, timelineLabel } from '../lib/labels';

export function DeploymentTimeline({ steps }: { steps: DeploymentTimelineStep[] }) {
  return (
    <div style={{ display: 'grid', gap: '0.55rem', marginTop: '0.9rem' }}>
      {steps.map((step) => {
        const tone = timelineTone(step.state);
        return (
          <div key={step.label} style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap' }}>
            <span className={`status-dot ${tone}`} />
            <span style={{ fontSize: '0.8rem', minWidth: '8.5rem' }}>{step.label}</span>
            <span className={`badge ${tone ? `status-${tone}` : ''}`}>{timelineLabel(step.state)}</span>
          </div>
        );
      })}
    </div>
  );
}
