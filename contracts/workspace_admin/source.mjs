export const generatedTypes = `import type { ProviderType } from './infrastructure';

export interface WorkspaceQuotaRecord {
  workspace_id: string;
  monthly_request_limit?: number | null;
  monthly_token_limit?: number | null;
  enforce_hard_limits: boolean;
  updated_at: string;
}

export interface WorkspaceQuotaResponse {
  quota: WorkspaceQuotaRecord;
}

export interface WorkspaceQuotaUpdateRequest {
  monthly_request_limit?: number | null;
  monthly_token_limit?: number | null;
  enforce_hard_limits?: boolean;
}

export interface WorkspaceProviderConfigRecord {
  workspace_id: string;
  provider: ProviderType;
  configured: boolean;
  endpoint?: string;
  options?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceProviderConfigsResponse {
  providers: WorkspaceProviderConfigRecord[];
  total: number;
}

export interface WorkspaceProviderConfigResponse {
  provider: WorkspaceProviderConfigRecord;
}

export interface WorkspaceProviderConfigUpsertRequest {
  api_key: string;
  api_secret?: string;
  endpoint?: string;
  options?: Record<string, string>;
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

export interface WorkspaceMembersResponse {
  members: WorkspaceMemberRecord[];
  total: number;
}

export interface WorkspaceMemberResponse {
  member: WorkspaceMemberRecord;
}

export interface WorkspaceMemberUpdateRequest {
  role: string;
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

export interface WorkspaceInvitationsResponse {
  invitations: WorkspaceInvitationRecord[];
  total: number;
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

export interface WorkspaceInvitationPreviewResponse {
  invitation: WorkspaceInvitationPreview;
}

export interface WorkspaceInvitationCreateRequest {
  email: string;
  display_name?: string;
  role?: string;
}

export interface WorkspaceInvitationCreateResponse {
  invitation_token: string;
  invitation: WorkspaceInvitationRecord;
}

export interface WorkspaceInvitationAcceptRequest {
  invitation_token: string;
  display_name?: string;
}

export interface WorkspaceInvitationAcceptedKeyRecord {
  id: string;
  workspace_id: string;
  workspace_slug: string;
  workspace_name: string;
  key_prefix: string;
  name: string;
  role: string;
  principal_type: string;
  membership_id?: string;
  member_email?: string;
  member_name?: string;
  created_at: string;
  last_used?: string;
  status: string;
}

export interface WorkspaceInvitationAcceptResponse {
  membership: WorkspaceMemberRecord;
  key: string;
  record: WorkspaceInvitationAcceptedKeyRecord;
}
`;

const quotaRecord = {
  workspace_id: 'ws_alpha',
  monthly_request_limit: 250,
  monthly_token_limit: 5000,
  enforce_hard_limits: false,
  updated_at: '2026-04-10T00:00:00Z',
};

const providerConfigRecord = {
  workspace_id: 'ws_alpha',
  provider: 'runpod',
  configured: true,
  endpoint: 'https://api.runpod.io/graphql',
  options: {
    location: 'us-east-1',
    note: 'primary',
  },
  created_at: '2026-04-10T00:00:00Z',
  updated_at: '2026-04-10T00:00:00Z',
};

const workspaceMemberRecord = {
  id: 'mbr_fixture_member',
  workspace_id: 'ws_alpha',
  email: 'member@example.com',
  display_name: 'Fixture Member',
  role: 'operator',
  status: 'active',
  created_at: '2026-04-10T00:00:00Z',
};

const workspaceInvitationRecord = {
  id: 'inv_fixture_invite',
  workspace_id: 'ws_alpha',
  email: 'invitee@example.com',
  display_name: 'Invitee Example',
  role: 'developer',
  invited_by_key_id: 'key_fixture_inviter',
  created_at: '2026-04-10T00:00:00Z',
  expires_at: '2026-04-17T00:00:00Z',
  status: 'pending',
};

const workspaceInvitationPreview = {
  workspace_id: 'ws_alpha',
  workspace_slug: 'fixture-team',
  workspace_name: 'Fixture Team',
  email: 'invitee@example.com',
  display_name: 'Invitee Example',
  role: 'developer',
  expires_at: '2026-04-17T00:00:00Z',
  status: 'pending',
};

