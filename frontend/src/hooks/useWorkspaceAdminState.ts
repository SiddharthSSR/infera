import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';

import {
  createApiKey,
  fetchApiKeys,
  revokeApiKey,
} from '../lib/authAccessClient';
import { fetchProviders } from '../lib/infrastructureClient';
import {
  createWorkspaceInvite,
  deleteWorkspaceProviderConfig,
  fetchAuditUsage,
  fetchWorkspaceInvites,
  fetchWorkspaceMembers,
  fetchWorkspaceProviderConfigs,
  fetchWorkspaceQuota,
  removeWorkspaceMember,
  revokeWorkspaceInvite,
  updateWorkspaceMember,
  updateWorkspaceQuota,
  upsertWorkspaceProviderConfig,
} from '../lib/workspaceAdminClient';
import { monthRange, parseNullableLimit } from '../lib/formatting';
import type {
  ApiKeyRecord,
  ProviderStatus,
  WorkspaceInvitationRecord,
  WorkspaceMemberRecord,
  WorkspaceProviderConfigRecord,
  WorkspaceQuotaRecord,
} from '../types';
import type { AuditUsageRow } from '../lib/apiCore';

function buildMemberRoleMap(members: WorkspaceMemberRecord[]): Record<string, string> {
  return members.reduce<Record<string, string>>((acc, record) => {
    acc[record.id] = record.role;
    return acc;
  }, {});
}

