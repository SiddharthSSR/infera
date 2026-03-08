/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Instances } from './Instances';
import { Models } from './Models';
import { ApiKeys } from './ApiKeys';
import { Logs } from './Logs';

const mocks = vi.hoisted(() => ({
  instances: { data: [], isLoading: false } as any,
  offerings: { data: [], isLoading: false } as any,
  workers: { data: [], isLoading: false } as any,
  models: { data: [], isLoading: false } as any,
  vaultModels: { data: { models: [] }, isLoading: false } as any,
}));

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => true,
}));

vi.mock('../hooks/useApi', () => ({
  useInstances: () => mocks.instances,
  useOfferings: () => mocks.offerings,
  useWorkers: () => mocks.workers,
  useModels: () => mocks.models,
  useVaultModels: () => mocks.vaultModels,
  useTerminateInstance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useStartInstance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useStopInstance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useProvisionInstance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useRegisterVaultModel: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useDeleteVaultModel: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));

const apiMocks = vi.hoisted(() => ({
  fetchApiKeys: vi.fn(),
  createApiKey: vi.fn(),
  revokeApiKey: vi.fn(),
}));

vi.mock('../lib/api', () => ({
  fetchApiKeys: apiMocks.fetchApiKeys,
  createApiKey: apiMocks.createApiKey,
  revokeApiKey: apiMocks.revokeApiKey,
}));

describe('Mobile Layouts', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.instances = { data: [], isLoading: false };
    mocks.offerings = { data: [], isLoading: false };
    mocks.workers = { data: [], isLoading: false };
    mocks.models = { data: [], isLoading: false };
    mocks.vaultModels = { data: { models: [] }, isLoading: false };
    apiMocks.fetchApiKeys.mockResolvedValue([]);
  });

  it('renders mobile cards on Instances page', () => {
    mocks.instances = {
      data: [
        {
          id: 'inst_abc123',
          name: 'edge-node-1',
          status: 'running',
          gpu_count: 1,
          gpu_type: 'A100_80GB',
          cost_per_hour: 2.75,
          public_ip: '1.2.3.4',
          provider: 'runpod',
          models: ['org/test-model'],
        },
      ],
      isLoading: false,
    };

    const { container } = render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );

    expect(screen.getByText('edge-node-1')).toBeInTheDocument();
    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    expect(screen.queryByText('NODE ID')).not.toBeInTheDocument();
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

    const { container } = render(
      <MemoryRouter>
        <Models />
      </MemoryRouter>,
    );

    expect(screen.getByText('model-a')).toBeInTheDocument();
    expect(screen.getByText('DEPLOY')).toBeInTheDocument();
    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    expect(screen.queryByText('MODEL NAME & VERSION')).not.toBeInTheDocument();
  });

  it('renders mobile cards on ApiKeys page', async () => {
    apiMocks.fetchApiKeys.mockResolvedValue([
      {
        id: 'key_1',
        name: 'Production',
        key_prefix: 'inf_live_***',
        role: 'admin',
        status: 'active',
        created_at: '2026-03-01T12:00:00Z',
        last_used: null,
      },
    ]);

    const { container } = render(
      <MemoryRouter>
        <ApiKeys />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('Production')).toBeInTheDocument();
    });

    expect(screen.getByText('REVOKE')).toBeInTheDocument();
    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    expect(screen.queryByText('NAME / PREFIX')).not.toBeInTheDocument();
  });

  it('renders mobile log feed cards on Logs page', async () => {
    const { container } = render(
      <MemoryRouter>
        <Logs />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    });

    expect(screen.queryByText('Timestamp')).not.toBeInTheDocument();
  });
});
