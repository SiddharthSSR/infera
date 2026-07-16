/// <reference types="vitest/globals" />

import { describe, expect, it } from 'vitest';

import { deriveNodeIncident } from './instanceIncidents';
import type { DeploymentAttemptSummary } from './deploymentHistory';
import type { Instance, Worker } from '../types';

const baseInstance: Instance = {
  id: 'inst_1',
  provider_id: 'prov_1',
  provider: 'runpod',
  name: 'worker-node',
  status: 'running',
  gpu_type: 'RTX_4090',
  gpu_count: 1,
  cost_per_hour: 1.2,
  created_at: '2026-04-10T10:00:00Z',
  updated_at: '2026-04-10T10:00:00Z',
};

const healthyWorker: Worker = {
  worker_id: 'worker_1',
  instance_id: 'inst_1',
  status: 'healthy',
  models: ['moonshotai/Kimi-K2.5-Instruct'],
  last_heartbeat: '2026-04-10T10:05:00Z',
};

const baseSummary: DeploymentAttemptSummary = {
  attempt: {
    id: 'attempt_1',
    workspace_id: 'ws_1',
    created_at: '2026-04-10T10:00:00Z',
    updated_at: '2026-04-10T10:05:00Z',
    outcome: 'provisioned',
    request: {
      gpu_type: 'RTX_4090',
      models: ['moonshotai/Kimi-K2.5-Instruct'],
    },
  },
  readiness: {
    label: 'SERVING VERIFIED',
    detail: 'Node is serving.',
    tone: '',
    serving: true,
    verified: true,
  },
  instance: baseInstance,
  retryable: false,
  inferenceVerified: false,
  autoVerificationRequested: false,
};

describe('instanceIncidents', () => {
  it('prefers failed inference verification over generic readiness', () => {
    const incident = deriveNodeIncident(
      {
        ...baseInstance,
        worker_id: healthyWorker.worker_id,
        models: healthyWorker.models,
      },
      [healthyWorker],
      {
        ...baseSummary,
        attempt: {
          ...baseSummary.attempt,
          inference_verification: {
            status: 'failed',
            verified_at: '2026-04-10T10:06:00Z',
            model: 'moonshotai/Kimi-K2.5-Instruct',
            error: 'Gateway timeout',
          },
        },
      },
    );

    expect(incident).toMatchObject({
      title: 'INFERENCE CHECK FAILED',
      tone: 'error',
    });
    expect(incident?.detail).toContain('Latest verification failed on');
    expect(incident?.detail).toContain('Gateway timeout');
  });

  it('returns provider incident details for errored instances', () => {
    const incident = deriveNodeIncident(
      {
        ...baseInstance,
        status: 'error',
        error: 'Provider boot failure',
      },
      undefined,
      null,
    );

    expect(incident).toEqual({
      title: 'PROVIDER INCIDENT',
      detail: 'Provider boot failure',
      tone: 'error',
    });
  });
});
