/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { InstanceMobileCard } from './InstanceMobileCard';

describe('InstanceMobileCard', () => {
  it('renders the mobile instance card summary', () => {
    const { container } = render(
      <InstanceMobileCard
        instance={{
          id: 'inst_abc123',
          name: 'edge-node-1',
          status: 'running',
          gpu_count: 1,
          gpu_type: 'A100_80GB',
          cost_per_hour: 2.75,
          public_ip: '1.2.3.4',
          provider: 'runpod',
          models: ['org/test-model'],
        }}
        statusClass=""
        statusLabel="Running"
        actions={<button type="button">STOP</button>}
      />,
    );

    expect(screen.getByText('edge-node-1')).toBeInTheDocument();
    expect(screen.getByText('Running')).toBeInTheDocument();
    expect(screen.getByText('STOP')).toBeInTheDocument();
    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
  });

  it('renders incident details when provided', () => {
    render(
      <InstanceMobileCard
        instance={{
          id: 'inst_incident',
          name: 'edge-node-2',
          status: 'running',
          gpu_count: 1,
          gpu_type: 'A100_80GB',
          cost_per_hour: 3.1,
          public_ip: null,
          provider: 'runpod',
          models: ['org/test-model'],
        }}
        statusClass=""
        statusLabel="Running"
        readiness={{ label: 'SERVING UNVERIFIED', detail: 'Runtime looks ready, but the latest signal is stale.', tone: 'warning' }}
        incident={{ title: 'INFERENCE CHECK FAILED', detail: 'Latest verification request failed against the deployed model.', tone: 'error' }}
      />,
    );

    expect(screen.getByText('INCIDENT')).toBeInTheDocument();
    expect(screen.getByText('INFERENCE CHECK FAILED')).toBeInTheDocument();
    expect(screen.getByText('Latest verification request failed against the deployed model.')).toBeInTheDocument();
  });

  it('hides duplicate incident details when they match readiness exactly', () => {
    render(
      <InstanceMobileCard
        instance={{
          id: 'inst_duplicate',
          name: 'edge-node-3',
          status: 'running',
          gpu_count: 1,
          gpu_type: 'A100_80GB',
          cost_per_hour: 3.1,
          public_ip: null,
          provider: 'runpod',
          models: ['org/test-model'],
        }}
        statusClass=""
        statusLabel="Running"
        readiness={{ label: 'WORKER NOT CONNECTED', detail: 'Node has been running for 11 minutes without a worker connection.', tone: 'error' }}
        incident={{ title: 'WORKER NOT CONNECTED', detail: 'Node has been running for 11 minutes without a worker connection.', tone: 'error' }}
      />,
    );

    expect(screen.queryByText('INCIDENT')).not.toBeInTheDocument();
    expect(screen.getAllByText('WORKER NOT CONNECTED')).toHaveLength(1);
  });
});
