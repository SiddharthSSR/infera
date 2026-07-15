import type {
  WorkspaceInvitationCreateRequest,
  WorkspaceInvitationCreateResponse,
  WorkspaceInvitationRecord,
  WorkspaceMemberRecord,
  WorkspaceMemberUpdateRequest,
  WorkspaceProviderConfigRecord,
  WorkspaceProviderConfigUpsertRequest,
  WorkspaceQuotaRecord,
  WorkspaceQuotaUpdateRequest,
} from '../types';
import {
  parseWorkspaceInvitationCreateResponse,
  parseWorkspaceInvitationsResponse,
  parseWorkspaceMemberResponse,
  parseWorkspaceMembersResponse,
  parseWorkspaceProviderConfigResponse,
  parseWorkspaceProviderConfigsResponse,
  parseWorkspaceQuotaResponse,
} from './workspaceAdmin';
import { API_BASE, type AuditUsageResponse, authFetch, readResponseError } from './apiCore';

export async function fetchWorkspaceQuota(workspaceId: string): Promise<WorkspaceQuotaRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/quota`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace quota'));
  return parseWorkspaceQuotaResponse(await response.json()).quota;
}

export async function updateWorkspaceQuota(
  workspaceId: string,
  payload: WorkspaceQuotaUpdateRequest,
): Promise<WorkspaceQuotaRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/quota`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to update workspace quota'));
  return parseWorkspaceQuotaResponse(await response.json()).quota;
}

export async function fetchWorkspaceMembers(workspaceId: string): Promise<WorkspaceMemberRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/members`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace members'));
  return parseWorkspaceMembersResponse(await response.json()).members;
}

export async function updateWorkspaceMember(
  workspaceId: string,
  memberId: string,
  payload: WorkspaceMemberUpdateRequest,
): Promise<WorkspaceMemberRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/members/${memberId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to update workspace member'));
  return parseWorkspaceMemberResponse(await response.json()).member;
}

export async function removeWorkspaceMember(workspaceId: string, memberId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/members/${memberId}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to remove workspace member'));
}

export async function fetchWorkspaceInvites(workspaceId: string): Promise<WorkspaceInvitationRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/invites`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace invites'));
  return parseWorkspaceInvitationsResponse(await response.json()).invitations;
}

export async function fetchWorkspaceProviderConfigs(workspaceId: string): Promise<WorkspaceProviderConfigRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace provider configs'));
  return parseWorkspaceProviderConfigsResponse(await response.json()).providers;
}

export async function upsertWorkspaceProviderConfig(
  workspaceId: string,
  provider: string,
  payload: WorkspaceProviderConfigUpsertRequest,
): Promise<WorkspaceProviderConfigRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers/${provider}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to save workspace provider config'));
  return parseWorkspaceProviderConfigResponse(await response.json()).provider;
}

export async function deleteWorkspaceProviderConfig(workspaceId: string, provider: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers/${provider}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to delete workspace provider config'));
}

export async function createWorkspaceInvite(
  workspaceId: string,
  payload: WorkspaceInvitationCreateRequest,
): Promise<WorkspaceInvitationCreateResponse> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/invites`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to create workspace invite'));
  return parseWorkspaceInvitationCreateResponse(await response.json());
}

export async function revokeWorkspaceInvite(workspaceId: string, inviteId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/invites/${inviteId}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to revoke workspace invite'));
}

export async function fetchAuditUsage(params: {
  start?: string;
  end?: string;
  bucket?: 'day' | 'hour';
  workspace_id?: string;
  key_id?: string;
  model?: string;
} = {}): Promise<AuditUsageResponse> {
  const searchParams = new URLSearchParams();
  if (params.start) searchParams.set('start', params.start);
  if (params.end) searchParams.set('end', params.end);
  if (params.bucket) searchParams.set('bucket', params.bucket);
  if (params.workspace_id) searchParams.set('workspace_id', params.workspace_id);
  if (params.key_id) searchParams.set('key_id', params.key_id);
  if (params.model) searchParams.set('model', params.model);

  const query = searchParams.toString();
  const response = await authFetch(`${API_BASE}/api/audit/usage${query ? `?${query}` : ''}`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch usage'));
  return response.json();
}
