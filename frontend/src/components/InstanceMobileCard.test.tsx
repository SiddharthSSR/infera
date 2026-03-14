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
});
