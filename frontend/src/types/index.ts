// API Types
import type { GPUType, ProviderType } from './generated/infrastructure';

export interface Worker {
  worker_id: string;
  address: string;
  status: 'healthy' | 'degraded' | 'unhealthy' | 'draining' | 'offline';
  models: string[];
  gpu_utilization: number;
  memory_used: number;
  memory_total: number;
  queue_depth: number;
  requests_per_sec: number;
  avg_latency_ms: number;
  p50_latency_ms: number;
  p99_latency_ms: number;
  error_rate: number;
  last_heartbeat: string;
}

export interface Model {
  id: string;
  object: string;
  created: number;
  owned_by: string;
  // Vault fields (present when vault is connected)
  loaded?: boolean;
  family?: string;
  parameters?: string;
  quantization?: string;
  vram_required?: number;
  max_context?: number;
  tags?: string[];
  vault_status?: string;
}

export interface Stats {
  workers: {
    total: number;
    healthy: number;
  };
  models: {
    available: number;
  };
  requests: {
    per_second: number;
    queue_depth: number;
  };
  latency: {
    avg_ms: number;
  };
  gpu: {
    avg_utilization: number;
  };
  memory: {
    used_bytes: number;
    total_bytes: number;
  };
  uptime_seconds: number;
}

export type {
  ChatCompletionChunk,
  ChatCompletionChoice,
  ChatCompletionDelta,
  ChatCompletionError,
  ChatCompletionErrorResponse,
  ChatCompletionRequest,
  ChatCompletionResponse,
  ChatCompletionUsage,
  ChatMessage,
  ChatMessageRole,
  ChatToolCall,
  ChatToolChoice,
  ChatToolChoiceObject,
  ChatToolDefinition,
  ChatToolFunction,
} from './generated/openaiChat';

export type {
  CostSummary,
  GPUOffering,
  GPUType,
  Instance,
  InstanceEngine,
  InstanceStatus,
  WorkerRegistrationStatus,
  KnownGPUType,
  OfferingsResponse,
  ProviderCapabilities,
  ProviderStatus,
  ProvidersResponse,
  ProviderType,
  InstancesResponse,
} from './generated/infrastructure';

export type {
  DeploymentAttemptOutcome,
  DeploymentAttemptRecord,
  DeploymentAttemptResponse,
  DeploymentAttemptsResponse,
  DeploymentAutoVerificationRequest,
  DeploymentInferenceVerification,
  DeploymentProvisionRequest,
} from './generated/deploymentHistory';

export type {
  ApiKeyCreateRequest,
  ApiKeyCreateResponse,
  ApiKeyListResponse,
  ApiKeyRecord,
  SessionCreateRequest,
  SessionInfo,
  SessionKeyInfo,
  SessionMemberInfo,
  SessionPayload,
  SessionSwitchWorkspaceRequest,
  SessionWorkspaceInfo,
  WorkspacesResponse,
  WorkspaceRecord,
} from './generated/authAccess';

export type {
  WorkspaceInvitationAcceptRequest,
  WorkspaceInvitationAcceptResponse,
  WorkspaceInvitationAcceptedKeyRecord,
  WorkspaceInvitationCreateRequest,
  WorkspaceInvitationCreateResponse,
  WorkspaceInvitationPreview,
  WorkspaceInvitationPreviewResponse,
  WorkspaceInvitationRecord,
  WorkspaceInvitationsResponse,
  WorkspaceMemberRecord,
  WorkspaceMemberResponse,
  WorkspaceMembersResponse,
  WorkspaceMemberUpdateRequest,
  WorkspaceProviderConfigRecord,
  WorkspaceProviderConfigResponse,
  WorkspaceProviderConfigUpsertRequest,
  WorkspaceProviderConfigsResponse,
  WorkspaceQuotaRecord,
  WorkspaceQuotaResponse,
  WorkspaceQuotaUpdateRequest,
} from './generated/workspaceAdmin';

export type PlaygroundMode = 'chat' | 'agent';
export type AgentExecutionMode = 'operations' | 'research' | 'multimodal';
export type AgentAnalysisDepth = 'standard' | 'deep';

export interface AgentToolDescriptor {
  name: string;
  description: string;
  modes?: AgentExecutionMode[];
}

export interface AgentDescriptor {
  id: string;
  name: string;
  description: string;
  default_max_steps: number;
  tools: AgentToolDescriptor[];
}

export type AgentRunStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled';
export type AgentRunStepType = 'tool_call' | 'tool_result' | 'final' | 'error';

export interface AgentSource {
  title: string;
  url: string;
  domain: string;
  snippet?: string;
}

export interface AgentAttachment {
  id: string;
  workspace_id: string;
  created_by_key_id?: string;
  run_id?: string;
  file_name: string;
  mime_type: string;
  size_bytes: number;
  width?: number;
  height?: number;
  sha256: string;
  created_at: string;
}

export interface AgentRun {
  id: string;
  workspace_id: string;
  created_by_key_id?: string;
  agent_id: string;
  mode: AgentExecutionMode;
  analysis_depth: AgentAnalysisDepth;
  model: string;
  input: string;
  status: AgentRunStatus;
  max_steps: number;
  current_step: number;
  final_output?: string;
  failure_reason?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
}

export interface AgentRunStep {
  id: number;
  run_id: string;
  index: number;
  type: AgentRunStepType;
  tool_name?: string;
  payload: unknown;
  created_at: string;
}

export interface AgentRunDetail {
  run: AgentRun;
  steps: AgentRunStep[];
  attachments?: AgentAttachment[];
  sources?: AgentSource[];
}

export interface ProvisionRequest {
  name?: string;
  provider?: ProviderType;
  gpu_type: GPUType;
  provider_gpu_type_id?: string;
  gpu_count?: number;
  region?: string;
  spot_instance?: boolean;
  max_cost_hour?: number;
  models?: string[];
  selected_model_name?: string;
}

// Vault (Model Registry) Types

export interface VaultModel {
  id: string;
  name: string;
  source: string;
  source_uri: string;
  parameters: string;
  quantization: string;
  vram_required: number;
  max_context: number;
  family: string;
  tags: string[];
  metadata: Record<string, string>;
  status: 'available' | 'testing' | 'deprecated';
  created_at: string;
  updated_at: string;
}

export interface VaultStats {
  total_models: number;
  available_models: number;
  deprecated_models: number;
  model_families: number;
}

export interface VaultModelFilter {
  family?: string;
  status?: string;
  search?: string;
  quantization?: string;
  tag?: string;
  min_vram?: number;
  max_vram?: number;
}

export interface CreateVaultModelInput {
  name: string;
  source_uri: string;
  source?: string;
  parameters?: string;
  quantization?: string;
  vram_required?: number;
  max_context?: number;
  family?: string;
  tags?: string[];
  metadata?: Record<string, string>;
}
