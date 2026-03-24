/// <reference types="vitest/globals" />
import { beforeEach, describe, expect, it } from 'vitest';
import type { Worker } from '../types';
import { resetStableWorkerSnapshotCache, stabilizeWorkerSnapshot } from './stableWorkers';

const healthyWorker: Worker = {
  worker_id: 'worker-1',
  address: '10.0.0.1',
  status: 'healthy',
  models: ['org/model-a'],
  gpu_utilization: 12,
  memory_used: 10,
  memory_total: 24,
  queue_depth: 0,
  requests_per_sec: 1.2,
  avg_latency_ms: 110,
  p50_latency_ms: 90,
  p99_latency_ms: 180,
  error_rate: 0,
  last_heartbeat: '2026-03-18T08:00:00.000Z',
};

describe('stabilizeWorkerSnapshot', () => {
  beforeEach(() => {
    resetStableWorkerSnapshotCache();
  });

  it('retains a recently seen worker when a single poll misses it', () => {
    const first = stabilizeWorkerSnapshot([healthyWorker], 'ws_alpha', 1000);
    const second = stabilizeWorkerSnapshot([], 'ws_alpha', 12_000);

    expect(first).toHaveLength(1);
    expect(second).toHaveLength(1);
    expect(second[0].worker_id).toBe('worker-1');
  });

  it('drops a missing worker after the grace window expires', () => {
    stabilizeWorkerSnapshot([healthyWorker], 'ws_alpha', 1000);
    const afterGrace = stabilizeWorkerSnapshot([], 'ws_alpha', 25_500);

    expect(afterGrace).toHaveLength(0);
  });

  it('isolates worker snapshots per workspace scope', () => {
    stabilizeWorkerSnapshot([healthyWorker], 'ws_alpha', 1000);
    const otherScope = stabilizeWorkerSnapshot([], 'ws_beta', 12_000);

    expect(otherScope).toHaveLength(0);
  });
});