const acceptedWorkspaceMemberRecord = {
  id: 'mbr_fixture_accepted',
  workspace_id: 'ws_alpha',
  email: 'invitee@example.com',
  display_name: 'Joined Example',
  role: 'developer',
  status: 'active',
  created_at: '2026-04-10T00:00:00Z',
};

const acceptedWorkspaceKeyRecord = {
  id: 'key_fixture_accepted',
  workspace_id: 'ws_alpha',
  workspace_slug: 'fixture-team',
  workspace_name: 'Fixture Team',
  key_prefix: 'inf_fixture...',
  name: 'Joined Example',
  role: 'developer',
  principal_type: 'human',
  membership_id: 'mbr_fixture_accepted',
  member_email: 'invitee@example.com',
  member_name: 'Joined Example',
  created_at: '2026-04-10T00:00:00Z',
  status: 'active',
};

export const fixtures = {
  'auth_error_cannot_assign_role.json': {
    error: {
      type: 'authorization_error',
      message: 'You cannot assign that role.',
    },
  },
  'auth_error_invalid_invitation.json': {
    error: {
      type: 'invalid_request_error',
      message: 'invalid invitation',
    },
  },
  'auth_error_method_not_allowed.json': {
    error: {
      type: 'invalid_request_error',
      message: 'Method not allowed',
    },
  },
  'auth_error_missing_member_role.json': {
    error: {
      type: 'invalid_request_error',
      message: 'role is required',
    },
  },
  'auth_error_missing_preview_token.json': {
    error: {
      type: 'invalid_request_error',
      message: 'token is required',
    },
  },
  'auth_error_not_found.json': {
    error: {
      type: 'not_found_error',
      message: 'Not found',
    },
  },
  'auth_error_unknown_provider.json': {
    error: {
      type: 'invalid_request_error',
      message: 'Unknown provider',
    },
  },
  'auth_error_usage_access_required.json': {
    error: {
      type: 'authorization_error',
      message: 'Usage access required.',
    },
  },
  'auth_error_workspace_path_required.json': {
    error: {
      type: 'invalid_request_error',
      message: 'Workspace path required',
    },
  },
  'workspace_invitation_accept_request.json': {
    invitation_token: 'invite_fixture_token',
    display_name: 'Joined Example',
  },
  'workspace_invitation_accept_response.json': {
    membership: acceptedWorkspaceMemberRecord,
    key: 'inf_fixture_secret',
    record: acceptedWorkspaceKeyRecord,
  },
  'workspace_invitation_create_request.json': {
    email: 'invitee@example.com',
    display_name: 'Invitee Example',
    role: 'developer',
  },
  'workspace_invitation_create_response.json': {
    invitation_token: 'invite_fixture_token',
    invitation: workspaceInvitationRecord,
  },
  'workspace_invitation_preview_response.json': {
    invitation: workspaceInvitationPreview,
  },
  'workspace_invitations_list_response.json': {
    invitations: [workspaceInvitationRecord],
    total: 1,
  },
  'workspace_member_response.json': {
    member: workspaceMemberRecord,
  },
  'workspace_member_update_request.json': {
    role: 'operator',
  },
  'workspace_members_list_response.json': {
    members: [workspaceMemberRecord],
    total: 1,
  },
  'workspace_provider_config_response.json': {
    provider: providerConfigRecord,
  },
  'workspace_provider_config_upsert_request.json': {
    api_key: 'rp_key',
    api_secret: 'rp_secret',
    endpoint: 'https://api.runpod.io/graphql',
    options: {
      location: 'us-east-1',
      note: 'primary',
    },
  },
  'workspace_provider_configs_list_response.json': {
    providers: [providerConfigRecord],
    total: 1,
  },
  'workspace_quota_response.json': {
    quota: quotaRecord,
  },
  'workspace_quota_update_request.json': {
    monthly_request_limit: 250,
    monthly_token_limit: 5000,
    enforce_hard_limits: false,
  },
};
