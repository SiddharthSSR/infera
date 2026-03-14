import { describe, expect, it } from 'vitest';
import { inviteStatusMeta, memberStatusMeta, normalizeInviteStatus } from './workspaceLifecycle';

describe('workspaceLifecycle', () => {
  it('normalizes expired invites from pending records', () => {
    const invite = {
      id: 'inv_1',
      workspace_id: 'ws_alpha',
      email: 'teammate@example.com',
      display_name: 'Teammate',
      role: 'developer',
      invited_by_key_id: 'key_admin',
      created_at: '2026-03-10T00:00:00Z',
      expires_at: '2026-03-11T00:00:00Z',
      status: 'pending',
    };

    expect(normalizeInviteStatus(invite, new Date('2026-03-12T00:00:00Z'))).toBe('expired');
    expect(inviteStatusMeta(invite, new Date('2026-03-12T00:00:00Z'))).toMatchObject({
      label: 'EXPIRED',
      tone: 'warning',
      actionable: false,
    });
  });

  it('keeps accepted and revoked lifecycle states explicit', () => {
    const acceptedInvite = {
      id: 'inv_accepted',
      workspace_id: 'ws_alpha',
      email: 'accepted@example.com',
      display_name: 'Accepted',
      role: 'developer',
      invited_by_key_id: 'key_admin',
      created_at: '2026-03-10T00:00:00Z',
      expires_at: '2026-03-11T00:00:00Z',
      status: 'accepted',
    };
    const revokedInvite = {
      ...acceptedInvite,
      id: 'inv_revoked',
      status: 'revoked',
    };

    expect(inviteStatusMeta(acceptedInvite)).toMatchObject({ label: 'ACCEPTED', actionable: false });
    expect(inviteStatusMeta(revokedInvite)).toMatchObject({ label: 'REVOKED', tone: 'inactive', actionable: false });
  });

  it('marks the current member session distinctly from other memberships', () => {
    const currentMember = {
      id: 'mem_1',
      workspace_id: 'ws_alpha',
      email: 'me@example.com',
      display_name: 'Me',
      role: 'admin',
      status: 'active',
      created_at: '2026-03-10T00:00:00Z',
    };
    const removedMember = {
      ...currentMember,
      id: 'mem_2',
      email: 'removed@example.com',
      status: 'removed',
    };

    expect(memberStatusMeta(currentMember, 'mem_1')).toMatchObject({ label: 'YOU' });
    expect(memberStatusMeta(removedMember, 'mem_1')).toMatchObject({ label: 'REMOVED', tone: 'inactive' });
    expect(memberStatusMeta({ ...currentMember, id: 'mem_3' }, 'mem_1')).toMatchObject({ label: 'ACTIVE' });
  });
});
