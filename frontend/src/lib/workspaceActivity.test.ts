import { describe, expect, it } from 'vitest';
import { buildWorkspaceActivityItems } from './workspaceActivity';

describe('buildWorkspaceActivityItems', () => {
  it('builds recent team and access activity in reverse chronological order', () => {
    const items = buildWorkspaceActivityItems({
      members: [
        {
          id: 'mem_1',
          workspace_id: 'ws_alpha',
          email: 'member@example.com',
          display_name: 'Member One',
          role: 'developer',
          status: 'active',
          created_at: '2026-03-10T10:00:00Z',
        },
      ],
      invites: [
        {
          id: 'inv_1',
          workspace_id: 'ws_alpha',
          email: 'invitee@example.com',
          display_name: 'Invitee',
          role: 'operator',
          invited_by_key_id: 'key_1',
          created_at: '2026-03-11T10:00:00Z',
          expires_at: '2026-03-20T10:00:00Z',
          status: 'pending',
        },
      ],
      serviceAccounts: [
        {
          id: 'key_1',
          key_prefix: 'inf_svc',
          name: 'ci-bot',
          role: 'operator',
          principal_type: 'service_account',
          created_at: '2026-03-09T08:00:00Z',
          last_used: '2026-03-12T12:00:00Z',
          status: 'active',
        },
      ],
      providerConfigs: [
        {
          workspace_id: 'ws_alpha',
          provider: 'runpod',
          configured: true,
          endpoint: '',
          created_at: '2026-03-08T08:00:00Z',
          updated_at: '2026-03-12T08:00:00Z',
        },
      ],
    });

    expect(items.map((item) => item.title)).toEqual([
      'SERVICE ACCOUNT USED',
      'PROVIDER CONFIG UPDATED',
      'INVITE CREATED',
      'MEMBER JOINED',
    ]);
    expect(items[2].detail).toContain('now pending');
  });

  it('marks expired and revoked invites with lifecycle-aware tone', () => {
    const items = buildWorkspaceActivityItems({
      members: [],
      invites: [
        {
          id: 'inv_expired',
          workspace_id: 'ws_alpha',
          email: 'expired@example.com',
          display_name: '',
          role: 'developer',
          invited_by_key_id: 'key_1',
          created_at: '2026-03-10T10:00:00Z',
          expires_at: '2026-03-10T11:00:00Z',
          status: 'pending',
        },
        {
          id: 'inv_revoked',
          workspace_id: 'ws_alpha',
          email: 'revoked@example.com',
          display_name: '',
          role: 'developer',
          invited_by_key_id: 'key_1',
          created_at: '2026-03-09T10:00:00Z',
          expires_at: '2026-03-20T10:00:00Z',
          status: 'revoked',
        },
      ],
      serviceAccounts: [],
      providerConfigs: [],
    });

    expect(items.find((item) => item.id === 'invite-inv_expired')?.tone).toBe('warning');
    expect(items.find((item) => item.id === 'invite-inv_revoked')?.tone).toBe('inactive');
  });
});
