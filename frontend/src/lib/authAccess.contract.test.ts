/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  createApiKey,
  createSession,
  fetchApiKeys,
  fetchWorkspaces,
  getSession,
  switchSessionWorkspace,
} from './api';
import {
  getInvitationRecoveryGuidance,
  parseApiKeyCreateResponse,
  parseApiKeysResponse,
  parseSessionResponse,
  parseWorkspacesResponse,
} from './authAccess';
import type {
  ApiKeyCreateRequest,
  SessionCreateRequest,
  SessionSwitchWorkspaceRequest,
} from '../types';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/auth_access');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

describe('auth access contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared session fixtures', () => {
    const adminPayload = loadJSONFixture('session_response.json');
    const memberPayload = loadJSONFixture('session_response_member.json');

    const adminSession = parseSessionResponse(adminPayload);
    const memberSession = parseSessionResponse(memberPayload);

    expect(adminSession.workspace.slug).toBe('fixture-team');
    expect(adminSession.member).toBeNull();
    expect(memberSession.member?.email).toBe('member@example.com');
    expect(memberSession.key.role).toBe('operator');
  });

  it('parses the shared api key fixtures', () => {
    const listPayload = loadJSONFixture('api_keys_list_response.json');
    const createPayload = loadJSONFixture('api_key_create_response.json');

    const list = parseApiKeysResponse(listPayload);
    const created = parseApiKeyCreateResponse(createPayload);

    expect(list.total).toBe(2);
    expect(list.keys[0]?.principal_type).toBe('service_account');
    expect(created.record.name).toBe('CI Bot');
    expect(created.record.workspace_slug).toBe('fixture-team');
  });

  it('parses the shared accessible workspaces fixture', () => {
    const payload = loadJSONFixture('workspaces_list_response.json');

    const parsed = parseWorkspacesResponse(payload);

    expect(parsed.total).toBe(2);
    expect(parsed.workspaces[0]?.slug).toBe('fixture-team');
    expect(parsed.workspaces[1]?.status).toBe('active');
  });

  it('auth API functions surface the shared auth error fixtures', async () => {
    mockFetch
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_service_account_session_forbidden.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_workspace_access_required.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_missing_workspace_id.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_key_management_access_required.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 405,
        statusText: 'Method Not Allowed',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_method_not_allowed.json'),
      });

    await expect(createSession('inf_fixture_service')).rejects.toThrow(
      'Service accounts cannot create dashboard sessions.',
    );
    await expect(fetchWorkspaces()).rejects.toThrow(
      'Failed to fetch workspaces (403 Forbidden): Workspace access required.',
    );
    await expect(switchSessionWorkspace('')).rejects.toThrow(
      'Failed to switch workspace (400 Bad Request): workspace_id is required',
    );
    await expect(fetchApiKeys()).rejects.toThrow(
      'Failed to fetch API keys (403 Forbidden): Key management access required.',
    );
    await expect(createApiKey('CI Bot', 'operator', 'service_account')).rejects.toThrow(
      'Failed to create key (405 Method Not Allowed): Method not allowed',
    );
  });

  it('auth API functions use the shared request and response fixtures', async () => {
    const sessionCreateRequest = loadJSONFixture('session_create_request.json') as SessionCreateRequest;
    const adminSessionResponse = loadJSONFixture('session_response.json');
    const memberSessionResponse = loadJSONFixture('session_response_member.json');
    const switchedSessionResponse = loadJSONFixture('session_response_switched_workspace.json');
    const sessionSwitchRequest = loadJSONFixture('session_switch_workspace_request.json') as SessionSwitchWorkspaceRequest;
    const workspacesListResponse = loadJSONFixture('workspaces_list_response.json');
    const keysListResponse = loadJSONFixture('api_keys_list_response.json');
    const apiKeyCreateRequest = loadJSONFixture('api_key_create_request.json') as ApiKeyCreateRequest;
    const apiKeyCreateResponse = loadJSONFixture('api_key_create_response.json');

    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => adminSessionResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => memberSessionResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => switchedSessionResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => workspacesListResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => keysListResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => apiKeyCreateResponse });

    await expect(createSession(sessionCreateRequest.api_key)).resolves.toEqual(
      expect.objectContaining({
        workspace: expect.objectContaining({ slug: 'fixture-team' }),
        member: null,
      }),
    );
    await expect(getSession()).resolves.toEqual(
      expect.objectContaining({
        member: expect.objectContaining({ email: 'member@example.com' }),
      }),
    );
    await expect(switchSessionWorkspace(sessionSwitchRequest.workspace_id)).resolves.toEqual(
      expect.objectContaining({
        workspace: expect.objectContaining({ slug: 'beta-team' }),
        key: expect.objectContaining({ role: 'developer' }),
      }),
    );
    await expect(fetchWorkspaces()).resolves.toEqual(
      expect.arrayContaining([
        expect.objectContaining({ id: 'ws_alpha', slug: 'fixture-team' }),
        expect.objectContaining({ id: 'ws_beta', slug: 'beta-team' }),
      ]),
    );
    await expect(fetchApiKeys()).resolves.toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: 'CI Bot', principal_type: 'service_account' }),
        expect.objectContaining({ name: 'Fixture Admin', role: 'admin' }),
      ]),
    );
    await expect(
      createApiKey(
        apiKeyCreateRequest.name,
        apiKeyCreateRequest.role,
        apiKeyCreateRequest.principal_type,
      ),
    ).resolves.toEqual(expect.objectContaining({
      key: 'inf_fixture_created_key',
      record: expect.objectContaining({ name: 'CI Bot' }),
    }));

    expect(JSON.parse(String(mockFetch.mock.calls[0]?.[1]?.body))).toEqual(sessionCreateRequest);
    expect(JSON.parse(String(mockFetch.mock.calls[2]?.[1]?.body))).toEqual(sessionSwitchRequest);
    expect(JSON.parse(String(mockFetch.mock.calls[5]?.[1]?.body))).toEqual(apiKeyCreateRequest);
  });
});

describe('invitation recovery guidance', () => {
  it('explains how to recover from an expired token', () => {
    expect(getInvitationRecoveryGuidance(new Error('Invitation expired'))).toBe(
      'Ask the workspace admin for a new invitation. Expired invitation tokens cannot be reused.',
    );
  });

  it('preserves retry guidance for network failures', () => {
    expect(getInvitationRecoveryGuidance(new Error('Failed to fetch'))).toContain(
      'Your invitation token remains in the field.',
    );
  });
});
