/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Models } from './Models';

const mocks = vi.hoisted(() => ({
  models: { data: [], isLoading: false } as any,
  vaultModels: { data: { models: [] }, isLoading: false } as any,
  offerings: { data: [], isLoading: false } as any,
  providers: { data: [], isLoading: false } as any,
  instances: { data: [], isLoading: false } as any,
  workers: { data: [], isLoading: false } as any,
  deploymentAttempts: { data: [], isLoading: false } as any,
}));

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => true,
}));

vi.mock('../hooks/useRuntimeApi', () => ({
  useModels: () => mocks.models,
  useWorkers: () => mocks.workers,
}));

vi.mock('../hooks/useInfrastructureApi', () => ({
  useOfferings: () => mocks.offerings,
  useProviders: () => mocks.providers,
  useInstances: () => mocks.instances,
}));

vi.mock('../hooks/useDeploymentApi', () => ({
  useDeploymentAttempts: () => mocks.deploymentAttempts,
  useUpdateDeploymentVerification: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));

vi.mock('../hooks/useVaultApi', () => ({
  useVaultModels: () => mocks.vaultModels,
  useRegisterVaultModel: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useDeleteVaultModel: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));

vi.mock('../lib/auth-context', () => ({
  useAuthSession: () => ({ session: { workspace: { id: 'ws_test' } } }),
}));

describe('Models mobile layout', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.models = { data: [], isLoading: false };
    mocks.vaultModels = { data: { models: [] }, isLoading: false };
    mocks.offerings = { data: [], isLoading: false };
    mocks.providers = { data: [], isLoading: false };
    mocks.instances = { data: [], isLoading: false };
    mocks.workers = { data: [], isLoading: false };
    mocks.deploymentAttempts = { data: [], isLoading: false };
  });

  it('renders mobile cards on Models page', () => {
    mocks.models = {
      data: [
        {
          id: 'org/model-a',
          loaded: false,
          vault_status: 'available',
          family: 'llama',
          parameters: '8B',
          quantization: 'AWQ',
          max_context: 8192,
          owned_by: 'org',
        },
      ],
      isLoading: false,
    };
    mocks.vaultModels = {
      data: { models: [{ id: 'vault_1', source_uri: 'org/model-a' }] },
      isLoading: false,
    };
    mocks.offerings = {
      data: [{ provider: 'runpod', gpu_type: 'RTX_4090', cost_per_hour: 0.4 }],
      isLoading: false,
    };
    mocks.providers = {
      data: [{ provider: 'runpod', connected: true }],
      isLoading: false,
    };

    const { container } = render(
      <MemoryRouter>
        <Models />
      </MemoryRouter>,
    );

    expect(screen.getByText('model-a')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'DEPLOY' })).toBeInTheDocument();
    expect(screen.getByText(/Ready on 1 GPU config/i)).toBeInTheDocument();
    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    expect(screen.queryByText('MODEL NAME & VERSION')).not.toBeInTheDocument();
  });

  it('renders degraded runtime drilldown actions for deployed models', () => {
    mocks.deploymentAttempts = { data: [
      {
        id: 'attempt_failed',
        created_at: '2026-03-16T11:40:00.000Z',
        updated_at: '2026-03-16T11:55:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', gpu_count: 1, models: ['org/model-a'] },
        instance_id: 'instance_1',
        inference_verification: {
          status: 'failed',
          verified_at: '2026-03-16T11:55:00.000Z',
          model: 'org/model-a',
          error: 'connection reset',
        },
      },
    ], isLoading: false };

    mocks.models = {
      data: [
        {
          id: 'org/model-a',
          loaded: false,
          vault_status: 'available',
          family: 'llama',
          parameters: '8B',
          quantization: 'AWQ',
          max_context: 8192,
          owned_by: 'org',
        },
      ],
      isLoading: false,
    };
    mocks.vaultModels = {
      data: { models: [{ id: 'vault_1', source_uri: 'org/model-a' }] },
      isLoading: false,
    };
    mocks.instances = {
      data: [
        {
          id: 'instance_1',
          provider_id: 'provider_1',
          provider: 'runpod',
          name: 'node-a',
          status: 'error',
          gpu_type: 'A100_40GB',
          gpu_count: 1,
          vcpu: 16,
          memory_gb: 64,
          storage_gb: 100,
          models: ['org/model-a'],
          cost_per_hour: 1.2,
          spot_instance: false,
          created_at: '2026-03-16T11:30:00.000Z',
        },
      ],
      isLoading: false,
    };
    mocks.providers = {
      data: [{ provider: 'runpod', connected: true }],
      isLoading: false,
    };

    render(
      <MemoryRouter>
        <Models />
      </MemoryRouter>,
    );

    expect(screen.getAllByText('DEGRADED').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: /show details/i }));
    expect(screen.getByText('connection reset')).toBeInTheDocument();
    expect(screen.getByText('OPEN DEGRADED NODES')).toBeInTheDocument();
    expect(screen.getByText('VIEW DEPLOYMENTS')).toBeInTheDocument();
  });
});
