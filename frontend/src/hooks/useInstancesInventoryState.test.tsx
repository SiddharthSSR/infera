/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GPUOffering, Instance, ProviderStatus, WorkspaceProviderConfigRecord } from '../types';
import { useInstancesInventoryState } from './useInstancesInventoryState';

vi.mock('../lib/workspaceAdminClient', () => ({
  fetchWorkspaceProviderConfigs: vi.fn(),
}));

import { fetchWorkspaceProviderConfigs } from '../lib/workspaceAdminClient';

const mockFetchWorkspaceProviderConfigs = vi.mocked(fetchWorkspaceProviderConfigs);

const providers: ProviderStatus[] = [
  { provider: 'runpod', connected: true, active_instances: 2 },
  { provider: 'mock', connected: true, active_instances: 1 },
  { provider: 'lambda', connected: true, active_instances: 1 },
];

const offerings: GPUOffering[] = [
  {
    provider: 'runpod',
    gpu_type: 'RTX_4090',
    display_name: 'NVIDIA RTX 4090',
    provider_gpu_type_id: '4090',
    gpu_count: 1,
    vcpu: 16,
    memory_gb: 24,
    storage_gb: 200,
    cost_per_hour: 1.2,
    region: 'us-ca',
    available: 2,
  },
  {
    provider: 'mock',
    gpu_type: 'A100_80GB',
    display_name: 'NVIDIA A100 80GB',
    provider_gpu_type_id: 'a100-80-local',
    gpu_count: 1,
    vcpu: 24,
    memory_gb: 80,
    storage_gb: 500,
    cost_per_hour: 0.9,
    region: 'local-lab',
    available: 4,
  },
  {
    provider: 'lambda',
    gpu_type: 'A100_80GB',
    display_name: 'NVIDIA A100 80GB',
    provider_gpu_type_id: 'a100-80-lambda',
    gpu_count: 1,
    vcpu: 24,
    memory_gb: 80,
    storage_gb: 500,
    cost_per_hour: 2.9,
    region: 'us-west',
    available: 4,
  },
];

const filteredInstances: Instance[] = [
  {
    id: 'inst_1',
    provider_id: 'prov_1',
    provider: 'runpod',
    name: 'worker-1',
    status: 'running',
    gpu_type: 'RTX_4090',
    gpu_count: 1,
    vcpu: 16,
    memory_gb: 24,
    storage_gb: 200,
    cost_per_hour: 1.2,
    created_at: '2026-04-01T00:00:00Z',
    models: ['Qwen/Qwen3-4B-Instruct'],
  },
];

describe('useInstancesInventoryState', () => {
  beforeEach(() => {
    vi.clearAllMocks();
});

  it('uses visible inventory providers directly for non-admin roles', async () => {
    const { result } = renderHook(() => useInstancesInventoryState({
      role: 'user',
      workspaceID: 'ws_1',
      providers,
      offerings,
      filteredInstances: [],
    }));

    await waitFor(() => {
      expect(result.current.configuredProviders).toEqual(['runpod', 'mock']);
    });

    expect(result.current.visibleProviderStatuses.map((status) => status.provider)).toEqual(['runpod', 'mock']);
    expect(result.current.visibleOfferings.map((offering) => offering.provider)).toEqual(['runpod', 'mock']);
    expect(result.current.connectedProviders.map((status) => status.provider)).toEqual(['runpod', 'mock']);
    expect(result.current.providerSummary).toEqual(['runpod', 'mock']);
    expect(result.current.providerRail).toEqual(['runpod', 'vastai', 'mock']);
    expect(mockFetchWorkspaceProviderConfigs).not.toHaveBeenCalled();
  });

  it('fetches configured workspace providers for admin roles', async () => {
    mockFetchWorkspaceProviderConfigs.mockResolvedValue([
      { provider: 'runpod', configured: true },
      { provider: 'vastai', configured: false },
    ] as WorkspaceProviderConfigRecord[]);

    const { result } = renderHook(() => useInstancesInventoryState({
      role: 'admin',
      workspaceID: 'ws_1',
      providers,
      offerings,
      filteredInstances,
    }));

    await waitFor(() => {
      expect(result.current.configuredProviders).toEqual(['runpod']);
    });

    expect(mockFetchWorkspaceProviderConfigs).toHaveBeenCalledWith('ws_1');
    expect(result.current.providerSummary).toEqual(['runpod']);
  });

  it('falls back to visible providers when admin config fetch fails', async () => {
    mockFetchWorkspaceProviderConfigs.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => useInstancesInventoryState({
      role: 'owner',
      workspaceID: 'ws_1',
      providers,
      offerings,
      filteredInstances: [],
    }));

    await waitFor(() => {
      expect(result.current.configuredProviders).toEqual(['runpod', 'mock']);
    });

    expect(result.current.providerSummary).toEqual(['runpod', 'mock']);
  });
});
