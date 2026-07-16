/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  acceptWorkspaceInvitation,
  createWorkspaceInvite,
  fetchInvitationPreview,
  fetchWorkspaceInvites,
  fetchWorkspaceMembers,
  updateWorkspaceMember,
} from './api';
import {
  parseWorkspaceInvitationAcceptResponse,
  parseWorkspaceInvitationCreateResponse,
  parseWorkspaceInvitationPreviewResponse,
  parseWorkspaceInvitationsResponse,
  parseWorkspaceMemberResponse,
  parseWorkspaceMembersResponse,
} from './workspaceAdmin';
import type {
  WorkspaceInvitationAcceptRequest,
  WorkspaceInvitationCreateRequest,
  WorkspaceMemberUpdateRequest,
} from '../types';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/workspace_admin');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

describe('workspace membership contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared workspace member fixtures', () => {
    const listPayload = loadJSONFixture('workspace_members_list_response.json');
    const memberPayload = loadJSONFixture('workspace_member_response.json');

    const list = parseWorkspaceMembersResponse(listPayload);
    const single = parseWorkspaceMemberResponse(memberPayload);

    expect(list.total).toBe(1);
    expect(list.members[0]?.display_name).toBe('Fixture Member');
    expect(single.member.role).toBe('operator');
  });

  it('parses the shared workspace invitation fixtures', () => {
    const listPayload = loadJSONFixture('workspace_invitations_list_response.json');
    const previewPayload = loadJSONFixture('workspace_invitation_preview_response.json');
    const createPayload = loadJSONFixture('workspace_invitation_create_response.json');
    const acceptPayload = loadJSONFixture('workspace_invitation_accept_response.json');

    const list = parseWorkspaceInvitationsResponse(listPayload);
    const preview = parseWorkspaceInvitationPreviewResponse(previewPayload);
    const created = parseWorkspaceInvitationCreateResponse(createPayload);
    const accepted = parseWorkspaceInvitationAcceptResponse(acceptPayload);

    expect(list.total).toBe(1);
    expect(list.invitations[0]?.invited_by_key_id).toBe('key_fixture_inviter');
    expect(preview.invitation.workspace_slug).toBe('fixture-team');
    expect(created.invitation_token).toBe('invite_fixture_token');
    expect(accepted.record.membership_id).toBe('mbr_fixture_accepted');
  });

  it('workspace membership API functions surface the shared auth error fixtures', async () => {
    mockFetch
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_missing_member_role.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_cannot_assign_role.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_missing_preview_token.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_invalid_invitation.json'),
      });

    await expect(updateWorkspaceMember('ws_alpha', 'mbr_fixture_member', {})).rejects.toThrow(
      'Failed to update workspace member (400 Bad Request): role is required',
    );
    await expect(createWorkspaceInvite('ws_alpha', { email: 'owner@example.com', role: 'admin' })).rejects.toThrow(
      'Failed to create workspace invite (403 Forbidden): You cannot assign that role.',
    );
    await expect(fetchInvitationPreview('')).rejects.toThrow(
      'Failed to load invitation (400 Bad Request): token is required',
    );
    await expect(acceptWorkspaceInvitation('invite_invalid', 'Joined Example')).rejects.toThrow(
      'Failed to accept invitation (400 Bad Request): invalid invitation',
    );
  });

  it('workspace membership API functions use the shared request and response fixtures', async () => {
    const memberListResponse = loadJSONFixture('workspace_members_list_response.json');
    const memberUpdateRequest = loadJSONFixture('workspace_member_update_request.json') as WorkspaceMemberUpdateRequest;
    const memberResponse = loadJSONFixture('workspace_member_response.json');
    const invitationListResponse = loadJSONFixture('workspace_invitations_list_response.json');
    const invitationPreviewResponse = loadJSONFixture('workspace_invitation_preview_response.json');
    const invitationAcceptRequest = loadJSONFixture('workspace_invitation_accept_request.json') as WorkspaceInvitationAcceptRequest;
    const invitationAcceptResponse = loadJSONFixture('workspace_invitation_accept_response.json');
    const invitationCreateRequest = loadJSONFixture('workspace_invitation_create_request.json') as WorkspaceInvitationCreateRequest;
    const invitationCreateResponse = loadJSONFixture('workspace_invitation_create_response.json');

    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => memberListResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => memberResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => invitationListResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => invitationPreviewResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => invitationAcceptResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => invitationCreateResponse });

    await expect(fetchWorkspaceMembers('ws_alpha')).resolves.toEqual([
      expect.objectContaining({ id: 'mbr_fixture_member', role: 'operator' }),
    ]);
    await expect(updateWorkspaceMember('ws_alpha', 'mbr_fixture_member', memberUpdateRequest)).resolves.toEqual(
      expect.objectContaining({ workspace_id: 'ws_alpha', display_name: 'Fixture Member' }),
    );
    await expect(fetchWorkspaceInvites('ws_alpha')).resolves.toEqual([
      expect.objectContaining({ id: 'inv_fixture_invite', email: 'invitee@example.com' }),
    ]);
    await expect(fetchInvitationPreview('invite_fixture_token')).resolves.toEqual(
      expect.objectContaining({ workspace_name: 'Fixture Team', role: 'developer' }),
    );
    await expect(
      acceptWorkspaceInvitation(
        invitationAcceptRequest.invitation_token,
        invitationAcceptRequest.display_name,
      ),
    ).resolves.toEqual(expect.objectContaining({
      key: 'inf_fixture_secret',
      membership: expect.objectContaining({ id: 'mbr_fixture_accepted' }),
    }));
    await expect(createWorkspaceInvite('ws_alpha', invitationCreateRequest)).resolves.toEqual(
      expect.objectContaining({ invitation_token: 'invite_fixture_token' }),
    );

    expect(mockFetch).toHaveBeenNthCalledWith(
      1,
      '/api/auth/workspaces/ws_alpha/members',
      expect.objectContaining({ credentials: 'include' }),
    );
    expect(JSON.parse(String(mockFetch.mock.calls[1]?.[1]?.body))).toEqual(memberUpdateRequest);
    expect(mockFetch).toHaveBeenNthCalledWith(
      4,
      '/api/auth/invitations/preview?token=invite_fixture_token',
    );
    expect(JSON.parse(String(mockFetch.mock.calls[4]?.[1]?.body))).toEqual(invitationAcceptRequest);
    expect(JSON.parse(String(mockFetch.mock.calls[5]?.[1]?.body))).toEqual(invitationCreateRequest);
  });
});
