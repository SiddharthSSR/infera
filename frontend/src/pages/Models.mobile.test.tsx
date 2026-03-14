/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
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
}));

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => true,
}));

vi.mock('../hooks/useApi', () => ({
  useModels: () => mocks.models,
  useVaultModels: () => mocks.vaultModels,
  useOfferings: () => mocks.offerings,
  useProviders: () => mocks.providers,
  useInstances: () => mocks.instances,
  useWorkers: () => mocks.workers,
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
});
