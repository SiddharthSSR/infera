/// <reference types="vitest/globals" />
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { Instance } from '../types';
import { InstancesList } from './InstancesList';
import { InstanceActions } from './instances/InstanceRow';

vi.mock('../hooks/useInfrastructureApi', () => ({
  useTerminateInstance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useStartInstance: () => ({ isPending: false, mutate: vi.fn(), mutateAsync: vi.fn() }),
  useStopInstance: () => ({ isPending: false, mutate: vi.fn(), mutateAsync: vi.fn() }),
}));

const baseInstance: Instance = {
  id: 'inst-actions',
  provider_id: 'provider-actions',
  provider: 'runpod',
  name: 'Lifecycle action node',
  status: 'running',
  gpu_type: 'A100_80GB',
  gpu_count: 1,
  vcpu: 8,
  memory_gb: 64,
  storage_gb: 100,
  cost_per_hour: 1,
  spot_instance: false,
  created_at: '2026-07-17T12:00:00.000Z',
};

describe('instance lifecycle action guards', () => {
  it.each(['starting', 'stopping'] as const)(
    'disables card termination while an instance is %s',
    (status) => {
      render(<InstancesList instances={[{ ...baseInstance, status }]} isLoading={false} onProvision={vi.fn()} />);

      expect(screen.getByTitle('Terminate')).toBeDisabled();
    },
  );

  it('does not render a card action for a terminating instance', () => {
    render(<InstancesList instances={[{ ...baseInstance, status: 'terminating' }]} isLoading={false} onProvision={vi.fn()} />);

    expect(screen.queryByTitle('Terminate')).not.toBeInTheDocument();
  });

  it.each(['starting', 'stopping', 'terminating'] as const)(
    'disables row termination while an instance is %s',
    (status) => {
      render(<InstanceActions instance={{ ...baseInstance, status }} />);

      expect(screen.getByRole('button', { name: 'TERMINATE' })).toBeDisabled();
    },
  );

  it('does not expose row termination for a terminated instance', () => {
    render(<InstanceActions instance={{ ...baseInstance, status: 'terminated' }} />);

    expect(screen.queryByRole('button', { name: 'TERMINATE' })).not.toBeInTheDocument();
  });
});
