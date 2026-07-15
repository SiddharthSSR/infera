// Compatibility barrel for legacy imports and contract tests.
// New production code should import the narrower feature clients directly.
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
  WorkspaceProviderConfigUpsertRequest,
  WorkspaceQuotaRecord,
  WorkspaceQuotaUpdateRequest,
} from '../types';
export type { AuditUsageResponse, AuditUsageRow } from './apiCore';
export type { AgentsListResponse, CreateAgentRunRequest } from './agentsClient';
export type { StreamChatCompletionOptions } from './chatClient';
export {
  acceptWorkspaceInvitation,
  createApiKey,
  createSession,
  destroySession,
  fetchApiKeys,
  fetchInvitationPreview,
  fetchWorkspaces,
  getSession,
  revokeApiKey,
  switchSessionWorkspace,
} from './authAccessClient';
export {
  createWorkspaceInvite,
  deleteWorkspaceProviderConfig,
  fetchAuditUsage,
  fetchWorkspaceInvites,
  fetchWorkspaceMembers,
  fetchWorkspaceProviderConfigs,
  fetchWorkspaceQuota,
  removeWorkspaceMember,
  revokeWorkspaceInvite,
  updateWorkspaceMember,
  updateWorkspaceQuota,
  upsertWorkspaceProviderConfig,
} from './workspaceAdminClient';
export {
  fetchCosts,
  fetchInstances,
  fetchOfferings,
  fetchProviders,
  provisionInstance,
  startInstance,
  stopInstance,
  terminateInstance,
} from './infrastructureClient';
export {
  fetchWorkers,
  fetchModels,
  fetchStats,
} from './runtimeClient';
export {
  sendChatCompletion,
  streamChatCompletion,
} from './chatClient';
export {
  fetchDeploymentAttempts,
  updateDeploymentVerification,
  markDeploymentAutoVerificationRequested,
} from './deploymentsClient';
export {
  fetchVaultModels,
  fetchVaultStats,
  fetchVaultFamilies,
  registerVaultModel,
  deleteVaultModel,
} from './vaultClient';
export {
  fetchAgents,
  uploadAgentAttachment,
  createAgentRun,
  fetchAgentRunDetail,
  cancelAgentRun,
} from './agentsClient';
