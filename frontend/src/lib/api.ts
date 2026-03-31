import type {
  Worker, Model, Stats, ChatCompletionRequest, ChatCompletionResponse,
  Instance, GPUOffering, ProviderStatus, CostSummary, ProvisionRequest,
  VaultModel, VaultStats, VaultModelFilter, CreateVaultModelInput,
  AgentDescriptor, AgentRun, AgentRunDetail
} from '../types';
import type { DeploymentAttemptRecord } from './deploymentHistory';

const API_BASE = '';

// Session info returned by session endpoints
export interface SessionInfo {
  session: { id: string; expires_at: string };
  key: {
    id: string;
    key_prefix: string;
    name: string;
    role: string;
    principal_type?: string;
    workspace_id?: string;
    workspace_slug?: string;
    workspace_name?: string;
  };
  workspace?: { id: string; slug: string; name: string };
  member?: { id: string; email?: string; display_name?: string };
}

export interface WorkspaceRecord {
  id: string;
  slug: string;
  name: string;
  created_at: string;
  status: string;
}

export interface WorkspaceQuotaRecord {
  workspace_id: string;
  monthly_request_limit?: number | null;
  monthly_token_limit?: number | null;
  enforce_hard_limits: boolean;
  updated_at: string;
}

export interface WorkspaceMemberRecord {
  id: string;
  workspace_id: string;
  email: string;
  display_name: string;
  role: string;
  status: string;
  created_at: string;
}

export interface WorkspaceInvitationRecord {
  id: string;
  workspace_id: string;
  email: string;
  display_name: string;
  role: string;
  invited_by_key_id: string;
  created_at: string;
  expires_at: string;
  status: string;
}

export interface WorkspaceInvitationPreview {
  workspace_id: string;
  workspace_slug: string;
  workspace_name: string;
  email: string;
  display_name: string;
  role: string;
  expires_at: string;
  status: string;
}

export interface WorkspaceProviderConfigRecord {
  workspace_id: string;
  provider: string;
  configured: boolean;
  endpoint?: string;
  options?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface AuditUsageRow {
  bucket_start: string;
  workspace_id: string;
  key_id: string;
  requests: number;
  tokens: number;
  successes: number;
  errors: number;
}

export interface AuditUsageResponse {
  bucket: 'day' | 'hour';
  start: string;
  end: string;
  rows: AuditUsageRow[];
}

export interface StreamChatCompletionOptions {
  onUsage?: (usage: ChatCompletionResponse['usage']) => void;
}

export interface AgentsListResponse {
  agents: AgentDescriptor[];
  default_agent_id: string;
}

export interface CreateAgentRunRequest {
  agent_id?: string;
  model: string;
  input: string;
  max_steps?: number;
}

// Create a server-side session (login). Sets HttpOnly cookie.
export async function createSession(apiKey: string): Promise<SessionInfo> {
  const response = await fetch(`${API_BASE}/api/auth/session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ api_key: apiKey }),
  });
  if (!response.ok) {
    if (response.status === 401) throw new Error('Invalid API key');
    if (response.status === 403) throw new Error('Admin access required');
    throw new Error(await readResponseError(response, 'Login failed'));
  }
  return response.json();
}

// Check if a valid session exists (used on startup).
export async function getSession(): Promise<SessionInfo | null> {
  try {
    const response = await fetch(`${API_BASE}/api/auth/session`, {
      credentials: 'include',
    });
    if (!response.ok) return null;
    return response.json();
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
  return response.json();
}

// Destroy the current session (logout). Always succeeds.
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

async function readResponseError(response: Response, fallbackMessage: string): Promise<string> {
  const contentType = response.headers?.get?.('content-type') || '';
  const statusCode = response.status ?? 0;
  const statusText = response.statusText ?? '';
  const status = `${statusCode || 'unknown'} ${statusText}`.trim();
  let detail = '';

  try {
    if (contentType.includes('application/json') || (!contentType && typeof response.json === 'function')) {
      const payload = await response.json();
      detail =
        payload?.error?.message ||
        payload?.message ||
        (payload ? JSON.stringify(payload) : '');
    } else {
      detail = (await response.text()).trim();
    }
  } catch {
    try {
      detail = (await response.text()).trim();
    } catch {
      detail = '';
    }
  }

  if (!detail) {
    return `${fallbackMessage} (${status})`;
  }
  return `${fallbackMessage} (${status}): ${detail}`;
}

async function authFetch(url: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(url, {
    ...init,
    credentials: 'include',
  });

  if (response.status === 401) {
    window.dispatchEvent(new Event('auth-expired'));
  }

  return response;
}

export async function fetchWorkers(): Promise<Worker[]> {
  const response = await authFetch(`${API_BASE}/api/workers`);
  if (!response.ok) throw new Error('Failed to fetch workers');
  const data = await response.json();
  return data.workers;
}

export async function fetchModels(): Promise<Model[]> {
  const response = await authFetch(`${API_BASE}/v1/models`);
  if (!response.ok) throw new Error('Failed to fetch models');
  const data = await response.json();
  return data.data;
}

export async function fetchAgents(): Promise<AgentsListResponse> {
  const response = await authFetch(`${API_BASE}/api/agents`);
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to fetch agents'));
  }
  return response.json();
}

export async function createAgentRun(request: CreateAgentRunRequest): Promise<AgentRun> {
  const response = await authFetch(`${API_BASE}/api/agents/runs`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to start agent run'));
  }

  const data = await response.json();
  return data.run;
}

export async function fetchAgentRunDetail(runID: string): Promise<AgentRunDetail> {
  const response = await authFetch(`${API_BASE}/api/agents/runs/${runID}`);
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to fetch agent run'));
  }

  const data = await response.json();
  return {
    run: data.run,
    steps: data.steps || [],
  };
}

export async function cancelAgentRun(runID: string): Promise<AgentRun> {
  const response = await authFetch(`${API_BASE}/api/agents/runs/${runID}/cancel`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to cancel agent run'));
  }

  const data = await response.json();
  return data.run;
}

export async function fetchStats(): Promise<Stats> {
  const response = await authFetch(`${API_BASE}/api/stats`);
  if (!response.ok) throw new Error('Failed to fetch stats');
  return response.json();
}

export async function sendChatCompletion(request: ChatCompletionRequest): Promise<ChatCompletionResponse> {
  const response = await authFetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Request failed'));
  }

  return response.json();
}

export async function* streamChatCompletion(
  request: ChatCompletionRequest,
  options?: StreamChatCompletionOptions,
): AsyncGenerator<string, void, unknown> {
  const response = await authFetch(`${API_BASE}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ ...request, stream: true }),
  });

  if (!response.ok) {
    const message = await readResponseError(response, 'Request failed');
    if (message.toLowerCase().includes('ngrok')) {
      throw new Error('Please visit the ngrok URL directly in your browser first to bypass the interstitial page');
    }
    throw new Error(message);
  }

  const reader = response.body?.getReader();
  if (!reader) throw new Error('No response body');

  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = line.slice(6);
        if (data === '[DONE]') return;

        try {
          const parsed = JSON.parse(data);
          const usage = parsed.usage;
          if (usage?.prompt_tokens != null && usage?.completion_tokens != null && usage?.total_tokens != null) {
            options?.onUsage?.(usage);
          }
          const content = parsed.choices?.[0]?.delta?.content;
          if (content) yield content;
        } catch {
          // Ignore parse errors for individual chunks
        }
      }
    }
  }
}

