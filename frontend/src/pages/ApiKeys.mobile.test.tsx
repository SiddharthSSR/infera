/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ApiKeys } from './ApiKeys';

const apiMocks = vi.hoisted(() => ({
  fetchApiKeys: vi.fn(),
  createApiKey: vi.fn(),
  revokeApiKey: vi.fn(),
}));

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => true,
}));

vi.mock('../lib/api', () => ({
  fetchApiKeys: apiMocks.fetchApiKeys,
  createApiKey: apiMocks.createApiKey,
  revokeApiKey: apiMocks.revokeApiKey,
}));

describe('ApiKeys mobile layout', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.fetchApiKeys.mockResolvedValue([]);
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
});
