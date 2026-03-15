import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { deriveModelRuntimeDrilldown } from './modelRuntimeDrilldown';
import type { DeploymentAttemptRecord } from './deploymentHistory';
import type { Instance, Worker } from '../types';

describe('deriveModelRuntimeDrilldown', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('marks a recent successful verification as fresh', () => {
    vi.setSystemTime(new Date('2026-03-16T12:00:00.000Z'));

    const instances: Instance[] = [
      {
        id: 'i1',
        provider_id: 'p1',
        provider: 'runpod',
        name: 'node-a',
        status: 'running',
        gpu_type: 'A100_40GB',
        gpu_count: 1,
        vcpu: 8,
        memory_gb: 32,
        storage_gb: 100,
        worker_id: 'w1',
        models: ['org/model-a'],
        cost_per_hour: 1,
        spot_instance: false,
        created_at: '2026-03-16T11:30:00.000Z',
      },
    ];
    const workers: Worker[] = [
      {
        worker_id: 'w1',
        status: 'healthy',
        gpu_utilization: 50,
        memory_used: 4,
        memory_total: 8,
        models: ['org/model-a'],
        last_heartbeat: '2026-03-16T11:59:30.000Z',
      },
    ];
    const attempts: DeploymentAttemptRecord[] = [
      {
        id: 'a1',
        created_at: '2026-03-16T11:40:00.000Z',
        updated_at: '2026-03-16T11:50:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', gpu_count: 1, models: ['org/model-a'] },
        instance_id: 'i1',
        inference_verification: {
          status: 'passed',
          verified_at: '2026-03-16T11:50:00.000Z',
          latency_ms: 180,
          model: 'org/model-a',
        },
      },
    ];

    const drilldown = deriveModelRuntimeDrilldown('org/model-a', instances, workers, attempts);

    expect(drilldown.activeNodes).toBe(1);
    expect(drilldown.verificationFreshness).toBe('fresh');
    expect(drilldown.verificationLabel).toBe('FRESH VERIFY');
    expect(drilldown.degradedNodes).toBe(0);
  });

  it('surfaces failed verification as a degraded node signal', () => {
    vi.setSystemTime(new Date('2026-03-16T12:00:00.000Z'));

    const attempts: DeploymentAttemptRecord[] = [
      {
        id: 'a1',
        created_at: '2026-03-16T11:40:00.000Z',
        updated_at: '2026-03-16T11:55:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', gpu_count: 1, models: ['org/model-a'] },
        inference_verification: {
          status: 'failed',
          verified_at: '2026-03-16T11:55:00.000Z',
          model: 'org/model-a',
          error: 'connection reset',
        },
      },
    ];

    const drilldown = deriveModelRuntimeDrilldown('org/model-a', [], [], attempts);

    expect(drilldown.degradedNodes).toBe(1);
    expect(drilldown.latestIssue).toBe('connection reset');
  });

  it('marks older successful verification as stale', () => {
    vi.setSystemTime(new Date('2026-03-16T18:30:00.000Z'));

    const attempts: DeploymentAttemptRecord[] = [
      {
        id: 'a1',
        created_at: '2026-03-16T09:00:00.000Z',
        updated_at: '2026-03-16T09:15:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', gpu_count: 1, models: ['org/model-a'] },
        inference_verification: {
          status: 'passed',
          verified_at: '2026-03-16T09:15:00.000Z',
          model: 'org/model-a',
        },
      },
    ];

    const drilldown = deriveModelRuntimeDrilldown('org/model-a', [], [], attempts);

    expect(drilldown.verificationFreshness).toBe('stale');
    expect(drilldown.verificationLabel).toBe('STALE VERIFY');
  });
});
