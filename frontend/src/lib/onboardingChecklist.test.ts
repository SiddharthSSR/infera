import { describe, expect, it } from 'vitest';
import { buildFirstWorkspaceChecklist } from './onboardingChecklist';

describe('buildFirstWorkspaceChecklist', () => {
  it('marks steps complete from live workspace state', () => {
    const checklist = buildFirstWorkspaceChecklist({
      providerReady: true,
      providerConnected: true,
      modelReady: true,
      nodeReady: true,
      inferenceVerified: true,
      collaborationReady: true,
    });

    expect(checklist.every((item) => item.done)).toBe(true);
  });

  it('keeps unmet steps actionable', () => {
    const checklist = buildFirstWorkspaceChecklist({
      providerReady: true,
      providerConnected: false,
      modelReady: false,
      nodeReady: false,
      inferenceVerified: false,
      collaborationReady: false,
    });

    expect(checklist[0]).toMatchObject({ done: false, action: 'open_workspace' });
    expect(checklist[0]).toMatchObject({
      label: 'Add provider access',
      actionLabel: 'CONNECT PROVIDER',
    });
    expect(checklist[0]?.detail).toContain('Start here');
    expect(checklist[1]).toMatchObject({ done: false, action: 'open_models' });
    expect(checklist[2]).toMatchObject({ done: false, action: 'open_clusters' });
    expect(checklist[3]).toMatchObject({ done: false, action: 'open_clusters' });
    expect(checklist[4]).toMatchObject({ done: false, action: 'open_workspace' });
    expect(checklist[4]?.detail).toContain('Invite a human for dashboard access');
    expect(checklist[4]?.detail).toContain('service account for unattended automation');
  });
});
