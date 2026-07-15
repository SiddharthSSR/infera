/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useDashboardWorkspaceState } from './useDashboardWorkspaceState';

const apiMocks = vi.hoisted(() => ({
  fetchWorkspaceQuota: vi.fn(),
  fetchAuditUsage: vi.fn(),
  fetchWorkspaceInvites: vi.fn(),
  fetchApiKeys: vi.fn(),
  updateWorkspaceQuota: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}));

vi.mock('../lib/authAccessClient', () => ({
  fetchApiKeys: apiMocks.fetchApiKeys,
}));

vi.mock('../lib/workspaceAdminClient', () => ({
  fetchWorkspaceQuota: apiMocks.fetchWorkspaceQuota,
  fetchAuditUsage: apiMocks.fetchAuditUsage,
  fetchWorkspaceInvites: apiMocks.fetchWorkspaceInvites,
  updateWorkspaceQuota: apiMocks.updateWorkspaceQuota,
}));

vi.mock('sonner', () => ({
  toast: toastMocks,
}));

describe('useDashboardWorkspaceState', () => {
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
      rows: [{ bucket_start: '2026-04-01T00:00:00Z', bucket_end: '2026-04-02T00:00:00Z', requests: 12, tokens: 2400 }],
    });
    apiMocks.fetchWorkspaceInvites.mockResolvedValue([{ id: 'invite-1', workspace_id: 'ws_test', email: 'user@example.com', role: 'developer', status: 'pending', created_at: '2026-04-01T00:00:00Z' }]);
    apiMocks.fetchApiKeys.mockResolvedValue([
      { id: 'svc-1', principal_type: 'service_account', status: 'active' },
      { id: 'user-1', principal_type: 'user', status: 'active' },
    ]);
    apiMocks.updateWorkspaceQuota.mockResolvedValue({
      workspace_id: 'ws_test',
      monthly_request_limit: 500,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
      updated_at: '2026-04-01T00:00:00Z',
    });
  });

  it('fetches quota, usage, invites, and service accounts for admin roles', async () => {
    const { result } = renderHook(() => useDashboardWorkspaceState({
      workspaceID: 'ws_test',
      role: 'admin',
    }));

    await waitFor(() => {
      expect(result.current.quota?.workspace_id).toBe('ws_test');
      expect(result.current.usageRows).toHaveLength(1);
      expect(result.current.workspaceInvites).toHaveLength(1);
      expect(result.current.workspaceServiceAccounts).toHaveLength(1);
    });

    expect(result.current.canEditQuota).toBe(true);
    expect(apiMocks.fetchWorkspaceQuota).toHaveBeenCalledWith('ws_test');
    expect(apiMocks.fetchWorkspaceInvites).toHaveBeenCalledWith('ws_test');
  });

  it('clears state when no workspace is active', async () => {
    const { result } = renderHook(() => useDashboardWorkspaceState({
      workspaceID: undefined,
      role: 'admin',
    }));

    expect(result.current.quota).toBeNull();
    expect(result.current.usageRows).toEqual([]);
    expect(result.current.workspaceInvites).toEqual([]);
    expect(result.current.workspaceServiceAccounts).toEqual([]);
  });

  it('updates quota through the save handler and surfaces success feedback', async () => {
    const { result } = renderHook(() => useDashboardWorkspaceState({
      workspaceID: 'ws_test',
      role: 'billing',
    }));

    await waitFor(() => {
      expect(result.current.quota?.monthly_request_limit).toBe(1000);
    });

    await act(async () => {
      await result.current.handleQuickConfigSave('monthly_request_limit', '500');
    });

    expect(apiMocks.updateWorkspaceQuota).toHaveBeenCalledWith('ws_test', {
      monthly_request_limit: 500,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
    });
    expect(result.current.quota?.monthly_request_limit).toBe(500);
    expect(toastMocks.success).toHaveBeenCalledWith('Configuration updated.');
  });
});
