import type { WorkspaceInvitationRecord, WorkspaceMemberRecord } from './api';

export type InviteLifecycleState = 'pending' | 'expired' | 'accepted' | 'revoked';

export function normalizeInviteStatus(invite: WorkspaceInvitationRecord, now = new Date()): InviteLifecycleState {
  if (invite.status === 'accepted' || invite.status === 'revoked') {
    return invite.status;
  }
  if (invite.status === 'pending' && new Date(invite.expires_at).getTime() <= now.getTime()) {
    return 'expired';
  }
  return 'pending';
}

export function inviteStatusMeta(invite: WorkspaceInvitationRecord, now = new Date()): {
  label: string;
  tone: '' | 'warning' | 'error' | 'inactive';
  detail: string;
  actionable: boolean;
} {
  const status = normalizeInviteStatus(invite, now);
  switch (status) {
    case 'accepted':
      return {
        label: 'ACCEPTED',
        tone: '',
        detail: 'The invite was consumed and the member joined this workspace.',
        actionable: false,
      };
    case 'revoked':
      return {
        label: 'REVOKED',
        tone: 'inactive',
        detail: 'The invite link/token was revoked and can no longer be used.',
        actionable: false,
      };
    case 'expired':
      return {
        label: 'EXPIRED',
        tone: 'warning',
        detail: 'The invite expired before it was accepted. Create a new invite if access is still needed.',
        actionable: false,
      };
    default:
      return {
        label: 'PENDING',
        tone: '',
        detail: 'This invite is still valid and waiting to be accepted.',
        actionable: true,
      };
  }
}

export function memberStatusMeta(member: WorkspaceMemberRecord, currentMemberId?: string): {
  label: string;
  tone: '' | 'warning' | 'error' | 'inactive';
  detail: string;
} {
  if (currentMemberId && member.id === currentMemberId) {
    return {
      label: 'YOU',
      tone: '',
      detail: 'This is the membership linked to your current dashboard session.',
    };
  }
  if (member.status === 'removed') {
    return {
      label: 'REMOVED',
      tone: 'inactive',
      detail: 'This membership was removed from the workspace.',
    };
  }
  return {
    label: 'ACTIVE',
    tone: '',
    detail: 'This member currently has access to the workspace.',
  };
}