export function useWorkspaceAdminState({
  workspaceId,
  role,
}: {
  workspaceId: string;
  role: string;
}) {
  const canManageMemberships = role === 'owner' || role === 'admin';
  const canManageKeys = role === 'owner' || role === 'admin';
  const canManageProviderConfigs = role === 'owner' || role === 'admin';
  const canManageQuota = role === 'owner' || role === 'admin' || role === 'billing';
  const canViewQuota = canManageQuota || role === 'read_only';
  const canViewUsage = role === 'owner' || role === 'admin' || role === 'billing' || role === 'read_only';

  const [loading, setLoading] = useState(true);
  const [quota, setQuota] = useState<WorkspaceQuotaRecord | null>(null);
  const [members, setMembers] = useState<WorkspaceMemberRecord[]>([]);
  const [invites, setInvites] = useState<WorkspaceInvitationRecord[]>([]);
  const [serviceAccounts, setServiceAccounts] = useState<ApiKeyRecord[]>([]);
  const [providerConfigs, setProviderConfigs] = useState<WorkspaceProviderConfigRecord[]>([]);
  const [providerStatuses, setProviderStatuses] = useState<ProviderStatus[]>([]);
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);
  const [memberRoles, setMemberRoles] = useState<Record<string, string>>({});
  const [savingQuota, setSavingQuota] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [creatingServiceAccount, setCreatingServiceAccount] = useState(false);
  const [savingProviderConfig, setSavingProviderConfig] = useState(false);
  const [updatingMemberId, setUpdatingMemberId] = useState<string | null>(null);
  const [removingMemberId, setRemovingMemberId] = useState<string | null>(null);

  const refreshMembers = useCallback(async () => {
    if (!workspaceId) {
      setMembers([]);
      setMemberRoles({});
      return [];
    }
    const nextMembers = await fetchWorkspaceMembers(workspaceId);
    setMembers(nextMembers);
    setMemberRoles(buildMemberRoleMap(nextMembers));
    return nextMembers;
  }, [workspaceId]);

  const refreshInvites = useCallback(async () => {
    if (!workspaceId) {
      setInvites([]);
      return [];
    }
    const nextInvites = await fetchWorkspaceInvites(workspaceId);
    setInvites(nextInvites);
    return nextInvites;
  }, [workspaceId]);

  const refreshServiceAccounts = useCallback(async () => {
    const keys = await fetchApiKeys();
    const nextAccounts = keys.filter((key) => key.principal_type === 'service_account');
    setServiceAccounts(nextAccounts);
    return nextAccounts;
  }, []);

  const refreshProviderConfigs = useCallback(async () => {
    if (!workspaceId) {
      setProviderConfigs([]);
      return [];
    }
    const nextConfigs = await fetchWorkspaceProviderConfigs(workspaceId);
    setProviderConfigs(nextConfigs);
    return nextConfigs;
  }, [workspaceId]);

  useEffect(() => {
    let cancelled = false;

    if (!workspaceId) {
      setLoading(false);
      setQuota(null);
      setMembers([]);
      setInvites([]);
      setServiceAccounts([]);
      setProviderConfigs([]);
      setProviderStatuses([]);
      setUsageRows([]);
      setMemberRoles({});
      return () => { cancelled = true; };
    }

    setLoading(true);

    const loadWorkspaceData = async () => {
      const tasks: Promise<void>[] = [];

      if (canViewQuota) {
        tasks.push(
          fetchWorkspaceQuota(workspaceId)
            .then((nextQuota) => {
              if (!cancelled) setQuota(nextQuota);
            })
            .catch(() => {
              if (!cancelled) setQuota(null);
            }),
        );
      } else {
        setQuota(null);
      }

      if (canViewUsage) {
        const { start, end } = monthRange();
        tasks.push(
          fetchAuditUsage({ start, end, bucket: 'day', workspace_id: workspaceId })
            .then((usage) => {
              if (!cancelled) setUsageRows(usage.rows);
            })
            .catch(() => {
              if (!cancelled) setUsageRows([]);
            }),
        );
      } else {
        setUsageRows([]);
      }

      if (canManageMemberships) {
        tasks.push(
          fetchWorkspaceMembers(workspaceId)
            .then((nextMembers) => {
              if (!cancelled) {
                setMembers(nextMembers);
                setMemberRoles(buildMemberRoleMap(nextMembers));
              }
            })
            .catch(() => {
              if (!cancelled) {
                setMembers([]);
                setMemberRoles({});
              }
            }),
        );
        tasks.push(
          fetchWorkspaceInvites(workspaceId)
            .then((nextInvites) => {
              if (!cancelled) setInvites(nextInvites);
            })
            .catch(() => {
              if (!cancelled) setInvites([]);
            }),
        );
      } else {
        setMembers([]);
        setInvites([]);
      }

      if (canManageKeys) {
        tasks.push(
          fetchApiKeys()
            .then((keys) => {
              if (!cancelled) setServiceAccounts(keys.filter((key) => key.principal_type === 'service_account'));
            })
            .catch(() => {
              if (!cancelled) setServiceAccounts([]);
            }),
        );
      } else {
        setServiceAccounts([]);
      }

      if (canManageProviderConfigs) {
        tasks.push(
          fetchWorkspaceProviderConfigs(workspaceId)
            .then((configs) => {
              if (!cancelled) setProviderConfigs(configs);
            })
            .catch(() => {
              if (!cancelled) setProviderConfigs([]);
            }),
        );
        tasks.push(
          fetchProviders()
            .then((statuses) => {
              if (!cancelled) setProviderStatuses(statuses.filter((status) => status.provider !== 'mock' && status.provider !== 'lambda'));
            })
            .catch(() => {
              if (!cancelled) setProviderStatuses([]);
            }),
        );
      } else {
        setProviderConfigs([]);
        setProviderStatuses([]);
      }

      await Promise.all(tasks);
      if (!cancelled) setLoading(false);
    };

    loadWorkspaceData().catch(() => {
      if (!cancelled) setLoading(false);
    });

    return () => { cancelled = true; };
  }, [canManageKeys, canManageMemberships, canManageProviderConfigs, canViewQuota, canViewUsage, workspaceId]);

  const handleSaveQuota = useCallback(async ({
    requestLimit,
    tokenLimit,
    enforceHardLimits,
  }: {
    requestLimit: string;
    tokenLimit: string;
    enforceHardLimits: boolean;
  }) => {
    const parsedRequestLimit = parseNullableLimit(requestLimit);
    const parsedTokenLimit = parseNullableLimit(tokenLimit);
    if (Number.isNaN(parsedRequestLimit) || Number.isNaN(parsedTokenLimit)) {
      toast.error('Quota limits must be blank or non-negative numbers.');
      return null;
    }

    setSavingQuota(true);
    try {
      const nextQuota = await updateWorkspaceQuota(workspaceId, {
        monthly_request_limit: parsedRequestLimit,
        monthly_token_limit: parsedTokenLimit,
        enforce_hard_limits: enforceHardLimits,
      });
      setQuota(nextQuota);
      toast.success('Workspace quota updated.');
      return nextQuota;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to update quota');
      return null;
    } finally {
      setSavingQuota(false);
    }
  }, [workspaceId]);

  const handleCreateInvite = useCallback(async ({
    email,
    displayName,
    inviteRole,
  }: {
    email: string;
    displayName: string;
    inviteRole: string;
  }) => {
    if (!email.trim()) {
      toast.error('Invite email is required.');
      return null;
    }
    setCreatingInvite(true);
    try {
      const result = await createWorkspaceInvite(workspaceId, {
        email: email.trim(),
        display_name: displayName.trim() || undefined,
        role: inviteRole,
      });
      await refreshInvites();
      toast.success('Workspace invitation created.');
      return result.invitation_token;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to create invitation');
      return null;
    } finally {
      setCreatingInvite(false);
    }
  }, [refreshInvites, workspaceId]);

  const handleRevokeInvite = useCallback(async (inviteId: string) => {
    try {
      await revokeWorkspaceInvite(workspaceId, inviteId);
      await refreshInvites();
      toast.success('Invitation revoked.');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to revoke invitation');
    }
  }, [refreshInvites, workspaceId]);

  const handleUpdateMemberRole = useCallback(async ({
    memberId,
    currentRole,
    nextRole,
  }: {
    memberId: string;
    currentRole: string;
    nextRole: string;
  }) => {
    if (nextRole === currentRole) return;

    setUpdatingMemberId(memberId);
    try {
      await updateWorkspaceMember(workspaceId, memberId, { role: nextRole });
      await refreshMembers();
      toast.success('Member role updated.');
    } catch (error) {
      setMemberRoles((current) => ({ ...current, [memberId]: currentRole }));
      toast.error(error instanceof Error ? error.message : 'Failed to update member role');
    } finally {
      setUpdatingMemberId(null);
    }
  }, [refreshMembers, workspaceId]);

  const handleRemoveMember = useCallback(async (memberId: string) => {
    setRemovingMemberId(memberId);
    try {
      await removeWorkspaceMember(workspaceId, memberId);
      await refreshMembers();
      toast.success('Member removed.');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to remove member');
    } finally {
      setRemovingMemberId(null);
    }
  }, [refreshMembers, workspaceId]);

  const handleCreateServiceAccount = useCallback(async ({
    name,
    accountRole,
  }: {
    name: string;
    accountRole: string;
  }) => {
    if (!name.trim()) {
      toast.error('Service account name is required.');
      return null;
    }
    setCreatingServiceAccount(true);
    try {
      const result = await createApiKey(name.trim(), accountRole, 'service_account');
      await refreshServiceAccounts();
      toast.success('Service account key created.');
      return result.key;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to create service account');
      return null;
    } finally {
      setCreatingServiceAccount(false);
    }
  }, [refreshServiceAccounts]);

  const handleRevokeServiceAccount = useCallback(async (keyId: string) => {
    try {
      await revokeApiKey(keyId);
      await refreshServiceAccounts();
      toast.success('Service account key revoked.');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to revoke service account');
    }
  }, [refreshServiceAccounts]);

  const handleSaveProviderConfig = useCallback(async ({
    selectedProvider,
    providerLabel,
    providerAPIKey,
    providerAPISecret,
    providerEndpoint,
    providerOptions,
    validationError,
  }: {
    selectedProvider: string;
    providerLabel: string;
    providerAPIKey: string;
    providerAPISecret: string;
    providerEndpoint: string;
    providerOptions: Record<string, string>;
    validationError: string | null;
  }) => {
    if (validationError) {
      toast.error(validationError);
      return false;
    }
    setSavingProviderConfig(true);
    try {
      await upsertWorkspaceProviderConfig(workspaceId, selectedProvider, {
        api_key: providerAPIKey.trim(),
        api_secret: providerAPISecret.trim() || undefined,
        endpoint: providerEndpoint.trim() || undefined,
        options: providerOptions,
      });
      await refreshProviderConfigs();
      toast.success(`${providerLabel} config saved.`);
      return true;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to save provider config');
      return false;
    } finally {
      setSavingProviderConfig(false);
    }
  }, [refreshProviderConfigs, workspaceId]);

  const handleDeleteProviderConfig = useCallback(async (provider: string) => {
    try {
      await deleteWorkspaceProviderConfig(workspaceId, provider);
      await refreshProviderConfigs();
      toast.success(`${provider} provider config deleted.`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to delete provider config');
    }
  }, [refreshProviderConfigs, workspaceId]);

  return {
    loading,
    quota,
    members,
    invites,
    serviceAccounts,
    providerConfigs,
    providerStatuses,
    usageRows,
    memberRoles,
    savingQuota,
    creatingInvite,
    creatingServiceAccount,
    savingProviderConfig,
    updatingMemberId,
    removingMemberId,
    setMemberRoles,
    handleSaveQuota,
    handleCreateInvite,
    handleRevokeInvite,
    handleUpdateMemberRole,
    handleRemoveMember,
    handleCreateServiceAccount,
    handleRevokeServiceAccount,
    handleSaveProviderConfig,
    handleDeleteProviderConfig,
  };
}
