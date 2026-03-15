import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { buildLiveWorkspaceOperations } from './liveWorkspaceOperations';
import type { DeploymentAttemptSummary } from './deploymentHistory';

function summary(partial: Partial<DeploymentAttemptSummary>): DeploymentAttemptSummary {
  return {
    attempt: {
      id: 'attempt_1',
      outcome: 'provisioned',
      created_at: '2026-03-16T10:00:00.000Z',
      updated_at: '2026-03-16T10:05:00.000Z',
      request: { gpu_type: 'A100_40GB', models: ['org/model-a'] },
      ...partial.attempt,
    } as DeploymentAttemptSummary['attempt'],
    instance: partial.instance ?? null,
    worker: partial.worker ?? null,
    readiness: partial.readiness || {
      label: 'SERVING VERIFIED',
      detail: 'Ready.',
      tone: '',
      serving: true,
      verified: true,
    },
  };
}

describe('buildLiveWorkspaceOperations', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('hides the panel for a new workspace', () => {
    const operations = buildLiveWorkspaceOperations({
      maturityState: 'new',
      modelServingStates: [],
      activeNodeCount: 0,
      deploymentSummaries: [],
      operationalAttentionQueue: [],
    });

    expect(operations.show).toBe(false);
  });

  it('shows a healthy live summary when verification is fresh', () => {
    vi.setSystemTime(new Date('2026-03-16T10:20:00.000Z'));

    const operations = buildLiveWorkspaceOperations({
      maturityState: 'serving_verified',
      modelServingStates: ['serving_verified', 'serving_unverified'],
      activeNodeCount: 2,
      deploymentSummaries: [
        summary({
          attempt: {
            inference_verification: {
              status: 'passed',
              verified_at: '2026-03-16T10:05:00.000Z',
              latency_ms: 120,
              model: 'org/model-a',
            },
          },
        }),
      ],
      operationalAttentionQueue: [],
    });

    expect(operations.show).toBe(true);
    expect(operations.headline).toContain('healthy');
    expect(operations.verificationLabel).toBe('FRESH VERIFICATION');
    expect(operations.activeServingModels).toBe(2);
  });

  it('surfaces the latest operator issue for live workspaces', () => {
    const operations = buildLiveWorkspaceOperations({
      maturityState: 'attention_required',
      modelServingStates: ['serving_verified', 'degraded'],
      activeNodeCount: 2,
      deploymentSummaries: [],
      operationalAttentionQueue: [
        {
          severity: 'critical',
          title: 'Workers are not connected',
          detail: 'No worker is currently reporting healthy runtime state.',
        },
      ],
    });

    expect(operations.operatorIssueTitle).toBe('Workers are not connected');
    expect(operations.headline).toContain('need attention');
    expect(operations.degradedRuntimeCount).toBe(1);
  });

  it('marks verification as stale when the last pass is old', () => {
    vi.setSystemTime(new Date('2026-03-16T18:30:00.000Z'));

    const operations = buildLiveWorkspaceOperations({
      maturityState: 'serving_unverified',
      modelServingStates: ['serving_unverified'],
      activeNodeCount: 1,
      deploymentSummaries: [
        summary({
          attempt: {
            inference_verification: {
              status: 'passed',
              verified_at: '2026-03-16T09:00:00.000Z',
              latency_ms: 145,
              model: 'org/model-a',
            },
          },
        }),
      ],
      operationalAttentionQueue: [],
    });

    expect(operations.verificationFreshness).toBe('stale');
    expect(operations.verificationLabel).toBe('STALE VERIFICATION');
  });
});
