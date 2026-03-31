/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { GPUOffering, Instance, ProviderStatus, VaultModel } from '../types';
import { ProvisionModal } from './Instances';

vi.mock('../hooks/useApi', () => ({
  useDeploymentAttempts: vi.fn(),
  useInstances: vi.fn(),
  useMarkDeploymentAutoVerificationRequested: vi.fn(),
  useOfferings: vi.fn(),
  useProviders: vi.fn(),
  useTerminateInstance: vi.fn(),
  useStartInstance: vi.fn(),
  useStopInstance: vi.fn(),
  useProvisionInstance: vi.fn(),
  useUpdateDeploymentVerification: vi.fn(),
  useVaultModels: vi.fn(),
  useWorkers: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { useProvisionInstance, useVaultModels } from '../hooks/useApi';

const mockUseProvisionInstance = vi.mocked(useProvisionInstance);
const mockUseVaultModels = vi.mocked(useVaultModels);

const sampleOfferings: GPUOffering[] = [
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
    provider: 'runpod',
    gpu_type: 'RTX_4090',
    display_name: 'NVIDIA RTX 4090',
    provider_gpu_type_id: '4090',
    gpu_count: 2,
    vcpu: 32,
    memory_gb: 48,
    storage_gb: 200,
    cost_per_hour: 2.3,
    region: 'us-ca',
    available: 1,
  },
];

const sampleProviderStatuses: ProviderStatus[] = [
  {
    provider: 'runpod',
    connected: true,
    active_instances: 0,
  },
];

const sampleModels: VaultModel[] = [
  {
    id: 'model-qwen',
    name: 'Qwen3 4B Thinking 2507',
    source: 'infera',
    source_uri: 'Qwen/Qwen3-4B-Thinking-2507',
    parameters: '4B',
    quantization: 'none',
    vram_required: 12 * 1024,
    max_context: 262144,
    family: 'qwen',
    tags: [],
    metadata: {},
    status: 'available',
    created_at: '2026-03-01T00:00:00Z',
    updated_at: '2026-03-01T00:00:00Z',
  },
  {
    id: 'model-big',
    name: 'Frontier 70B',
    source: 'infera',
    source_uri: 'infera/frontier-70b',
    parameters: '70B',
    quantization: 'none',
    vram_required: 60 * 1024,
    max_context: 32768,
    family: 'frontier',
    tags: [],
    metadata: {},
    status: 'available',
    created_at: '2026-03-01T00:00:00Z',
    updated_at: '2026-03-01T00:00:00Z',
  },
];

describe('ProvisionModal', () => {
  const mutateAsync = vi.fn<() => Promise<Instance>>();
  const onProvisioned = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mutateAsync.mockResolvedValue({
      id: 'inst_1',
      provider_id: 'prov_1',
      provider: 'runpod',
      name: 'designer-node',
      status: 'pending',
      gpu_type: 'RTX_4090',
      gpu_count: 1,
      vcpu: 16,
      memory_gb: 24,
      storage_gb: 200,
      cost_per_hour: 1.2,
      spot_instance: false,
      created_at: '2026-03-01T00:00:00Z',
    });
    mockUseProvisionInstance.mockReturnValue({
      mutateAsync,
      isPending: false,
    } as ReturnType<typeof useProvisionInstance>);
    mockUseVaultModels.mockReturnValue({
      data: { models: sampleModels },
    } as ReturnType<typeof useVaultModels>);
  });

  it('guides the user from compute to models to review before provisioning', async () => {
    render(
      <ProvisionModal
        isOpen
        onClose={vi.fn()}
        onProvisioned={onProvisioned}
        onProvisionFailed={vi.fn()}
        onOpenWorkspace={vi.fn()}
        offerings={sampleOfferings}
        providerStatuses={sampleProviderStatuses}
        configuredProviders={['runpod']}
      />,
    );

    expect(screen.getByText('Choose the GPU family and node size')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Continue to models/i })).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: /1x GPU/i }));
    expect(screen.getByRole('button', { name: /Continue to models/i })).toBeEnabled();

    fireEvent.click(screen.getByRole('button', { name: /Continue to models/i }));
    expect(screen.getByText('Pick models that fit the selected GPU')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Qwen3 4B Thinking 2507/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Frontier 70B/i })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Qwen3 4B Thinking 2507/i }));
    fireEvent.click(screen.getByRole('button', { name: /Continue to review/i }));

    expect(screen.getByText('Review deployment details')).toBeInTheDocument();

    const input = screen.getByPlaceholderText('infera-worker');
    fireEvent.change(input, { target: { value: 'designer-node' } });
    fireEvent.click(screen.getByRole('button', { name: /Provision node/i }));

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({
        name: 'designer-node',
        provider: 'runpod',
        gpu_type: 'RTX_4090',
        gpu_count: 1,
        models: ['Qwen/Qwen3-4B-Thinking-2507'],
        selected_model_name: 'Qwen3 4B Thinking 2507',
      }));
    });
    expect(onProvisioned).toHaveBeenCalled();
  });

  it('shows live local inventory even when no workspace provider config exists', () => {
    render(
      <ProvisionModal
        isOpen
        onClose={vi.fn()}
        onProvisioned={onProvisioned}
        onProvisionFailed={vi.fn()}
        onOpenWorkspace={vi.fn()}
        offerings={[
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
        ]}
        providerStatuses={[
          {
            provider: 'mock',
            connected: true,
            active_instances: 0,
          },
        ]}
        configuredProviders={[]}
      />,
    );

    expect(screen.queryByText('No live inventory is connected yet')).not.toBeInTheDocument();
    expect(screen.getByText('LIVE SOURCES')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /1x GPU · local-lab/i })).toBeInTheDocument();
  });
});
