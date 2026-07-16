export const generatedTypes = `
export interface SessionPayload {
  id: string;
  expires_at: string;
}

export type AuthErrorType =
  | 'authentication_error'
  | 'authorization_error'
  | 'invalid_request_error'
  | 'not_found_error'
  | 'internal_error';

export interface AuthError {
  type: AuthErrorType;
  message: string;
}

export interface AuthErrorResponse {
  error: AuthError;
}

export interface SessionKeyInfo {
  id: string;
  key_prefix: string;
  name: string;
  role: string;
  principal_type: string;
  workspace_id: string;
  workspace_slug: string;
  workspace_name: string;
}

export interface SessionWorkspaceInfo {
  id: string;
  slug: string;
  name: string;
}

export interface WorkspaceRecord {
  id: string;
  slug: string;
  name: string;
  created_at: string;
  status: string;
}

export interface WorkspacesResponse {
  workspaces: WorkspaceRecord[];
  total: number;
}

export interface SessionMemberInfo {
  id: string;
  email?: string;
  display_name?: string;
}

export interface SessionInfo {
  session: SessionPayload;
  key: SessionKeyInfo;
  workspace: SessionWorkspaceInfo;
  member: SessionMemberInfo | null;
}

export interface SessionCreateRequest {
  api_key: string;
}

export interface SessionSwitchWorkspaceRequest {
  workspace_id: string;
}

export interface ApiKeyRecord {
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

export interface ApiKeyListResponse {
  keys: ApiKeyRecord[];
  total: number;
}

export interface ApiKeyCreateRequest {
  name: string;
  role?: string;
  principal_type?: string;
  workspace_id?: string;
}

export interface ApiKeyCreateResponse {
  key: string;
  record: ApiKeyRecord;
}
`;

const workspaceFixture = {
  id: 'ws_alpha',
  slug: 'fixture-team',
  name: 'Fixture Team',
};

const betaWorkspaceFixture = {
  id: 'ws_beta',
  slug: 'beta-team',
  name: 'Beta Team',
};

const workspaceRecord = {
  ...workspaceFixture,
  created_at: '2026-04-10T00:00:00Z',
  status: 'active',
};

const betaWorkspaceRecord = {
  ...betaWorkspaceFixture,
  created_at: '2026-04-11T00:00:00Z',
  status: 'active',
};

const adminKeyRecord = {
  id: 'key_fixture_admin',
  workspace_id: workspaceFixture.id,
  workspace_slug: workspaceFixture.slug,
  workspace_name: workspaceFixture.name,
  key_prefix: 'inf_fixture_admin...',
  name: 'Fixture Admin',
  role: 'admin',
  principal_type: 'human',
  created_at: '2026-04-10T00:00:00Z',
  status: 'active',
};

const serviceAccountKeyRecord = {
  id: 'key_fixture_service_account',
  workspace_id: workspaceFixture.id,
  workspace_slug: workspaceFixture.slug,
  workspace_name: workspaceFixture.name,
  key_prefix: 'inf_fixture_bot...',
  name: 'CI Bot',
  role: 'operator',
  principal_type: 'service_account',
  created_at: '2026-04-10T00:05:00Z',
  status: 'active',
};

export const fixtures = {
  'api_key_create_request.json': {
    name: 'CI Bot',
    role: 'operator',
    principal_type: 'service_account',
  },
  'api_key_create_response.json': {
    key: 'inf_fixture_created_key',
    record: serviceAccountKeyRecord,
  },
  'api_keys_list_response.json': {
    keys: [serviceAccountKeyRecord, adminKeyRecord],
    total: 2,
  },
  'auth_error_invalid_api_key.json': {
    error: {
      type: 'authentication_error',
      message: 'Invalid or revoked API key.',
    },
  },
  'auth_error_key_management_access_required.json': {
    error: {
      type: 'authorization_error',
      message: 'Key management access required.',
    },
  },
  'auth_error_method_not_allowed.json': {
    error: {
      type: 'invalid_request_error',
      message: 'Method not allowed',
    },
  },
  'auth_error_missing_session_cookie.json': {
    error: {
      type: 'authentication_error',
      message: 'No session cookie.',
    },
  },
  'auth_error_missing_workspace_id.json': {
    error: {
      type: 'invalid_request_error',
      message: 'workspace_id is required',
    },
  },
  'auth_error_service_account_session_forbidden.json': {
    error: {
      type: 'authorization_error',
      message: 'Service accounts cannot create dashboard sessions.',
    },
  },
  'auth_error_workspace_access_required.json': {
    error: {
      type: 'authorization_error',
      message: 'Workspace access required.',
    },
  },
  'session_create_request.json': {
    api_key: 'inf_fixture_admin_key',
  },
  'session_response.json': {
    session: {
      id: 'sess_fixture_admin',
      expires_at: '2026-04-12T00:00:00Z',
    },
    key: {
      id: adminKeyRecord.id,
      key_prefix: adminKeyRecord.key_prefix,
      name: adminKeyRecord.name,
      role: adminKeyRecord.role,
      principal_type: adminKeyRecord.principal_type,
      workspace_id: adminKeyRecord.workspace_id,
      workspace_slug: adminKeyRecord.workspace_slug,
      workspace_name: adminKeyRecord.workspace_name,
    },
    workspace: workspaceFixture,
    member: null,
  },
  'session_response_member.json': {
    session: {
      id: 'sess_fixture_member',
      expires_at: '2026-04-12T00:00:00Z',
    },
    key: {
      id: 'key_fixture_member',
      key_prefix: 'inf_fixture_member...',
      name: 'Joined Member',
      role: 'operator',
      principal_type: 'human',
      workspace_id: workspaceFixture.id,
      workspace_slug: workspaceFixture.slug,
      workspace_name: workspaceFixture.name,
    },
    workspace: workspaceFixture,
    member: {
      id: 'mbr_fixture_member',
      email: 'member@example.com',
      display_name: 'Joined Member',
    },
  },
  'session_response_switched_workspace.json': {
    session: {
      id: 'sess_fixture_member',
      expires_at: '2026-04-12T00:00:00Z',
    },
    key: {
      id: 'key_fixture_switched',
      key_prefix: 'inf_fixture_switched...',
      name: 'Joined Member',
      role: 'developer',
      principal_type: 'human',
      workspace_id: betaWorkspaceFixture.id,
      workspace_slug: betaWorkspaceFixture.slug,
      workspace_name: betaWorkspaceFixture.name,
    },
    workspace: betaWorkspaceFixture,
    member: {
      id: 'mbr_fixture_member_beta',
      email: 'member@example.com',
      display_name: 'Joined Member',
    },
  },
  'session_switch_workspace_request.json': {
    workspace_id: betaWorkspaceFixture.id,
  },
  'workspaces_list_response.json': {
    workspaces: [workspaceRecord, betaWorkspaceRecord],
    total: 2,
  },
};