// Instance Management API

export async function fetchInstances(): Promise<Instance[]> {
  const response = await authFetch(`${API_BASE}/api/instances`);
  if (!response.ok) throw new Error('Failed to fetch instances');
  const data = await response.json();
  return data.instances;
}

export async function fetchOfferings(): Promise<GPUOffering[]> {
  const response = await authFetch(`${API_BASE}/api/offerings`);
  if (!response.ok) throw new Error('Failed to fetch offerings');
  const data = await response.json();
  return data.offerings;
}

export async function fetchProviders(): Promise<ProviderStatus[]> {
  const response = await authFetch(`${API_BASE}/api/providers`);
  if (!response.ok) throw new Error('Failed to fetch providers');
  const data = await response.json();
  return data.providers;
}

export async function fetchCosts(): Promise<CostSummary> {
  const response = await authFetch(`${API_BASE}/api/costs`);
  if (!response.ok) throw new Error('Failed to fetch costs');
  return response.json();
}

export async function provisionInstance(request: ProvisionRequest): Promise<Instance> {
  const response = await authFetch(`${API_BASE}/api/instances/provision`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Provisioning failed'));
  }

  const data = await response.json();
  return data.instance;
}

export async function terminateInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}`, {
    method: 'DELETE',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Termination failed'));
  }
}

export async function startInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}/start`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Start failed'));
  }
}

