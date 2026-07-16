/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useWorkspaceAdminState } from './useWorkspaceAdminState';

const apiMocks = vi.hoisted(() => ({
  fetchWorkspaceQuota: vi.fn(),
  fetchAuditUsage: vi.fn(),
  fetchWorkspaceMembers: vi.fn(),
  fetchWorkspaceInvites: vi.fn(),
  fetchApiKeys: vi.fn(),
  fetchWorkspaceProviderConfigs: vi.fn(),
  fetchProviders: vi.fn(),
  updateWorkspaceQuota: vi.fn(),
  createWorkspaceInvite: vi.fn(),
  revokeWorkspaceInvite: vi.fn(),
  updateWorkspaceMember: vi.fn(),
  removeWorkspaceMember: vi.fn(),
  createApiKey: vi.fn(),
  revokeApiKey: vi.fn(),
  upsertWorkspaceProviderConfig: vi.fn(),
  deleteWorkspaceProviderConfig: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}));

vi.mock('../lib/authAccessClient', () => ({
  fetchApiKeys: apiMocks.fetchApiKeys,
  createApiKey: apiMocks.createApiKey,
  revokeApiKey: apiMocks.revokeApiKey,
}));

vi.mock('../lib/infrastructureClient', () => ({
  fetchProviders: apiMocks.fetchProviders,
}));

vi.mock('../lib/workspaceAdminClient', () => ({
  fetchWorkspaceQuota: apiMocks.fetchWorkspaceQuota,
  fetchAuditUsage: apiMocks.fetchAuditUsage,
  fetchWorkspaceMembers: apiMocks.fetchWorkspaceMembers,
  fetchWorkspaceInvites: apiMocks.fetchWorkspaceInvites,
  fetchWorkspaceProviderConfigs: apiMocks.fetchWorkspaceProviderConfigs,
  updateWorkspaceQuota: apiMocks.updateWorkspaceQuota,
  createWorkspaceInvite: apiMocks.createWorkspaceInvite,
  revokeWorkspaceInvite: apiMocks.revokeWorkspaceInvite,
  updateWorkspaceMember: apiMocks.updateWorkspaceMember,
  removeWorkspaceMember: apiMocks.removeWorkspaceMember,
  upsertWorkspaceProviderConfig: apiMocks.upsertWorkspaceProviderConfig,
  deleteWorkspaceProviderConfig: apiMocks.deleteWorkspaceProviderConfig,
}));

vi.mock('sonner', () => ({
  toast: toastMocks,
}));

