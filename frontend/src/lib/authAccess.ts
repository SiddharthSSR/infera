import type {
  ApiKeyCreateResponse,
  ApiKeyListResponse,
  ApiKeyRecord,
  SessionInfo,
  WorkspaceRecord,
  WorkspacesResponse,
} from '../types';
import type { AuthErrorType } from '../types/generated/authAccess';

type JSONRecord = Record<string, unknown>;

export type SessionCreateErrorCode =
  | 'invalid_credentials'
  | 'service_account_forbidden'
  | 'dashboard_access_forbidden'
  | 'gateway_response_error';

interface ParsedAuthError {
  type?: AuthErrorType;
  message?: string;
}

export class SessionCreateError extends Error {
  constructor(
    readonly code: SessionCreateErrorCode,
    readonly status: number,
    readonly errorType?: AuthErrorType,
  ) {
    super('Dashboard session request rejected');
    this.name = 'SessionCreateError';
  }
}

export function parseAuthErrorResponse(value: unknown): ParsedAuthError {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) {
    return {};
  }

  const record = value as JSONRecord;
  const nestedError = record.error;
  if (nestedError != null && typeof nestedError === 'object' && !Array.isArray(nestedError)) {
    const error = nestedError as JSONRecord;
    return {
      type: isAuthErrorType(error.type) ? error.type : undefined,
      message: typeof error.message === 'string' ? error.message : undefined,
    };
  }

  return {
    message:
      typeof record.message === 'string'
        ? record.message
        : typeof nestedError === 'string'
          ? nestedError
          : undefined,
  };
}

export function classifySessionCreateError(status: number, error: ParsedAuthError): SessionCreateError {
  if (status === 401) {
    return new SessionCreateError('invalid_credentials', status, error.type);
  }
  if (status === 403) {
    const message = error.message?.toLowerCase() ?? '';
    const code = message.includes('service account')
      ? 'service_account_forbidden'
      : 'dashboard_access_forbidden';
    return new SessionCreateError(code, status, error.type);
  }
  return new SessionCreateError('gateway_response_error', status, error.type);
}

export function getSessionSignInGuidance(error: unknown): string {
  if (error instanceof SessionCreateError) {
    switch (error.code) {
      case 'invalid_credentials':
        return 'Invalid or revoked human dashboard key. Check your key and try again.';
      case 'service_account_forbidden':
        return 'Dashboard access requires a human key. Service-account keys are for API and automation use.';
      case 'dashboard_access_forbidden':
        return 'Dashboard access required. Use an active human key with dashboard access; inference-only keys cannot sign in.';
      case 'gateway_response_error':
        return 'Sign-in is temporarily unavailable. Try again in a moment.';
    }
  }

  if (isNetworkError(error)) {
    return 'Could not connect to the gateway. Check its availability and try again.';
  }
  return 'Could not complete sign-in. Try again.';
}

export function getInvitationRecoveryGuidance(error: unknown): string {
  const message = error instanceof Error ? error.message.toLowerCase() : '';

  if (message.includes('expired')) {
    return 'Ask the workspace admin for a new invitation. Expired invitation tokens cannot be reused.';
  }
  if (
    message.includes('invalid')
    || message.includes('revoked')
    || message.includes('already accepted')
    || message.includes('not found')
  ) {
    return 'Confirm that you pasted the complete token. If it still fails, ask the workspace admin for a new invitation.';
  }
  if (message.includes('network') || message.includes('failed to fetch')) {
    return 'Check your connection and retry. Your invitation token remains in the field.';
  }
  return 'Retry once. If the problem continues, ask the workspace admin to confirm the invitation is still active.';
}

function isAuthErrorType(value: unknown): value is AuthErrorType {
  return value === 'authentication_error'
    || value === 'authorization_error'
    || value === 'invalid_request_error'
    || value === 'not_found_error'
    || value === 'internal_error';
}

function isNetworkError(error: unknown): boolean {
  if (error instanceof TypeError) {
    return true;
  }
  if (!(error instanceof Error)) {
    return false;
  }
  const message = error.message.toLowerCase();
  return message.includes('network')
    || message.includes('failed to fetch')
    || message.includes('load failed');
}

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

function parseApiKeyRecord(value: unknown, label: string): ApiKeyRecord {
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

function parseWorkspaceRecord(value: unknown, label: string): WorkspaceRecord {
  const record = expectRecord(value, label);
  return {
    id: expectString(record, 'id', label),
    slug: expectString(record, 'slug', label),
    name: expectString(record, 'name', label),
    created_at: expectString(record, 'created_at', label),
    status: expectString(record, 'status', label),
  };
}

export function parseApiKeysResponse(value: unknown): ApiKeyListResponse {
  const record = expectRecord(value, 'api keys response');
  const items = expectArray(record.keys, 'api keys response.keys');
  const total = record.total;
  if (typeof total !== 'number' || Number.isNaN(total)) {
    throw new Error('Invalid api keys response.total');
  }
  return {
    keys: items.map((item, index) => parseApiKeyRecord(item, `api keys response.keys[${index}]`)),
    total,
  };
}

export function parseApiKeyCreateResponse(value: unknown): ApiKeyCreateResponse {
  const record = expectRecord(value, 'api key create response');
  return {
    key: expectString(record, 'key', 'api key create response'),
    record: parseApiKeyRecord(record.record, 'api key create response.record'),
  };
}

export function parseWorkspacesResponse(value: unknown): WorkspacesResponse {
  const record = expectRecord(value, 'workspaces response');
  const items = expectArray(record.workspaces, 'workspaces response.workspaces');
  const total = record.total;
  if (typeof total !== 'number' || Number.isNaN(total)) {
    throw new Error('Invalid workspaces response.total');
  }
  return {
    workspaces: items.map((item, index) => parseWorkspaceRecord(item, `workspaces response.workspaces[${index}]`)),
    total,
  };
}

export function parseSessionResponse(value: unknown): SessionInfo {
  const record = expectRecord(value, 'session response');
  const session = expectRecord(record.session, 'session response.session');
  const key = expectRecord(record.key, 'session response.key');
  const workspace = expectRecord(record.workspace, 'session response.workspace');
  const rawMember = record.member;

  return {
    session: {
      id: expectString(session, 'id', 'session response.session'),
      expires_at: expectString(session, 'expires_at', 'session response.session'),
    },
    key: {
      id: expectString(key, 'id', 'session response.key'),
      key_prefix: expectString(key, 'key_prefix', 'session response.key'),
      name: expectString(key, 'name', 'session response.key'),
      role: expectString(key, 'role', 'session response.key'),
      principal_type: expectString(key, 'principal_type', 'session response.key'),
      workspace_id: expectString(key, 'workspace_id', 'session response.key'),
      workspace_slug: expectString(key, 'workspace_slug', 'session response.key'),
      workspace_name: expectString(key, 'workspace_name', 'session response.key'),
    },
    workspace: {
      id: expectString(workspace, 'id', 'session response.workspace'),
      slug: expectString(workspace, 'slug', 'session response.workspace'),
      name: expectString(workspace, 'name', 'session response.workspace'),
    },
    member: rawMember == null
      ? null
      : {
          id: expectString(expectRecord(rawMember, 'session response.member'), 'id', 'session response.member'),
          email: optionalString(expectRecord(rawMember, 'session response.member'), 'email', 'session response.member'),
          display_name: optionalString(expectRecord(rawMember, 'session response.member'), 'display_name', 'session response.member'),
        },
  };
}