export async function stopInstance(instanceId: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/instances/${instanceId}/stop`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Stop failed'));
  }
}

export async function fetchDeploymentAttempts(): Promise<DeploymentAttemptRecord[]> {
  const response = await authFetch(`${API_BASE}/api/deployments`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch deployment history'));
  const data = await response.json();
  return data.attempts;
}

export async function updateDeploymentVerification(
  attemptId: string,
  verification: NonNullable<DeploymentAttemptRecord['inference_verification']>,
): Promise<DeploymentAttemptRecord> {
  const response = await authFetch(`${API_BASE}/api/deployments/${attemptId}/verification`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(verification),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to update deployment verification'));
  }
  const data = await response.json();
  return data.attempt;
}

export async function markDeploymentAutoVerificationRequested(
  attemptId: string,
  requestedAt: string,
): Promise<DeploymentAttemptRecord> {
  const response = await authFetch(`${API_BASE}/api/deployments/${attemptId}/auto-verification`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ requested_at: requestedAt }),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to update deployment auto verification'));
  }
  const data = await response.json();
  return data.attempt;
}

// Vault (Model Registry) API

export async function fetchVaultModels(filters?: VaultModelFilter): Promise<{ models: VaultModel[]; count: number }> {
  const params = new URLSearchParams();
  if (filters?.family) params.set('family', filters.family);
  if (filters?.status) params.set('status', filters.status);
  if (filters?.search) params.set('search', filters.search);
  if (filters?.quantization) params.set('quantization', filters.quantization);
  if (filters?.tag) params.set('tag', filters.tag);

  const query = params.toString();
  const url = `${API_BASE}/api/vault/models${query ? '?' + query : ''}`;
  const response = await authFetch(url);
  if (!response.ok) throw new Error('Failed to fetch vault models');
  return response.json();
}

export async function fetchVaultStats(): Promise<VaultStats> {
  const response = await authFetch(`${API_BASE}/api/vault/stats`);
  if (!response.ok) throw new Error('Failed to fetch vault stats');
  return response.json();
}

export async function fetchVaultFamilies(): Promise<string[]> {
  const response = await authFetch(`${API_BASE}/api/vault/models/families`);
  if (!response.ok) throw new Error('Failed to fetch families');
  const data = await response.json();
  return data.families;
}

export async function registerVaultModel(model: CreateVaultModelInput): Promise<VaultModel> {
  const response = await authFetch(`${API_BASE}/api/vault/models`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(model),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Registration failed'));
  }
  return response.json();
}

export async function deleteVaultModel(id: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/vault/models/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Delete failed'));
  }
}

// Auth API (admin only)

export interface ApiKeyRecord {
  id: string;
  workspace_id?: string;
  workspace_slug?: string;
  workspace_name?: string;
  key_prefix: string;
  name: string;
  role: string;
  principal_type?: string;
  membership_id?: string;
  created_at: string;
  last_used: string | null;
  status: string;
}

export async function fetchApiKeys(): Promise<ApiKeyRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/keys`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch API keys'));
  const data = await response.json();
  return data.keys;
}

export async function createApiKey(
  name: string,
  role: string = 'user',
  principalType: string = 'human',
): Promise<{ key: string; record: ApiKeyRecord }> {
  const response = await authFetch(`${API_BASE}/api/auth/keys`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, role, principal_type: principalType }),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to create key'));
  }
  return response.json();
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
  const data = await response.json();
  return data.workspaces;
}

export async function fetchWorkspaceQuota(workspaceId: string): Promise<WorkspaceQuotaRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/quota`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace quota'));
  const data = await response.json();
  return data.quota;
}

export async function updateWorkspaceQuota(
  workspaceId: string,
  payload: {
    monthly_request_limit?: number | null;
    monthly_token_limit?: number | null;
    enforce_hard_limits: boolean;
  },
): Promise<WorkspaceQuotaRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/quota`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to update workspace quota'));
  const data = await response.json();
  return data.quota;
}

export async function fetchWorkspaceMembers(workspaceId: string): Promise<WorkspaceMemberRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/members`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace members'));
  const data = await response.json();
  return data.members;
}

export async function updateWorkspaceMember(
  workspaceId: string,
  memberId: string,
  payload: { role: string },
): Promise<WorkspaceMemberRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/members/${memberId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to update workspace member'));
  const data = await response.json();
  return data.member;
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
  const data = await response.json();
  return data.invitations;
}

export async function fetchInvitationPreview(token: string): Promise<WorkspaceInvitationPreview> {
  const response = await fetch(`${API_BASE}/api/auth/invitations/preview?token=${encodeURIComponent(token)}`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to load invitation'));
  const data = await response.json();
  return data.invitation;
}

export async function acceptWorkspaceInvitation(
  invitationToken: string,
  displayName?: string,
): Promise<{ membership: WorkspaceMemberRecord; key: string; record: ApiKeyRecord }> {
  const response = await fetch(`${API_BASE}/api/auth/invitations/accept`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      invitation_token: invitationToken,
      display_name: displayName,
    }),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to accept invitation'));
  return response.json();
}

export async function fetchWorkspaceProviderConfigs(workspaceId: string): Promise<WorkspaceProviderConfigRecord[]> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch workspace provider configs'));
  const data = await response.json();
  return data.providers;
}

export async function upsertWorkspaceProviderConfig(
  workspaceId: string,
  provider: string,
  payload: { api_key: string; api_secret?: string; endpoint?: string; options?: Record<string, string> },
): Promise<WorkspaceProviderConfigRecord> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers/${provider}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to save workspace provider config'));
  const data = await response.json();
  return data.provider;
}

export async function deleteWorkspaceProviderConfig(workspaceId: string, provider: string): Promise<void> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/providers/${provider}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to delete workspace provider config'));
}

export async function createWorkspaceInvite(
  workspaceId: string,
  payload: { email: string; display_name?: string; role?: string },
): Promise<{ invitation_token: string; invitation: WorkspaceInvitationRecord }> {
  const response = await authFetch(`${API_BASE}/api/auth/workspaces/${workspaceId}/invites`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to create workspace invite'));
  return response.json();
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
