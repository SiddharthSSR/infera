/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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
  useIsMobile: () => false,
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

describe('Models desktop layout', () => {
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

  it('keeps primary row actions visible without a horizontal scroll table', () => {
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

    expect(screen.getByRole('button', { name: 'DEPLOY' })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'OPEN NODES' }).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'REMOVE' })).toBeInTheDocument();
    expect(container.querySelector('.responsive-scroll-x')).not.toBeInTheDocument();
  });
});
