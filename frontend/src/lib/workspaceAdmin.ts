import type {
  WorkspaceInvitationAcceptResponse,
  WorkspaceInvitationCreateResponse,
  WorkspaceInvitationPreview,
  WorkspaceInvitationPreviewResponse,
  WorkspaceInvitationRecord,
  WorkspaceInvitationsResponse,
  WorkspaceInvitationAcceptedKeyRecord,
  WorkspaceMemberRecord,
  WorkspaceMemberResponse,
  WorkspaceMembersResponse,
  WorkspaceProviderConfigRecord,
  WorkspaceProviderConfigResponse,
  WorkspaceProviderConfigsResponse,
  WorkspaceQuotaRecord,
  WorkspaceQuotaResponse,
} from '../types';

type JSONRecord = Record<string, unknown>;

function expectRecord(value: unknown, label: string): JSONRecord {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`Invalid ${label}`);
  }
  return value as JSONRecord;
}

function expectArray(value: unknown, label: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new Error(`Invalid ${label}`);
  }
  return value;
}

function expectString(record: JSONRecord, key: string, label: string): string {
  const value = record[key];
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalString(record: JSONRecord, key: string, label: string): string | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'string') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalNullableNumber(record: JSONRecord, key: string, label: string): number | null | undefined {
  const value = record[key];
  if (value === null) {
    return null;
  }
  if (value == null) {
    return undefined;
  }
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function expectBoolean(record: JSONRecord, key: string, label: string): boolean {
  const value = record[key];
  if (typeof value !== 'boolean') {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function optionalStringRecord(record: JSONRecord, key: string, label: string): Record<string, string> | undefined {
  const value = record[key];
  if (value == null) {
    return undefined;
  }
  const parsed = expectRecord(value, `${label}.${key}`);
  const out: Record<string, string> = {};
  for (const [entryKey, entryValue] of Object.entries(parsed)) {
    if (typeof entryValue !== 'string') {
      throw new Error(`Invalid ${label}.${key}.${entryKey}`);
    }
    out[entryKey] = entryValue;
  }
  return out;
}

function expectNumber(record: JSONRecord, key: string, label: string): number {
  const value = record[key];
  if (typeof value !== 'number' || Number.isNaN(value)) {
    throw new Error(`Invalid ${label}.${key}`);
  }
  return value;
}

function parseWorkspaceQuotaRecord(value: unknown, label: string): WorkspaceQuotaRecord {
  const record = expectRecord(value, label);
  return {
    workspace_id: expectString(record, 'workspace_id', label),
    monthly_request_limit: optionalNullableNumber(record, 'monthly_request_limit', label),
    monthly_token_limit: optionalNullableNumber(record, 'monthly_token_limit', label),
    enforce_hard_limits: expectBoolean(record, 'enforce_hard_limits', label),
    updated_at: expectString(record, 'updated_at', label),
  };
}

function parseWorkspaceProviderConfigRecord(value: unknown, label: string): WorkspaceProviderConfigRecord {
  const record = expectRecord(value, label);
  return {
    workspace_id: expectString(record, 'workspace_id', label),
    provider: expectString(record, 'provider', label) as WorkspaceProviderConfigRecord['provider'],
    configured: expectBoolean(record, 'configured', label),
    endpoint: optionalString(record, 'endpoint', label),
    options: optionalStringRecord(record, 'options', label),
    created_at: expectString(record, 'created_at', label),
    updated_at: expectString(record, 'updated_at', label),
  };
}

function parseWorkspaceMemberRecord(value: unknown, label: string): WorkspaceMemberRecord {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    workspace_id: expectString(record, 'workspace_id', label),
    email: expectString(record, 'email', label),
    display_name: expectString(record, 'display_name', label),
    role: expectString(record, 'role', label),
    status: expectString(record, 'status', label),
    created_at: expectString(record, 'created_at', label),
  };
}

function parseWorkspaceInvitationRecord(value: unknown, label: string): WorkspaceInvitationRecord {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    workspace_id: expectString(record, 'workspace_id', label),
    email: expectString(record, 'email', label),
    display_name: expectString(record, 'display_name', label),
    role: expectString(record, 'role', label),
    invited_by_key_id: expectString(record, 'invited_by_key_id', label),
    created_at: expectString(record, 'created_at', label),
    expires_at: expectString(record, 'expires_at', label),
    status: expectString(record, 'status', label),
  };
}

function parseWorkspaceInvitationPreview(value: unknown, label: string): WorkspaceInvitationPreview {
  const record = expectRecord(value, label);
  return {
    workspace_id: expectString(record, 'workspace_id', label),
    workspace_slug: expectString(record, 'workspace_slug', label),
    workspace_name: expectString(record, 'workspace_name', label),
    email: expectString(record, 'email', label),
    display_name: expectString(record, 'display_name', label),
    role: expectString(record, 'role', label),
    expires_at: expectString(record, 'expires_at', label),
    status: expectString(record, 'status', label),
  };
}

function parseWorkspaceInvitationAcceptedKeyRecord(
  value: unknown,
  label: string,
): WorkspaceInvitationAcceptedKeyRecord {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    workspace_id: expectString(record, 'workspace_id', label),
    workspace_slug: expectString(record, 'workspace_slug', label),
    workspace_name: expectString(record, 'workspace_name', label),
    key_prefix: expectString(record, 'key_prefix', label),
    name: expectString(record, 'name', label),
    role: expectString(record, 'role', label),
    principal_type: expectString(record, 'principal_type', label),
    membership_id: optionalString(record, 'membership_id', label),
    member_email: optionalString(record, 'member_email', label),
    member_name: optionalString(record, 'member_name', label),
    created_at: expectString(record, 'created_at', label),
    last_used: optionalString(record, 'last_used', label),
    status: expectString(record, 'status', label),
  };
}

