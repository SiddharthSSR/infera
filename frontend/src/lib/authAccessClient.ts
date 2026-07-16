import type {
  ApiKeyCreateRequest,
  ApiKeyCreateResponse,
  ApiKeyRecord,
  SessionInfo,
  WorkspaceInvitationAcceptResponse,
  WorkspaceInvitationPreview,
  WorkspaceRecord,
} from '../types';
import { parseApiKeyCreateResponse, parseApiKeysResponse, parseSessionResponse, parseWorkspacesResponse } from './authAccess';
import { parseWorkspaceInvitationAcceptResponse, parseWorkspaceInvitationPreviewResponse } from './workspaceAdmin';
import { API_BASE, authFetch, readResponseError, readResponseMessage } from './apiCore';

export async function createSession(apiKey: string): Promise<SessionInfo> {
  const response = await fetch(`${API_BASE}/api/auth/session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ api_key: apiKey }),
  });
  if (!response.ok) {
    if (response.status === 401) throw new Error(await readResponseMessage(response, 'Invalid API key'));
    if (response.status === 403) throw new Error(await readResponseMessage(response, 'Admin access required'));
    throw new Error(await readResponseError(response, 'Login failed'));
  }
  return parseSessionResponse(await response.json());
}

export async function getSession(): Promise<SessionInfo | null> {
  try {
    const response = await fetch(`${API_BASE}/api/auth/session`, {
      credentials: 'include',
    });
    if (!response.ok) return null;
    return parseSessionResponse(await response.json());
  } catch {
    return null;
  }
}

export async function switchSessionWorkspace(workspaceId: string): Promise<SessionInfo> {
  const response = await authFetch(`${API_BASE}/api/auth/session/workspace`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace_id: workspaceId }),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to switch workspace'));
  }
  return parseSessionResponse(await response.json());
}

export async function destroySession(): Promise<void> {
  try {
    await fetch(`${API_BASE}/api/auth/session`, {
      method: 'DELETE',
      credentials: 'include',
    });
  } catch {
    // Ignore errors — cookie will expire anyway.
  }
}

export async function fetchApiKeys(): Promise<ApiKeyRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/keys`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch API keys'));
  return parseApiKeysResponse(await response.json()).keys;
}

export async function createApiKey(
  name: string,
  role: string = 'user',
  principalType: string = 'human',
): Promise<ApiKeyCreateResponse> {
  const payload: ApiKeyCreateRequest = {
    name,
    role,
    principal_type: principalType,
  };
  const response = await authFetch(`${API_BASE}/api/auth/keys`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to create key'));
  }
  return parseApiKeyCreateResponse(await response.json());
}

export async function revokeApiKey(id: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/auth/keys/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to revoke key'));
  }
}

export async function fetchWorkspaces(): Promise<WorkspaceRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspaces'));
  return parseWorkspacesResponse(await response.json()).workspaces;
}

export async function fetchInvitationPreview(token: string): Promise<WorkspaceInvitationPreview> {
  const response = await fetch(`${API_BASE}/api/auth/invitations/preview?token=${encodeURIComponent(token)}`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to load invitation'));
  return parseWorkspaceInvitationPreviewResponse(await response.json()).invitation;
}

export async function acceptWorkspaceInvitation(
  invitationToken: string,
  displayName?: string,
): Promise<WorkspaceInvitationAcceptResponse> {
  const response = await fetch(`${API_BASE}/api/auth/invitations/accept`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      invitation_token: invitationToken,
      display_name: displayName,
    }),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to accept invitation'));
  return parseWorkspaceInvitationAcceptResponse(await response.json());
}
