/// <reference types="vitest/globals" />
import { beforeEach, describe, expect, it } from 'vitest';
import type { Instance, Worker } from '../types';
import {
  getDeploymentRemediation,
  getDeploymentTimeline,
  markAutoVerificationRequested,
  recordInferenceVerification,
  readDeploymentAttempts,
  recordFailedAttempt,
  recordProvisionedAttempt,
  selectPrimaryDeploymentSummary,
  summarizeDeploymentAttempt,
} from './deploymentHistory';

const workspaceID = 'ws_test';

const baseInstance: Instance = {
  id: 'inst_1',
  provider_id: 'prov_1',
  provider: 'runpod',
  name: 'worker-1',
  status: 'running',
  gpu_type: 'A100_80GB',
  gpu_count: 1,
  vcpu: 16,
  memory_gb: 64,
  storage_gb: 100,
  cost_per_hour: 2.4,
  spot_instance: false,
  created_at: '2026-03-14T10:00:00.000Z',
  worker_id: 'worker-1',
  models: ['org/model-a'],
};

const healthyWorker: Worker = {
  worker_id: 'worker-1',
  address: '10.0.0.1',
  status: 'healthy',
  models: ['org/model-a'],
  gpu_utilization: 0.2,
  memory_used: 4,
  memory_total: 8,
  queue_depth: 0,
  requests_per_sec: 0.1,
  avg_latency_ms: 100,
  p50_latency_ms: 90,
  p99_latency_ms: 180,
  error_rate: 0,
  last_heartbeat: '2026-03-14T10:11:00.000Z',
};

describe('deploymentHistory', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('records and reloads provisioned attempts for a workspace', () => {
    recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );

    const attempts = readDeploymentAttempts(workspaceID);
    expect(attempts).toHaveLength(1);
    expect(attempts[0]).toEqual(expect.objectContaining({
      outcome: 'provisioned',
      instance_id: 'inst_1',
      selected_model_name: 'Model A',
    }));
  });

  it('keeps workspaces isolated in storage', () => {
    recordFailedAttempt(workspaceID, { gpu_type: 'RTX_4090' }, 'bad credentials');
    recordFailedAttempt('ws_other', { gpu_type: 'H100' }, 'capacity unavailable');

    expect(readDeploymentAttempts(workspaceID)).toHaveLength(1);
    expect(readDeploymentAttempts('ws_other')).toHaveLength(1);
    expect(readDeploymentAttempts(workspaceID)[0].failure_reason).toBe('bad credentials');
  });

  it('summarizes request failures as retryable deployment history entries', () => {
    const [attempt] = recordFailedAttempt(workspaceID, { provider: 'runpod', gpu_type: 'RTX_4090' }, 'auth failed');
    const summary = summarizeDeploymentAttempt(attempt, [], []);

    expect(summary.retryable).toBe(true);
    expect(summary.readiness.label).toBe('REQUEST FAILED');
    expect(summary.readiness.detail).toContain('auth failed');
    expect(summary.instance).toBeNull();
  });

  it('reconciles provisioned attempts against live instances and workers', () => {
    const [attempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );

    const summary = summarizeDeploymentAttempt(
      attempt,
      [baseInstance],
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(summary.readiness.label).toBe('SERVING VERIFIED');
    expect(summary.instance?.id).toBe('inst_1');
    expect(summary.retryable).toBe(false);
  });

  it('builds a serving timeline for verified deployments', () => {
    const [attempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );

    const summary = summarizeDeploymentAttempt(
      attempt,
      [baseInstance],
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    const timeline = getDeploymentTimeline(summary);
    expect(timeline.map((step) => step.state)).toEqual(['done', 'done', 'done', 'done', 'done', 'done', 'active']);
    expect(getDeploymentRemediation(summary)?.action).toBe('verify_inference');
  });

  it('keeps the node-running timeline step active while an instance is starting', () => {
    const [attempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );
    const summary = summarizeDeploymentAttempt(attempt, [{ ...baseInstance, status: 'starting' }], []);

    expect(getDeploymentTimeline(summary)[2]).toEqual({ label: 'Node running', state: 'active' });
  });

  it('routes auth-like request failures to workspace remediation', () => {
    const [attempt] = recordFailedAttempt(
      workspaceID,
      { provider: 'runpod', gpu_type: 'RTX_4090' },
      'provider auth failed',
    );
    const summary = summarizeDeploymentAttempt(attempt, [], []);
    const remediation = getDeploymentRemediation(summary);

    expect(remediation?.action).toBe('open_workspace');
    expect(remediation?.label).toBe('FIX PROVIDER SETUP');
    expect(getDeploymentTimeline(summary)[1].state).toBe('failed');
  });

  it('marks inference verification as complete after a successful live check', () => {
    const [attempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );

    const [updatedAttempt] = recordInferenceVerification(workspaceID, attempt.id, {
      status: 'passed',
      verified_at: '2026-03-14T10:13:00.000Z',
      latency_ms: 420,
      model: 'org/model-a',
      response_preview: 'READY',
    });

    const summary = summarizeDeploymentAttempt(
      updatedAttempt,
      [baseInstance],
      [healthyWorker],
      new Date('2026-03-14T10:12:20.000Z'),
    );

    expect(summary.inferenceVerified).toBe(true);
    expect(getDeploymentTimeline(summary)[6].state).toBe('done');
    expect(getDeploymentRemediation(summary)).toBeNull();
  });

  it('marks auto verification as requested and changes remediation copy', () => {
    const [attempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );

    const [updatedAttempt] = markAutoVerificationRequested(workspaceID, attempt.id, '2026-03-14T10:12:10.000Z');
    const summary = summarizeDeploymentAttempt(
      updatedAttempt,
      [baseInstance],
      [healthyWorker],
      new Date('2026-03-14T10:12:20.000Z'),
    );

    expect(summary.autoVerificationRequested).toBe(true);
    expect(getDeploymentRemediation(summary)?.label).toBe('VERIFY NOW');
  });

  it('prefers a live serving deployment over a newer failed request for the primary summary', () => {
    const [servingAttempt] = recordProvisionedAttempt(
      workspaceID,
      { name: 'worker-1', provider: 'runpod', gpu_type: 'A100_80GB', gpu_count: 1, models: ['org/model-a'] },
      baseInstance,
      'Model A',
    );
    const [failedAttempt] = recordFailedAttempt(
      workspaceID,
      { provider: 'runpod', gpu_type: 'H100', gpu_count: 8 },
      'unsupported GPU type',
    );

    const servingSummary = summarizeDeploymentAttempt(
      {
        ...servingAttempt,
        updated_at: '2026-03-18T08:00:00.000Z',
        inference_verification: {
          status: 'passed',
          verified_at: '2026-03-18T08:00:00.000Z',
          latency_ms: 210,
          model: 'org/model-a',
        },
      },
      [baseInstance],
      [{ ...healthyWorker, last_heartbeat: '2026-03-18T08:00:30.000Z' }],
      new Date('2026-03-18T08:01:00.000Z'),
    );
    const failedSummary = summarizeDeploymentAttempt(
      {
        ...failedAttempt,
        updated_at: '2026-03-18T08:05:00.000Z',
      },
      [],
      [],
      new Date('2026-03-18T08:06:00.000Z'),
    );

    const primary = selectPrimaryDeploymentSummary([failedSummary, servingSummary]);

    expect(primary?.attempt.id).toBe(servingSummary.attempt.id);
    expect(primary?.readiness.label).toBe('SERVING VERIFIED');
  });
});