export function parseWorkspaceQuotaResponse(value: unknown): WorkspaceQuotaResponse {
  const record = expectRecord(value, 'workspace quota response');
  return {
    quota: parseWorkspaceQuotaRecord(record.quota, 'workspace quota response.quota'),
  };
}

export function parseWorkspaceProviderConfigsResponse(value: unknown): WorkspaceProviderConfigsResponse {
  const record = expectRecord(value, 'workspace provider configs response');
  const items = expectArray(record.providers, 'workspace provider configs response.providers');
  return {
    providers: items.map((item, index) => (
      parseWorkspaceProviderConfigRecord(item, `workspace provider configs response.providers[${index}]`)
    )),
    total: expectNumber(record, 'total', 'workspace provider configs response'),
  };
}

export function parseWorkspaceProviderConfigResponse(value: unknown): WorkspaceProviderConfigResponse {
  const record = expectRecord(value, 'workspace provider config response');
  return {
    provider: parseWorkspaceProviderConfigRecord(record.provider, 'workspace provider config response.provider'),
  };
}

export function parseWorkspaceMembersResponse(value: unknown): WorkspaceMembersResponse {
  const record = expectRecord(value, 'workspace members response');
  const items = expectArray(record.members, 'workspace members response.members');
  return {
    members: items.map((item, index) => (
      parseWorkspaceMemberRecord(item, `workspace members response.members[${index}]`)
    )),
    total: expectNumber(record, 'total', 'workspace members response'),
  };
}

export function parseWorkspaceMemberResponse(value: unknown): WorkspaceMemberResponse {
  const record = expectRecord(value, 'workspace member response');
  return {
    member: parseWorkspaceMemberRecord(record.member, 'workspace member response.member'),
  };
}

export function parseWorkspaceInvitationsResponse(value: unknown): WorkspaceInvitationsResponse {
  const record = expectRecord(value, 'workspace invitations response');
  const items = expectArray(record.invitations, 'workspace invitations response.invitations');
  return {
    invitations: items.map((item, index) => (
      parseWorkspaceInvitationRecord(item, `workspace invitations response.invitations[${index}]`)
    )),
    total: expectNumber(record, 'total', 'workspace invitations response'),
  };
}

export function parseWorkspaceInvitationPreviewResponse(value: unknown): WorkspaceInvitationPreviewResponse {
  const record = expectRecord(value, 'workspace invitation preview response');
  return {
    invitation: parseWorkspaceInvitationPreview(
      record.invitation,
      'workspace invitation preview response.invitation',
    ),
  };
}

export function parseWorkspaceInvitationCreateResponse(value: unknown): WorkspaceInvitationCreateResponse {
  const record = expectRecord(value, 'workspace invitation create response');
  return {
    invitation_token: expectString(record, 'invitation_token', 'workspace invitation create response'),
    invitation: parseWorkspaceInvitationRecord(
      record.invitation,
      'workspace invitation create response.invitation',
    ),
  };
}

export function parseWorkspaceInvitationAcceptResponse(value: unknown): WorkspaceInvitationAcceptResponse {
  const record = expectRecord(value, 'workspace invitation accept response');
  return {
    membership: parseWorkspaceMemberRecord(
      record.membership,
      'workspace invitation accept response.membership',
    ),
    key: expectString(record, 'key', 'workspace invitation accept response'),
    record: parseWorkspaceInvitationAcceptedKeyRecord(
      record.record,
      'workspace invitation accept response.record',
    ),
  };
}
