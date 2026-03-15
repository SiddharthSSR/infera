import type {
  ApiKeyRecord,
  WorkspaceInvitationRecord,
  WorkspaceMemberRecord,
  WorkspaceProviderConfigRecord,
} from './api';
import { normalizeInviteStatus } from './workspaceLifecycle';

export interface WorkspaceActivityItem {
  id: string;
  category: 'team' | 'access';
  title: string;
  detail: string;
  timestamp: string;
  tone: '' | 'warning' | 'error' | 'inactive';
}

function providerLabel(provider: string): string {
  if (provider === 'runpod') return 'RunPod';
  if (provider === 'vastai') return 'Vast.ai';
  return provider;
}

export function buildWorkspaceActivityItems(input: {
  members: WorkspaceMemberRecord[];
  invites: WorkspaceInvitationRecord[];
  serviceAccounts: ApiKeyRecord[];
  providerConfigs: WorkspaceProviderConfigRecord[];
}): WorkspaceActivityItem[] {
  const items: WorkspaceActivityItem[] = [];

  for (const member of input.members) {
    items.push({
      id: `member-${member.id}`,
      category: 'team',
      title: member.status === 'removed' ? 'MEMBERSHIP REMOVED' : 'MEMBER JOINED',
      detail: `${member.display_name || member.email} · ${member.role}`,
      timestamp: member.created_at,
      tone: member.status === 'removed' ? 'inactive' : '',
    });
  }

  for (const invite of input.invites) {
    const status = normalizeInviteStatus(invite);
    items.push({
      id: `invite-${invite.id}`,
      category: 'team',
      title: 'INVITE CREATED',
      detail: `${invite.display_name || invite.email} · ${invite.role} · now ${status}`,
      timestamp: invite.created_at,
      tone:
        status === 'expired'
          ? 'warning'
          : status === 'revoked'
            ? 'inactive'
            : '',
    });
  }

  for (const account of input.serviceAccounts) {
    items.push({
      id: `svc-${account.id}`,
      category: 'access',
      title: account.last_used ? 'SERVICE ACCOUNT USED' : 'SERVICE ACCOUNT CREATED',
      detail: `${account.name} · ${account.role}`,
      timestamp: account.last_used || account.created_at,
      tone: '',
    });
  }

  for (const config of input.providerConfigs) {
    items.push({
      id: `provider-${config.provider}`,
      category: 'access',
      title: 'PROVIDER CONFIG UPDATED',
      detail: `${providerLabel(config.provider)} · ${config.endpoint || 'default endpoint'}`,
      timestamp: config.updated_at,
      tone: '',
    });
  }

  return items.sort((left, right) => new Date(right.timestamp).getTime() - new Date(left.timestamp).getTime());
}
