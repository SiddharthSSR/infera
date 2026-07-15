/// <reference types="vitest/globals" />

import { describe, expect, it } from 'vitest';

import type { DeploymentAttemptSummary, DeploymentInferenceVerification } from './deploymentHistory';
import { formatInferenceVerificationCopy, getDeploymentAttemptTitle, getLatestDeploymentTitle } from './deploymentPresentation';

const baseSummary = {
  attempt: {
    id: 'attempt_1',
    workspace_id: 'ws_1',
    created_at: '2026-04-10T10:00:00Z',
    updated_at: '2026-04-10T10:10:00Z',
    outcome: 'provisioned',
    request: {
      gpu_type: 'RTX_4090',
      provider: 'runpod',
      gpu_count: 1,
      name: 'designer-node',
      models: ['moonshotai/Kimi-K2.5-Instruct'],
    },
    instance_id: 'inst_1234567890abcdef',
  },
  readiness: {
    label: 'SERVING VERIFIED',
    detail: 'Node is healthy.',
    tone: 'success',
    serving: true,
    verified: true,
  },
  instance: null,
  retryable: false,
  inferenceVerified: false,
  autoVerificationRequested: false,
} satisfies DeploymentAttemptSummary;

describe('deploymentPresentation', () => {
  it('formats successful inference verification messages with latency and preview', () => {
    const verification = {
      status: 'passed',
      verified_at: '2026-04-10T10:12:00Z',
      latency_ms: 842,
      response_preview: 'Inference is healthy.',
      model: 'moonshotai/Kimi-K2.5-Instruct',
    } satisfies DeploymentInferenceVerification;

    expect(
      formatInferenceVerificationCopy(
        verification,
        (value) => value,
        (latencyMs) => `${latencyMs}ms`,
      ),
    ).toBe('Verified on 2026-04-10T10:12:00Z in 842ms. Response: Inference is healthy.');
  });

  it('formats failed inference verification messages with the backend error', () => {
    const verification = {
      status: 'failed',
      verified_at: '2026-04-10T10:13:00Z',
      model: 'moonshotai/Kimi-K2.5-Instruct',
      error: 'Gateway timeout',
    } satisfies DeploymentInferenceVerification;

    expect(
      formatInferenceVerificationCopy(
        verification,
        (value) => value,
        () => null,
      ),
    ).toBe('Inference check failed on 2026-04-10T10:13:00Z: Gateway timeout');
  });

  it('uses the right title fallbacks for latest and history deployment views', () => {
    expect(getLatestDeploymentTitle(baseSummary)).toBe('designer-node');
    expect(getDeploymentAttemptTitle(baseSummary)).toBe('designer-node');

    const fallbackSummary = {
      ...baseSummary,
      attempt: {
        ...baseSummary.attempt,
        request: {
          ...baseSummary.attempt.request,
          name: '',
          models: ['org/frontier-model'],
        },
        instance_name: '',
      },
    } satisfies DeploymentAttemptSummary;

    expect(getLatestDeploymentTitle(fallbackSummary)).toBe('inst_1234567890a');
    expect(getDeploymentAttemptTitle(fallbackSummary)).toBe('frontier-model');
  });
});
