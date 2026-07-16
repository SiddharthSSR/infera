import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';

import { monthRange } from '../lib/formatting';
import type { ApiKeyRecord } from '../types';
import type { AuditUsageRow } from '../lib/apiCore';
import {
  fetchApiKeys,
} from '../lib/authAccessClient';
import {
  fetchAuditUsage,
  fetchWorkspaceInvites,
  fetchWorkspaceQuota,
  updateWorkspaceQuota,
} from '../lib/workspaceAdminClient';
import type { WorkspaceInvitationRecord, WorkspaceQuotaRecord } from '../types';

export function useDashboardWorkspaceState({
  workspaceID,
  role,
}: {
  workspaceID: string | undefined;
  role: string;
}) {
  const [quota, setQuota] = useState<WorkspaceQuotaRecord | null>(null);
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);
  const [workspaceInvites, setWorkspaceInvites] = useState<WorkspaceInvitationRecord[]>([]);
  const [workspaceServiceAccounts, setWorkspaceServiceAccounts] = useState<ApiKeyRecord[]>([]);

  const canViewQuota = role === 'owner' || role === 'admin' || role === 'billing' || role === 'read_only';
  const canViewUsage = canViewQuota;
  const canEditQuota = role === 'owner' || role === 'admin' || role === 'billing';
  const canManageWorkspaceMembership = role === 'owner' || role === 'admin';

  const handleQuickConfigSave = useCallback(async (key: string, value: string) => {
    if (!workspaceID) return;
    const currentQuota = quota ?? { workspace_id: workspaceID, enforce_hard_limits: true, updated_at: '' };
    const payload: { monthly_request_limit?: number | null; monthly_token_limit?: number | null; enforce_hard_limits: boolean } = {
      monthly_request_limit: currentQuota.monthly_request_limit,
      monthly_token_limit: currentQuota.monthly_token_limit,
      enforce_hard_limits: currentQuota.enforce_hard_limits,
    };
    if (key === 'monthly_request_limit') payload.monthly_request_limit = value === '' ? null : Number(value);
    else if (key === 'monthly_token_limit') payload.monthly_token_limit = value === '' ? null : Number(value);
    else if (key === 'enforce_hard_limits') payload.enforce_hard_limits = value === 'true';

    try {
      const nextQuota = await updateWorkspaceQuota(workspaceID, payload);
      setQuota(nextQuota);
      toast.success('Configuration updated.');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to save configuration.');
      throw err;
    }
  }, [workspaceID, quota]);

  useEffect(() => {
    let cancelled = false;

    if (!workspaceID) {
      setQuota(null);
      setUsageRows([]);
      setWorkspaceInvites([]);
      setWorkspaceServiceAccounts([]);
      return () => { cancelled = true; };
    }

    if (canViewQuota) {
      fetchWorkspaceQuota(workspaceID)
        .then((nextQuota) => { if (!cancelled) setQuota(nextQuota); })
        .catch(() => { if (!cancelled) setQuota(null); });
    } else {
      setQuota(null);
    }

    if (canViewUsage) {
      const { start, end } = monthRange();
      fetchAuditUsage({ start, end, bucket: 'day', workspace_id: workspaceID })
        .then((usage) => { if (!cancelled) setUsageRows(usage.rows); })
        .catch(() => { if (!cancelled) setUsageRows([]); });
    } else {
      setUsageRows([]);
    }

    if (canManageWorkspaceMembership) {
      fetchWorkspaceInvites(workspaceID)
        .then((invites) => { if (!cancelled) setWorkspaceInvites(invites); })
        .catch(() => { if (!cancelled) setWorkspaceInvites([]); });

      fetchApiKeys()
        .then((keys) => {
          if (!cancelled) {
            setWorkspaceServiceAccounts(
              keys.filter((key) => key.principal_type === 'service_account' && key.status === 'active'),
            );
          }
        })
        .catch(() => { if (!cancelled) setWorkspaceServiceAccounts([]); });
    } else {
      setWorkspaceInvites([]);
      setWorkspaceServiceAccounts([]);
    }

    return () => { cancelled = true; };
  }, [canManageWorkspaceMembership, canViewQuota, canViewUsage, workspaceID]);

  return {
    quota,
    usageRows,
    workspaceInvites,
    workspaceServiceAccounts,
    canEditQuota,
    handleQuickConfigSave,
  };
}
