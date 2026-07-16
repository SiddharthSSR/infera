/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { Instance, Worker } from '../types';
import { useInstancesViewState } from './useInstancesViewState';

const healthyInstance: Instance = {
  id: 'inst_healthy',
  provider_id: 'prov_healthy',
  provider: 'runpod',
  name: 'healthy-node',
  status: 'running',
  worker_id: 'worker-1',
  gpu_type: 'RTX_4090',
  gpu_count: 1,
  vcpu: 16,
  memory_gb: 24,
  storage_gb: 200,
  cost_per_hour: 1.2,
  created_at: '2026-04-01T00:00:00Z',
  models: ['Qwen/Qwen3-4B-Instruct'],
};

const degradedInstance: Instance = {
  id: 'inst_degraded',
  provider_id: 'prov_degraded',
  provider: 'mock',
  name: 'degraded-node',
  status: 'running',
  gpu_type: 'A100_80GB',
  gpu_count: 1,
  vcpu: 24,
  memory_gb: 80,
  storage_gb: 500,
  cost_per_hour: 0.9,
  created_at: '2026-04-01T00:00:00Z',
  models: ['meta-llama/Llama-3.1-8B-Instruct'],
};

const terminatedInstance: Instance = {
  id: 'inst_terminated',
  provider_id: 'prov_terminated',
  provider: 'runpod',
  name: 'terminated-node',
  status: 'terminated',
  gpu_type: 'RTX_4090',
  gpu_count: 1,
  vcpu: 16,
  memory_gb: 24,
  storage_gb: 200,
  cost_per_hour: 0.7,
  created_at: '2026-04-01T00:00:00Z',
};

const workers: Worker[] = [
  {
    worker_id: 'worker-1',
    address: 'http://worker-1',
    status: 'healthy',
    models: ['Qwen/Qwen3-4B-Instruct'],
    gpu_utilization: 42,
    memory_used: 200,
    memory_total: 1000,
    queue_depth: 0,
    requests_per_sec: 0,
    avg_latency_ms: 12,
    p50_latency_ms: 10,
    p99_latency_ms: 20,
    error_rate: 0,
    last_heartbeat: '2100-04-01T00:00:00Z',
  },
];

describe('useInstancesViewState', () => {
  it('parses model drilldown params and filters active instances to that model', () => {
    const { result } = renderHook(() => useInstancesViewState({
      searchParams: new URLSearchParams('model=Qwen%2FQwen3-4B-Instruct'),
      instances: [healthyInstance, degradedInstance, terminatedInstance],
      workers,
    }));

    expect(result.current.drilldownModel).toBe('Qwen/Qwen3-4B-Instruct');
    expect(result.current.drilldownModelLabel).toBe('Qwen3-4B-Instruct');
    expect(result.current.filteredInstances.map((instance) => instance.id)).toEqual(['inst_healthy']);
  });

  it('keeps active as the default status filter and updates derived metrics when changed', () => {
    const { result } = renderHook(() => useInstancesViewState({
      searchParams: new URLSearchParams(),
      instances: [healthyInstance, degradedInstance, terminatedInstance],
      workers,
    }));

    expect(result.current.statusFilter).toBe('active');
    expect(result.current.filteredInstances.map((instance) => instance.id)).toEqual(['inst_healthy', 'inst_degraded']);
    expect(result.current.totalInstanceCount).toBe(3);
    expect(result.current.healthyWorkers).toEqual(workers);
    expect(result.current.totalGpuUtil).toBe(42);
    expect(result.current.totalMemUsed).toBe(200);
    expect(result.current.totalMemTotal).toBe(1000);
    expect(result.current.runningCount).toBe(2);
    expect(result.current.totalCostPerHour).toBeCloseTo(2.1, 5);

    act(() => {
      result.current.setStatusFilter('all');
    });

    expect(result.current.filteredInstances.map((instance) => instance.id)).toEqual([
      'inst_healthy',
      'inst_degraded',
      'inst_terminated',
    ]);
    expect(result.current.totalCostPerHour).toBeCloseTo(2.8, 5);
  });

  it('filters to degraded instances when the drilldown focus requests it', () => {
    const { result } = renderHook(() => useInstancesViewState({
      searchParams: new URLSearchParams('focus=degraded'),
      instances: [healthyInstance, degradedInstance],
      workers,
    }));

    expect(result.current.drilldownFocus).toBe('degraded');
    expect(result.current.filteredInstances.map((instance) => instance.id)).toEqual(['inst_degraded']);
  });
});
