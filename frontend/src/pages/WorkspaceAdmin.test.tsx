/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { WorkspaceAdmin } from './WorkspaceAdmin';
import type { AuditUsageResponse } from '../lib/apiCore';

const state = vi.hoisted(() => ({
  empty: [],
  handlers: {
    setMemberRoles: vi.fn(),
    handleSaveQuota: vi.fn(),
    handleCreateInvite: vi.fn(),
    handleRevokeInvite: vi.fn(),
    handleUpdateMemberRole: vi.fn(),
    handleRemoveMember: vi.fn(),
    handleCreateServiceAccount: vi.fn(),
    handleRevokeServiceAccount: vi.fn(),
    handleSaveProviderConfig: vi.fn(),
    handleDeleteProviderConfig: vi.fn(),
  },
  usage: null as AuditUsageResponse | null,
}));

function usageFixture(): AuditUsageResponse {
  return {
    bucket: 'day' as const,
    start: '2026-07-01T00:00:00Z',
    end: '2026-07-02T00:00:00Z',
    reconciliation: { status: 'ok' as const, discrepancies: [] },
    rows: [{
      bucket_start: '2026-07-01T00:00:00Z',
      workspace_id: 'redacted',
      key_id: 'redacted',
      attempts: 3,
      requests: 2,
      tokens: 200,
      exact_tokens: 120,
      estimated_tokens: 80,
      successes: 2,
      errors: 1,
      cost: {
        currency: 'USD' as const,
        cost_usd: 0.001,
        cost_per_request_usd: 0.0005,
        cost_per_token_usd: 0.000005,
        cost_per_1m_tokens_usd: 5,
        costed_requests: 2,
        costed_tokens: 200,
        exact_requests: 0,
        estimated_requests: 2,
        unavailable_requests: 1,
      },
    }],
  };
}

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock('../lib/auth-context', () => ({
  useAuthSession: () => ({
    session: {
      workspace: { id: 'redacted' },
      key: { role: 'billing' },
      member: null,
    },
  }),
}));

vi.mock('../hooks/useWorkspaceAdminState', () => ({
  useWorkspaceAdminState: () => ({
    loading: false,
    quota: null,
    members: state.empty,
    invites: state.empty,
    serviceAccounts: state.empty,
    providerConfigs: state.empty,
    providerStatuses: state.empty,
    usage: state.usage,
    usageRows: state.usage?.rows ?? state.empty,
    memberRoles: {},
    savingQuota: false,
    creatingInvite: false,
    creatingServiceAccount: false,
    savingProviderConfig: false,
    updatingMemberId: null,
    removingMemberId: null,
    ...state.handlers,
  }),
}));

describe('WorkspaceAdmin usage evidence', () => {
  beforeEach(() => {
    state.usage = usageFixture();
  });

  it('shows aggregate cost units, conservative token labels, range, and reconciliation', () => {
    render(<WorkspaceAdmin />);

    expect(screen.getByText('120 exact / 80 estimated, mixed, or unknown')).toBeInTheDocument();
    expect(screen.getByText('$0.001')).toBeInTheDocument();
    expect(screen.getByText('$0.0005 USD/request')).toBeInTheDocument();
    expect(screen.getByText('$0.000005 USD/token')).toBeInTheDocument();
    expect(screen.getByText('$5.00 USD/1M tokens')).toBeInTheDocument();
    expect(screen.getByText('RECONCILED')).toBeInTheDocument();
    expect(screen.getByText(/Half-open UTC range/)).toHaveTextContent(
      'Half-open UTC range [2026-07-01T00:00:00Z, 2026-07-02T00:00:00Z)',
    );
    expect(screen.getByText(/price-version metadata is retained/)).toBeInTheDocument();
  });

  it('does not present an empty range as reconciled or zero cost', () => {
    if (!state.usage) throw new Error('usage fixture is missing');
    state.usage.rows = [];

    render(<WorkspaceAdmin />);

    expect(screen.getByText('NO DATA')).toBeInTheDocument();
    expect(screen.getByText(/no reconciliation claim is made/i)).toBeInTheDocument();
    expect(screen.getByText(/Cost is unavailable/)).toBeInTheDocument();
    expect(screen.queryByText('RECONCILED')).not.toBeInTheDocument();
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument();
  });

  it('distinguishes an unavailable summary from a valid empty range', () => {
    state.usage = null;

    render(<WorkspaceAdmin />);

    expect(screen.getByText(/Cost evidence is unavailable/)).toBeInTheDocument();
    expect(screen.getByText(/usage summary could not be loaded, so no reconciliation claim/i)).toBeInTheDocument();
    expect(screen.queryByText('NO DATA')).not.toBeInTheDocument();
  });
});