describe('useWorkspaceAdminState', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.fetchWorkspaceQuota.mockResolvedValue({
      workspace_id: 'ws_test',
      monthly_request_limit: 1000,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
      updated_at: '2026-04-01T00:00:00Z',
    });
    apiMocks.fetchAuditUsage.mockResolvedValue({
      rows: [{ bucket_start: '2026-04-01T00:00:00Z', bucket_end: '2026-04-02T00:00:00Z', requests: 12, tokens: 2400, successes: 11, errors: 1 }],
    });
    apiMocks.fetchWorkspaceMembers.mockResolvedValue([
      { id: 'member-1', workspace_id: 'ws_test', email: 'admin@example.com', display_name: 'Admin', role: 'admin', created_at: '2026-04-01T00:00:00Z' },
    ]);
    apiMocks.fetchWorkspaceInvites.mockResolvedValue([
      { id: 'invite-1', workspace_id: 'ws_test', email: 'user@example.com', role: 'developer', status: 'pending', created_at: '2026-04-01T00:00:00Z' },
    ]);
    apiMocks.fetchApiKeys.mockResolvedValue([
      { id: 'svc-1', name: 'ci-bot', role: 'operator', principal_type: 'service_account', status: 'active', key_prefix: 'sk_live', last_used: '2026-04-02T00:00:00Z' },
      { id: 'user-1', name: 'human', role: 'admin', principal_type: 'user', status: 'active', key_prefix: 'sk_user', last_used: '2026-04-02T00:00:00Z' },
    ]);
    apiMocks.fetchWorkspaceProviderConfigs.mockResolvedValue([
      { id: 'cfg-1', workspace_id: 'ws_test', provider: 'runpod', configured: true, endpoint: '', options: {}, updated_at: '2026-04-01T00:00:00Z' },
    ]);
    apiMocks.fetchProviders.mockResolvedValue([
      { provider: 'runpod', healthy: true, active_instances: 1, capabilities: { known_regions: ['us-east'] } },
      { provider: 'mock', healthy: true, active_instances: 0, capabilities: {} },
    ]);
    apiMocks.updateWorkspaceQuota.mockResolvedValue({
      workspace_id: 'ws_test',
      monthly_request_limit: 500,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
      updated_at: '2026-04-01T00:00:00Z',
    });
    apiMocks.createWorkspaceInvite.mockResolvedValue({ invitation_token: 'invite-token-123' });
  });

  it('loads admin-facing workspace data and filters provider status noise', async () => {
    const { result } = renderHook(() => useWorkspaceAdminState({
      workspaceId: 'ws_test',
      role: 'admin',
    }));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.quota?.workspace_id).toBe('ws_test');
      expect(result.current.members).toHaveLength(1);
      expect(result.current.invites).toHaveLength(1);
      expect(result.current.serviceAccounts).toHaveLength(1);
      expect(result.current.providerConfigs).toHaveLength(1);
      expect(result.current.providerStatuses).toHaveLength(1);
      expect(result.current.usageRows).toHaveLength(1);
      expect(result.current.memberRoles['member-1']).toBe('admin');
    });
  });

  it('saves quota and updates the local quota snapshot', async () => {
    const { result } = renderHook(() => useWorkspaceAdminState({
      workspaceId: 'ws_test',
      role: 'billing',
    }));

    await waitFor(() => {
      expect(result.current.quota?.monthly_request_limit).toBe(1000);
    });

    await act(async () => {
      await result.current.handleSaveQuota({
        requestLimit: '500',
        tokenLimit: '10000',
        enforceHardLimits: true,
      });
    });

    expect(apiMocks.updateWorkspaceQuota).toHaveBeenCalledWith('ws_test', {
      monthly_request_limit: 500,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
    });
    expect(result.current.quota?.monthly_request_limit).toBe(500);
    expect(toastMocks.success).toHaveBeenCalledWith('Workspace quota updated.');
  });

  it('creates an invite and refreshes the invite list', async () => {
    apiMocks.fetchWorkspaceInvites
      .mockResolvedValueOnce([{ id: 'invite-1', workspace_id: 'ws_test', email: 'user@example.com', role: 'developer', status: 'pending', created_at: '2026-04-01T00:00:00Z' }])
      .mockResolvedValueOnce([
        { id: 'invite-1', workspace_id: 'ws_test', email: 'user@example.com', role: 'developer', status: 'pending', created_at: '2026-04-01T00:00:00Z' },
        { id: 'invite-2', workspace_id: 'ws_test', email: 'new@example.com', role: 'developer', status: 'pending', created_at: '2026-04-03T00:00:00Z' },
      ]);

    const { result } = renderHook(() => useWorkspaceAdminState({
      workspaceId: 'ws_test',
      role: 'admin',
    }));

    await waitFor(() => {
      expect(result.current.invites).toHaveLength(1);
    });

    let token: string | null = null;
    await act(async () => {
      token = await result.current.handleCreateInvite({
        email: 'new@example.com',
        displayName: '',
        inviteRole: 'developer',
      });
    });

    expect(token).toBe('invite-token-123');
    expect(result.current.invites).toHaveLength(2);
    expect(apiMocks.createWorkspaceInvite).toHaveBeenCalledWith('ws_test', {
      email: 'new@example.com',
      display_name: undefined,
      role: 'developer',
    });
    expect(toastMocks.success).toHaveBeenCalledWith('Workspace invitation created.');
  });
});
