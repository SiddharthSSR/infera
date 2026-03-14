/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ApiKeys } from './ApiKeys';

const apiMocks = vi.hoisted(() => ({
  fetchApiKeys: vi.fn(),
  createApiKey: vi.fn(),
  revokeApiKey: vi.fn(),
}));

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => false,
}));

vi.mock('../lib/auth-context', () => ({
  useAuthSession: () => ({
    session: {
      workspace: { id: 'ws_alpha', name: 'Alpha Team', slug: 'alpha-team' },
      key: { role: 'admin', principal_type: 'human' },
    },
    availableWorkspaces: [
      { id: 'ws_alpha', name: 'Alpha Team', slug: 'alpha-team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
      { id: 'ws_beta', name: 'Beta Team', slug: 'beta-team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ],
  }),
}));

vi.mock('../lib/api', () => ({
  fetchApiKeys: apiMocks.fetchApiKeys,
  createApiKey: apiMocks.createApiKey,
  revokeApiKey: apiMocks.revokeApiKey,
}));

describe('ApiKeys session clarity', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.fetchApiKeys.mockResolvedValue([
      {
        id: 'key_1',
        name: 'Operator Console',
        key_prefix: 'inf_alpha_***',
        role: 'operator',
        principal_type: 'human',
        workspace_name: 'Alpha Team',
        workspace_slug: 'alpha-team',
        status: 'active',
        created_at: '2026-03-01T12:00:00Z',
        last_used: '2026-03-14T12:00:00Z',
      },
      {
        id: 'key_2',
        name: 'CI Bot',
        key_prefix: 'inf_ci_***',
        role: 'operator',
        principal_type: 'service_account',
        workspace_name: 'Alpha Team',
        workspace_slug: 'alpha-team',
        status: 'active',
        created_at: '2026-03-02T12:00:00Z',
        last_used: null,
      },
    ]);
  });

  it('renders active session scope and workspace summary', async () => {
    render(
      <MemoryRouter>
        <ApiKeys />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('ACTIVE SESSION')).toBeInTheDocument();
    });

    expect(screen.getAllByText('Alpha Team').length).toBeGreaterThan(0);
    expect(screen.getByText('HUMAN SESSION')).toBeInTheDocument();
    expect(screen.getByText(/Switching workspaces updates the active session context/i)).toBeInTheDocument();
    expect(screen.getAllByText('SERVICE ACCOUNTS').length).toBeGreaterThan(0);
  });

  it('switches role options when creating a service-account key', async () => {
    render(
      <MemoryRouter>
        <ApiKeys />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('CREATE WORKSPACE KEY')).toBeInTheDocument();
    });

    expect(screen.getByText('Use for a person who needs a dashboard session in this workspace.')).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/Service account/i));

    expect(screen.getByText('Use for CI, scripts, agents, and automation. No dashboard session access.')).toBeInTheDocument();
    expect(screen.queryByText('Full workspace administration, membership, key, quota, and provider management.')).not.toBeInTheDocument();
  });
});
