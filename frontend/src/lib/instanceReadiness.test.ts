/// <reference types="vitest/globals" />
import { describe, it, expect } from 'vitest';
import { getInstanceReadiness } from './instanceReadiness';
import type { Instance, Worker } from '../types';

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

describe('getInstanceReadiness', () => {
  it('keeps a starting instance non-serving until durable startup finalization', () => {
    const readiness = getInstanceReadiness(
      { ...baseInstance, status: 'starting' },
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('STARTING');
    expect(readiness.tone).toBe('warning');
    expect(readiness.serving).toBe(false);
    expect(readiness.verified).toBe(false);
  });

  it('marks serving as verified when assigned model is loaded on a fresh healthy worker', () => {
    const readiness = getInstanceReadiness(
      baseInstance,
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('SERVING VERIFIED');
    expect(readiness.verified).toBe(true);
    expect(readiness.serving).toBe(true);
  });

  it('marks worker-not-connected as an explicit failure when running too long without a worker', () => {
    const readiness = getInstanceReadiness(
      { ...baseInstance, worker_id: undefined },
      [],
      new Date('2026-03-14T10:08:00.000Z'),
    );

    expect(readiness.label).toBe('WORKER NOT CONNECTED');
    expect(readiness.tone).toBe('error');
    expect(readiness.verified).toBe(false);
  });

  it('uses provider-running-no-network lifecycle state from the instance API', () => {
    const readiness = getInstanceReadiness(
      {
        ...baseInstance,
        worker_id: undefined,
        worker_registration_status: 'provider_running_no_network',
        provider_network_error: 'Provider reports instance running, but no public/proxy endpoint is available yet.',
      },
      [],
      new Date('2026-03-14T10:08:00.000Z'),
    );

    expect(readiness.label).toBe('NO NETWORK');
    expect(readiness.tone).toBe('error');
    expect(readiness.detail).toContain('no public/proxy endpoint');
    expect(readiness.serving).toBe(false);
  });

  it('uses provider-running-worker-unregistered lifecycle state from the instance API', () => {
    const readiness = getInstanceReadiness(
      {
        ...baseInstance,
        worker_id: undefined,
        worker_registration_status: 'provider_running_worker_unregistered',
        last_worker_registration_error: 'Provider reports instance running, but no gateway worker registered before the deadline.',
      },
      [],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('WORKER NOT REGISTERED');
    expect(readiness.tone).toBe('error');
    expect(readiness.detail).toContain('no gateway worker registered');
    expect(readiness.verified).toBe(false);
  });

  it('shows an unregistered worker as pending before the registration deadline', () => {
    const readiness = getInstanceReadiness(
      {
        ...baseInstance,
        worker_id: undefined,
        worker_registration_status: 'pending',
        provider_network_ready: true,
      },
      [],
      new Date('2026-03-14T10:02:00.000Z'),
    );

    expect(readiness.label).toBe('WAITING FOR WORKER');
    expect(readiness.tone).toBe('warning');
    expect(readiness.serving).toBe(false);
  });

  it('uses registered-unhealthy lifecycle state even when worker data looks healthy', () => {
    const readiness = getInstanceReadiness(
      {
        ...baseInstance,
        worker_registration_status: 'registered_unhealthy',
        last_worker_registration_error: 'Gateway registry reports the linked worker as unhealthy',
      },
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('WORKER UNHEALTHY');
    expect(readiness.tone).toBe('warning');
    expect(readiness.detail).toContain('registry reports');
    expect(readiness.verified).toBe(false);
  });

  it('maps lifecycle ready to serving verified when model and heartbeat are current', () => {
    const readiness = getInstanceReadiness(
      { ...baseInstance, worker_registration_status: 'ready' },
      [healthyWorker],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('SERVING VERIFIED');
    expect(readiness.tone).toBe('');
    expect(readiness.serving).toBe(true);
    expect(readiness.verified).toBe(true);
  });

  it('marks serving as unverified when the worker heartbeat is stale', () => {
    const readiness = getInstanceReadiness(
      baseInstance,
      [{ ...healthyWorker, last_heartbeat: '2026-03-14T10:07:00.000Z' }],
      new Date('2026-03-14T10:12:00.000Z'),
    );

    expect(readiness.label).toBe('SERVING UNVERIFIED');
    expect(readiness.verified).toBe(false);
    expect(readiness.serving).toBe(true);
  });

  it('marks model load as delayed when nothing is loaded after a long wait', () => {
    const readiness = getInstanceReadiness(
      baseInstance,
      [{ ...healthyWorker, models: [], last_heartbeat: '2026-03-14T10:14:00.000Z' }],
      new Date('2026-03-14T10:15:00.000Z'),
    );

    expect(readiness.label).toBe('MODEL LOAD DELAY');
    expect(readiness.tone).toBe('warning');
    expect(readiness.serving).toBe(false);
  });
});
