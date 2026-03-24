import { describe, expect, it } from 'vitest';
import { buildWorkspaceMaturity } from './workspaceMaturity';
import type { OnboardingItem } from './onboardingChecklist';

function checklist(items: Partial<OnboardingItem>[]): OnboardingItem[] {
  return items.map((item, index) => ({
    id: item.id || `item_${index}`,
    label: item.label || `Item ${index}`,
    detail: item.detail || `Detail ${index}`,
    done: item.done ?? false,
    actionLabel: item.actionLabel || 'OPEN',
    action: item.action || 'open_workspace',
  }));
}

describe('buildWorkspaceMaturity', () => {
  it('marks a completely empty workspace as new', () => {
    const maturity = buildWorkspaceMaturity({
      checklist: checklist([
        { id: 'provider', label: 'Add provider access', detail: 'Save provider credentials first.', done: false, actionLabel: 'OPEN WORKSPACE', action: 'open_workspace' },
        { id: 'model', done: false },
      ]),
      attentionQueue: [],
      servingVerifiedCount: 0,
      servingUnverifiedCount: 0,
      pendingDeploymentCount: 0,
      activeInstanceCount: 0,
    });

    expect(maturity.state).toBe('new');
    expect(maturity.label).toBe('NEW WORKSPACE');
    expect(maturity.action).toBe('open_workspace');
  });

  it('prefers attention required when a critical issue exists', () => {
    const maturity = buildWorkspaceMaturity({
      checklist: checklist([
        { done: true },
        { done: true },
      ]),
      attentionQueue: [
        {
          severity: 'critical',
          title: 'No live provider connection',
          detail: 'Deployments will fail until provider access is restored.',
          actionLabel: 'OPEN WORKSPACE',
          action: 'open_workspace',
        },
      ],
      servingVerifiedCount: 1,
      servingUnverifiedCount: 0,
      pendingDeploymentCount: 0,
      activeInstanceCount: 1,
    });

    expect(maturity.state).toBe('attention_required');
    expect(maturity.label).toBe('ATTENTION REQUIRED');
    expect(maturity.actionLabel).toBe('OPEN WORKSPACE');
  });

  it('marks a configured but unverified workspace as serving unverified', () => {
    const maturity = buildWorkspaceMaturity({
      checklist: checklist([
        { done: true },
        { done: true },
        { done: true },
        { done: true },
        { done: true },
      ]),
      attentionQueue: [],
      servingVerifiedCount: 0,
      servingUnverifiedCount: 2,
      pendingDeploymentCount: 0,
      activeInstanceCount: 2,
    });

    expect(maturity.state).toBe('serving_unverified');
    expect(maturity.label).toBe('SERVING UNVERIFIED');
    expect(maturity.action).toBe('verify_now');
  });

  it('marks a verified workspace as serving verified', () => {
    const maturity = buildWorkspaceMaturity({
      checklist: checklist([
        { done: true },
        { done: true },
        { done: true },
        { done: true },
        { done: true },
      ]),
      attentionQueue: [],
      servingVerifiedCount: 1,
      servingUnverifiedCount: 0,
      pendingDeploymentCount: 0,
      activeInstanceCount: 1,
    });

    expect(maturity.state).toBe('serving_verified');
    expect(maturity.label).toBe('SERVING VERIFIED');
    expect(maturity.action).toBe('open_models');
  });
});
