import { useEffect, useMemo, useState } from 'react';
import { toast } from 'sonner';
import {
  fetchApiKeys,
  createApiKey,
  revokeApiKey,
  fetchAuditUsage,
  fetchWorkspaceQuota,
  updateWorkspaceQuota,
  fetchWorkspaceMembers,
  updateWorkspaceMember,
  removeWorkspaceMember,
  fetchWorkspaceInvites,
  fetchWorkspaceProviderConfigs,
  createWorkspaceInvite,
  upsertWorkspaceProviderConfig,
  deleteWorkspaceProviderConfig,
  revokeWorkspaceInvite,
  type ApiKeyRecord,
  type AuditUsageRow,
  type WorkspaceQuotaRecord,
  type WorkspaceMemberRecord,
  type WorkspaceInvitationRecord,
  type WorkspaceProviderConfigRecord,
} from '../lib/api';
import { useAuthSession } from '../lib/auth-context';

const assignableInviteRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const serviceAccountRoles = ['operator', 'developer', 'read_only', 'billing'] as const;
const workspaceMemberRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const configurableProviders = [
  { id: 'runpod', name: 'RunPod', endpointPlaceholder: 'https://api.runpod.io/graphql' },
  { id: 'vastai', name: 'Vast.ai', endpointPlaceholder: 'Optional custom endpoint' },
] as const;

function formatDate(dateStr?: string | null) {
  if (!dateStr) return 'Never';
  try {
    return new Date(dateStr).toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  } catch {
    return dateStr;
  }
}

function parseNullableLimit(value: string): number | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) return NaN;
  return parsed;
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value);
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value) || value < 0) return 0;
  return Math.min(value * 100, 100);
}

function monthRange() {
  const now = new Date();
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1, 0, 0, 0, 0));
  return { start: start.toISOString(), end: now.toISOString() };
}

export function WorkspaceAdmin() {
  const { session } = useAuthSession();
  const workspaceId = session?.workspace?.id ?? '';
  const role = session?.key?.role ?? 'user';
  const member = session?.member;

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
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);

  const [requestLimit, setRequestLimit] = useState('');
  const [tokenLimit, setTokenLimit] = useState('');
  const [enforceHardLimits, setEnforceHardLimits] = useState(true);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteDisplayName, setInviteDisplayName] = useState('');
  const [inviteRole, setInviteRole] = useState<typeof assignableInviteRoles[number]>('developer');
  const [newServiceAccountName, setNewServiceAccountName] = useState('');
  const [newServiceAccountRole, setNewServiceAccountRole] = useState<typeof serviceAccountRoles[number]>('operator');
  const [selectedProvider, setSelectedProvider] = useState<typeof configurableProviders[number]['id']>('runpod');
  const [providerAPIKey, setProviderAPIKey] = useState('');
  const [providerAPISecret, setProviderAPISecret] = useState('');
  const [providerEndpoint, setProviderEndpoint] = useState('');
  const [memberRoles, setMemberRoles] = useState<Record<string, string>>({});
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);
  const [createdInviteToken, setCreatedInviteToken] = useState<string | null>(null);
  const [savingQuota, setSavingQuota] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [creatingServiceAccount, setCreatingServiceAccount] = useState(false);
  const [savingProviderConfig, setSavingProviderConfig] = useState(false);
  const [updatingMemberId, setUpdatingMemberId] = useState<string | null>(null);
  const [removingMemberId, setRemovingMemberId] = useState<string | null>(null);

  const visibleInviteRoles = useMemo(() => {
    if (role === 'owner') return assignableInviteRoles;
    return assignableInviteRoles.filter((candidate) => candidate !== 'admin');
  }, [role]);

  const visibleMemberRoles = useMemo(() => {
    if (role === 'owner') return workspaceMemberRoles;
    return workspaceMemberRoles.filter((candidate) => candidate !== 'admin');
  }, [role]);

  const usageSummary = useMemo(() => {
    const byDay = new Map<string, { requests: number; tokens: number }>();
    const byKey = new Map<string, { requests: number; tokens: number; successes: number; errors: number }>();
    let requests = 0;
    let tokens = 0;
    let successes = 0;
    let errors = 0;

    for (const row of usageRows) {
      requests += row.requests;
      tokens += row.tokens;
      successes += row.successes;
      errors += row.errors;

      const day = row.bucket_start.slice(0, 10);
      const dayTotals = byDay.get(day) || { requests: 0, tokens: 0 };
      dayTotals.requests += row.requests;
      dayTotals.tokens += row.tokens;
      byDay.set(day, dayTotals);

      const keyId = row.key_id || 'unknown';
      const keyTotals = byKey.get(keyId) || { requests: 0, tokens: 0, successes: 0, errors: 0 };
      keyTotals.requests += row.requests;
      keyTotals.tokens += row.tokens;
      keyTotals.successes += row.successes;
      keyTotals.errors += row.errors;
      byKey.set(keyId, keyTotals);
    }

    const dailyTrend = Array.from(byDay.entries())
      .sort(([left], [right]) => left.localeCompare(right))
      .slice(-14)
      .map(([day, totals]) => ({ day, ...totals }));

    const topKeys = Array.from(byKey.entries())
      .sort(([, left], [, right]) => right.tokens - left.tokens || right.requests - left.requests)
      .slice(0, 5)
      .map(([keyId, totals]) => ({ keyId, ...totals }));

    return { requests, tokens, successes, errors, dailyTrend, topKeys };
  }, [usageRows]);

  const requestUsageRatio = quota?.monthly_request_limit ? usageSummary.requests / quota.monthly_request_limit : 0;
  const tokenUsageRatio = quota?.monthly_token_limit ? usageSummary.tokens / quota.monthly_token_limit : 0;
  const quotaPressure = Math.max(requestUsageRatio, tokenUsageRatio);
  const quotaState = quotaPressure >= 1 ? 'EXCEEDED' : quotaPressure >= 0.8 ? 'NEAR LIMIT' : 'HEALTHY';
  const quotaStateClass = quotaPressure >= 1 ? 'error' : quotaPressure >= 0.8 ? 'warning' : '';
  const selectedProviderMeta = configurableProviders.find((provider) => provider.id === selectedProvider) || configurableProviders[0];

  const loadWorkspaceData = async () => {
    if (!workspaceId) return;

    const tasks: Promise<void>[] = [];

    if (canViewQuota) {
      tasks.push(
        fetchWorkspaceQuota(workspaceId).then((nextQuota) => {
          setQuota(nextQuota);
          setRequestLimit(nextQuota.monthly_request_limit != null ? String(nextQuota.monthly_request_limit) : '');
          setTokenLimit(nextQuota.monthly_token_limit != null ? String(nextQuota.monthly_token_limit) : '');
          setEnforceHardLimits(nextQuota.enforce_hard_limits);
        }).catch(() => setQuota(null)),
      );
    } else {
      setQuota(null);
    }

    if (canViewUsage) {
      const { start, end } = monthRange();
      tasks.push(
        fetchAuditUsage({ start, end, bucket: 'day', workspace_id: workspaceId })
          .then((usage) => setUsageRows(usage.rows))
          .catch(() => setUsageRows([])),
      );
    } else {
      setUsageRows([]);
    }

    if (canManageMemberships) {
      tasks.push(
        fetchWorkspaceMembers(workspaceId).then((nextMembers) => {
          setMembers(nextMembers);
          setMemberRoles(
            nextMembers.reduce<Record<string, string>>((acc, record) => {
              acc[record.id] = record.role;
              return acc;
            }, {}),
          );
        }).catch(() => {
          setMembers([]);
          setMemberRoles({});
        }),
      );
      tasks.push(
        fetchWorkspaceInvites(workspaceId).then(setInvites).catch(() => setInvites([])),
      );
    } else {
      setMembers([]);
      setInvites([]);
    }

    if (canManageKeys) {
      tasks.push(
        fetchApiKeys().then((keys) => {
          setServiceAccounts(keys.filter((key) => key.principal_type === 'service_account'));
        }).catch(() => setServiceAccounts([])),
      );
    } else {
      setServiceAccounts([]);
    }

    if (canManageProviderConfigs) {
      tasks.push(
        fetchWorkspaceProviderConfigs(workspaceId)
          .then(setProviderConfigs)
          .catch(() => setProviderConfigs([])),
      );
    } else {
      setProviderConfigs([]);
    }

    await Promise.all(tasks);
  };

  useEffect(() => {
    setLoading(true);
    loadWorkspaceData().finally(() => setLoading(false));
  }, [workspaceId, canManageMemberships, canManageKeys, canManageProviderConfigs, canViewQuota, canViewUsage]);

  const handleSaveQuota = async () => {
    const parsedRequestLimit = parseNullableLimit(requestLimit);
    const parsedTokenLimit = parseNullableLimit(tokenLimit);
    if (Number.isNaN(parsedRequestLimit) || Number.isNaN(parsedTokenLimit)) {
      toast.error('Quota limits must be blank or non-negative numbers.');
      return;
    }

    setSavingQuota(true);
    try {
      const nextQuota = await updateWorkspaceQuota(workspaceId, {
        monthly_request_limit: parsedRequestLimit,
        monthly_token_limit: parsedTokenLimit,
        enforce_hard_limits: enforceHardLimits,
      });
      setQuota(nextQuota);
      setRequestLimit(nextQuota.monthly_request_limit != null ? String(nextQuota.monthly_request_limit) : '');
      setTokenLimit(nextQuota.monthly_token_limit != null ? String(nextQuota.monthly_token_limit) : '');
      setEnforceHardLimits(nextQuota.enforce_hard_limits);
      toast.success('Workspace quota updated.');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to update quota');
    } finally {
      setSavingQuota(false);
    }
  };

  const handleCreateInvite = async () => {
    if (!inviteEmail.trim()) {
      toast.error('Invite email is required.');
      return;
    }
    setCreatingInvite(true);
    try {
      const result = await createWorkspaceInvite(workspaceId, {
        email: inviteEmail.trim(),
        display_name: inviteDisplayName.trim() || undefined,
        role: inviteRole,
      });
      setCreatedInviteToken(result.invitation_token);
      setInviteEmail('');
      setInviteDisplayName('');
      setInviteRole(visibleInviteRoles[0]);
      toast.success('Workspace invitation created.');
      setInvites(await fetchWorkspaceInvites(workspaceId));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to create invitation');
    } finally {
      setCreatingInvite(false);
    }
  };

  const handleRevokeInvite = async (inviteId: string) => {
    if (!confirm('Revoke this invitation?')) return;
    try {
      await revokeWorkspaceInvite(workspaceId, inviteId);
      toast.success('Invitation revoked.');
      setInvites(await fetchWorkspaceInvites(workspaceId));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to revoke invitation');
    }
  };

  const roleOptionsForMember = (currentRole: string) => {
    const options = new Set<string>(visibleMemberRoles);
    options.add(currentRole);
    return Array.from(options);
  };

  const handleUpdateMemberRole = async (memberId: string, currentRole: string) => {
    const nextRole = memberRoles[memberId] || currentRole;
    if (nextRole === currentRole) return;
    setUpdatingMemberId(memberId);
    try {
      await updateWorkspaceMember(workspaceId, memberId, { role: nextRole });
      toast.success('Member role updated.');
      const nextMembers = await fetchWorkspaceMembers(workspaceId);
      setMembers(nextMembers);
      setMemberRoles(
        nextMembers.reduce<Record<string, string>>((acc, record) => {
          acc[record.id] = record.role;
          return acc;
        }, {}),
      );
    } catch (error) {
      setMemberRoles((current) => ({ ...current, [memberId]: currentRole }));
      toast.error(error instanceof Error ? error.message : 'Failed to update member role');
    } finally {
      setUpdatingMemberId(null);
    }
  };

  const handleRemoveMember = async (memberId: string) => {
    if (!confirm('Remove this member from the workspace? Their linked human keys will be revoked.')) return;
    setRemovingMemberId(memberId);
    try {
      await removeWorkspaceMember(workspaceId, memberId);
      toast.success('Member removed.');
      const nextMembers = await fetchWorkspaceMembers(workspaceId);
      setMembers(nextMembers);
      setMemberRoles(
        nextMembers.reduce<Record<string, string>>((acc, record) => {
          acc[record.id] = record.role;
          return acc;
        }, {}),
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to remove member');
    } finally {
      setRemovingMemberId(null);
    }
  };

  const handleCreateServiceAccount = async () => {
    if (!newServiceAccountName.trim()) {
      toast.error('Service account name is required.');
      return;
    }
    setCreatingServiceAccount(true);
    try {
      const result = await createApiKey(newServiceAccountName.trim(), newServiceAccountRole, 'service_account');
      setCreatedSecret(result.key);
      setNewServiceAccountName('');
      setNewServiceAccountRole('operator');
      toast.success('Service account key created.');
      const keys = await fetchApiKeys();
      setServiceAccounts(keys.filter((key) => key.principal_type === 'service_account'));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to create service account');
    } finally {
      setCreatingServiceAccount(false);
    }
  };

  const handleRevokeServiceAccount = async (keyId: string) => {
    if (!confirm('Revoke this service account key?')) return;
    try {
      await revokeApiKey(keyId);
      toast.success('Service account key revoked.');
      const keys = await fetchApiKeys();
      setServiceAccounts(keys.filter((key) => key.principal_type === 'service_account'));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to revoke service account');
    }
  };

  const handleSaveProviderConfig = async () => {
    if (!providerAPIKey.trim()) {
      toast.error('Provider API key is required.');
      return;
    }
    setSavingProviderConfig(true);
    try {
      await upsertWorkspaceProviderConfig(workspaceId, selectedProvider, {
        api_key: providerAPIKey.trim(),
        api_secret: providerAPISecret.trim() || undefined,
        endpoint: providerEndpoint.trim() || undefined,
      });
      setProviderAPIKey('');
      setProviderAPISecret('');
      setProviderEndpoint('');
      setProviderConfigs(await fetchWorkspaceProviderConfigs(workspaceId));
      toast.success(`${selectedProviderMeta.name} config saved.`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to save provider config');
    } finally {
      setSavingProviderConfig(false);
    }
  };

  const handleDeleteProviderConfig = async (provider: string) => {
    if (!confirm(`Delete ${provider} provider config for this workspace?`)) return;
    try {
      await deleteWorkspaceProviderConfig(workspaceId, provider);
      setProviderConfigs(await fetchWorkspaceProviderConfigs(workspaceId));
      toast.success(`${provider} provider config deleted.`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to delete provider config');
    }
  };

  if (loading) {
    return (
      <div className="animate-fade-in">
        <div className="grid-row">
          <div className="cell" style={{ gridColumn: 'span 4', color: 'var(--text-secondary)' }}>
            Loading workspace settings...
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="animate-fade-in">
      {createdInviteToken && (
        <div style={{
          padding: '1.25rem 2rem',
          backgroundColor: '#E8F5E9',
          borderBottom: 'var(--grid-line)',
        }}>
          <div className="label-text" style={{ marginBottom: '0.6rem' }}>INVITATION TOKEN — COPY NOW</div>
          <div className="code-block" style={{ marginTop: 0 }}>{createdInviteToken}</div>
          <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.5 }}>
            This does not send an email yet. Share the token or the <code style={{ fontFamily: 'var(--font-mono)' }}>/accept-invite</code> link manually with the person you invited.
          </div>
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <button className="btn-primary" onClick={() => navigator.clipboard.writeText(createdInviteToken).then(() => toast.success('Invitation token copied.'))}>COPY TOKEN</button>
            <button className="btn-secondary" onClick={() => setCreatedInviteToken(null)}>DISMISS</button>
          </div>
        </div>
      )}

      {createdSecret && (
        <div style={{
          padding: '1.25rem 2rem',
          backgroundColor: '#E8F5E9',
          borderBottom: 'var(--grid-line)',
        }}>
          <div className="label-text" style={{ marginBottom: '0.6rem' }}>SERVICE ACCOUNT KEY — COPY NOW</div>
          <div className="code-block" style={{ marginTop: 0 }}>{createdSecret}</div>
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <button className="btn-primary" onClick={() => navigator.clipboard.writeText(createdSecret).then(() => toast.success('Service account key copied.'))}>COPY KEY</button>
            <button className="btn-secondary" onClick={() => setCreatedSecret(null)}>DISMISS</button>
          </div>
        </div>
      )}

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1rem' }}>WORKSPACE PROFILE</div>
          <h2 style={{ fontSize: '2rem', lineHeight: 1.1 }}>{session?.workspace?.name || 'Workspace'}</h2>
          <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <span className="badge">{role.toUpperCase()}</span>
            <span className="badge">{session?.key?.principal_type === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN'}</span>
            {session?.workspace?.slug && <span className="badge mono">{session.workspace.slug}</span>}
          </div>
          <div style={{ marginTop: '1.5rem', color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.6 }}>
            {member?.email
              ? `Signed in as ${member.email}.`
              : 'Signed in with a workspace-scoped key.'}
            <br />
            Workspace administration is gated by the backend role model already enforced on auth, quota, infrastructure, and audit routes.
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '1rem' }}>ACCESS SURFACE</div>
          <div style={{ display: 'grid', gap: '0.8rem', fontSize: '0.9rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage memberships</span><span className="mono">{canManageMemberships ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage service accounts</span><span className="mono">{canManageKeys ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage provider configs</span><span className="mono">{canManageProviderConfigs ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage quota</span><span className="mono">{canManageQuota ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>View quota</span><span className="mono">{canViewQuota ? 'YES' : 'NO'}</span></div>
          </div>
        </div>
      </div>

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>CURRENT MONTH USAGE</div>
          {canViewUsage ? (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '1rem' }}>
                <div style={{ padding: '1rem', backgroundColor: 'var(--bg-accent)' }}>
                  <div className="label-text">REQUESTS</div>
                  <div style={{ fontSize: '2rem', marginTop: '0.5rem' }}>{formatCount(usageSummary.requests)}</div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                    {formatCount(usageSummary.successes)} success / {formatCount(usageSummary.errors)} error
                  </div>
                </div>
                <div style={{ padding: '1rem', backgroundColor: 'var(--bg-accent)' }}>
                  <div className="label-text">TOKENS</div>
                  <div style={{ fontSize: '2rem', marginTop: '0.5rem' }}>{formatCount(usageSummary.tokens)}</div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                    Aggregated from workspace audit records
                  </div>
                </div>
              </div>

              <div style={{ marginTop: '1.5rem', display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
                <span className={`status-dot ${quotaStateClass}`}></span>
                <span className="label-text">QUOTA STATE</span>
                <span className="badge">{quotaState}</span>
              </div>

              <div style={{ marginTop: '1rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.9rem' }}>
                  <span>Request quota</span>
                  <span className="mono">
                    {quota?.monthly_request_limit != null
                      ? `${formatCount(usageSummary.requests)} / ${formatCount(quota.monthly_request_limit)} (${formatPercent(requestUsageRatio)})`
                      : `${formatCount(usageSummary.requests)} / unlimited`}
                  </span>
                </div>
                <div className="progress-track">
                  <div className="progress-fill" style={{ width: `${clampPercent(requestUsageRatio)}%` }} />
                </div>
              </div>

              <div style={{ marginTop: '1rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.9rem' }}>
                  <span>Token quota</span>
                  <span className="mono">
                    {quota?.monthly_token_limit != null
                      ? `${formatCount(usageSummary.tokens)} / ${formatCount(quota.monthly_token_limit)} (${formatPercent(tokenUsageRatio)})`
                      : `${formatCount(usageSummary.tokens)} / unlimited`}
                  </span>
                </div>
                <div className="progress-track">
                  <div className="progress-fill" style={{ width: `${clampPercent(tokenUsageRatio)}%` }} />
                </div>
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted to workspace owners, admins, billing, and read-only roles.
            </div>
          )}
        </div>

        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>RECENT DAILY TREND</div>
          {canViewUsage ? (
            usageSummary.dailyTrend.length > 0 ? (
              <div className="mobile-data-list">
                {usageSummary.dailyTrend.map((entry) => (
                  <div key={entry.day} className="mobile-data-card">
                    <div className="mobile-data-card-header">
                      <div>
                        <div className="mobile-data-title">{entry.day}</div>
                        <div className="mobile-data-subtitle">{formatCount(entry.requests)} requests</div>
                      </div>
                      <span className="badge mono">{formatCount(entry.tokens)} TOKENS</span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                No usage recorded for this workspace in the current month yet.
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted for this role.
            </div>
          )}
        </div>
      </div>

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>PROVIDER CONFIGS</div>
          {canManageProviderConfigs ? (
            <>
              <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th>PROVIDER</th>
                      <th>STATE</th>
                      <th>ENDPOINT</th>
                      <th>UPDATED</th>
                      <th style={{ textAlign: 'right' }}>ACTION</th>
                    </tr>
                  </thead>
                  <tbody>
                    {providerConfigs.map((config) => (
                      <tr key={config.provider}>
                        <td>{config.provider}</td>
                        <td><span className="badge">{config.configured ? 'CONFIGURED' : 'INCOMPLETE'}</span></td>
                        <td className="mono">{config.endpoint || 'default'}</td>
                        <td>{formatDate(config.updated_at)}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button className="action-btn destructive" onClick={() => handleDeleteProviderConfig(config.provider)}>DELETE</button>
                        </td>
                      </tr>
                    ))}
                    {providerConfigs.length === 0 && (
                      <tr><td colSpan={5} style={{ color: 'var(--text-secondary)', padding: '1.5rem 0' }}>No workspace provider configs yet.</td></tr>
                    )}
                  </tbody>
                </table>
              </div>

              <div style={{ display: 'grid', gap: '1rem' }}>
                <div>
                  <div className="label-text">PROVIDER</div>
                  <select className="control-input" value={selectedProvider} onChange={(e) => setSelectedProvider(e.target.value as typeof configurableProviders[number]['id'])}>
                    {configurableProviders.map((provider) => (
                      <option key={provider.id} value={provider.id}>{provider.name}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <div className="label-text">API KEY</div>
                  <input className="control-input" type="password" value={providerAPIKey} onChange={(e) => setProviderAPIKey(e.target.value)} placeholder="Write-only" />
                </div>
                <div>
                  <div className="label-text">API SECRET</div>
                  <input className="control-input" type="password" value={providerAPISecret} onChange={(e) => setProviderAPISecret(e.target.value)} placeholder="Optional write-only secret" />
                </div>
                <div>
                  <div className="label-text">ENDPOINT</div>
                  <input className="control-input" value={providerEndpoint} onChange={(e) => setProviderEndpoint(e.target.value)} placeholder={selectedProviderMeta.endpointPlaceholder} />
                </div>
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                  Stored secrets are never shown again after save. Update a provider by submitting a new key/secret for the selected provider.
                </div>
                <div>
                  <button className="btn-primary" disabled={savingProviderConfig} onClick={handleSaveProviderConfig}>
                    {savingProviderConfig ? 'SAVING...' : 'SAVE PROVIDER CONFIG'}
                  </button>
                </div>
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Provider configuration is restricted to workspace owners and admins.
            </div>
          )}
        </div>

        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>WORKSPACE QUOTA</div>
          {canViewQuota && quota ? (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>
                <div>
                  <div className="label-text">MONTHLY REQUEST LIMIT</div>
                  <input className="control-input" value={requestLimit} disabled={!canManageQuota} onChange={(e) => setRequestLimit(e.target.value)} placeholder="Unlimited" />
                </div>
                <div>
                  <div className="label-text">MONTHLY TOKEN LIMIT</div>
                  <input className="control-input" value={tokenLimit} disabled={!canManageQuota} onChange={(e) => setTokenLimit(e.target.value)} placeholder="Unlimited" />
                </div>
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '1.25rem', fontSize: '0.9rem' }}>
                <input type="checkbox" checked={enforceHardLimits} disabled={!canManageQuota} onChange={(e) => setEnforceHardLimits(e.target.checked)} />
                Enforce hard limits before routing inference traffic
              </label>
              <div style={{ marginTop: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                Last updated {formatDate(quota.updated_at)}
              </div>
              {canManageQuota && (
                <button className="btn-primary" style={{ marginTop: '1.25rem' }} disabled={savingQuota} onClick={handleSaveQuota}>
                  {savingQuota ? 'SAVING...' : 'SAVE QUOTA'}
                </button>
              )}
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              You do not have permission to view quota settings for this workspace.
            </div>
          )}
        </div>

        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>SERVICE ACCOUNTS</div>
          {canManageKeys ? (
            <>
              <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th>NAME</th>
                      <th>ROLE</th>
                      <th>PREFIX</th>
                      <th>LAST USED</th>
                      <th style={{ textAlign: 'right' }}>ACTION</th>
                    </tr>
                  </thead>
                  <tbody>
                    {serviceAccounts.map((key) => (
                      <tr key={key.id}>
                        <td>{key.name}</td>
                        <td><span className="badge">{key.role.toUpperCase()}</span></td>
                        <td className="mono">{key.key_prefix}</td>
                        <td>{formatDate(key.last_used)}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button className="action-btn destructive" onClick={() => handleRevokeServiceAccount(key.id)}>REVOKE</button>
                        </td>
                      </tr>
                    ))}
                    {serviceAccounts.length === 0 && (
                      <tr><td colSpan={5} style={{ color: 'var(--text-secondary)', padding: '1.5rem 0' }}>No service accounts yet.</td></tr>
                    )}
                  </tbody>
                </table>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr auto', gap: '1rem', alignItems: 'end' }}>
                <div>
                  <div className="label-text">NAME</div>
                  <input className="control-input" value={newServiceAccountName} onChange={(e) => setNewServiceAccountName(e.target.value)} placeholder="ci-bot" />
                </div>
                <div>
                  <div className="label-text">ROLE</div>
                  <select className="control-input" value={newServiceAccountRole} onChange={(e) => setNewServiceAccountRole(e.target.value as typeof serviceAccountRoles[number])}>
                    {serviceAccountRoles.map((candidate) => (
                      <option key={candidate} value={candidate}>{candidate}</option>
                    ))}
                  </select>
                </div>
                <button className="btn-primary" disabled={creatingServiceAccount} onClick={handleCreateServiceAccount}>
                  {creatingServiceAccount ? 'CREATING...' : 'CREATE'}
                </button>
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Service account management is restricted to workspace owners and admins.
            </div>
          )}
        </div>
      </div>

      <div className="grid-row" style={{ alignItems: 'start' }}>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>MEMBERS</div>
          {canManageMemberships ? (
            members.length > 0 ? (
              <div className="mobile-data-list">
                {members.map((memberRecord) => (
                  <div key={memberRecord.id} className="mobile-data-card">
                    <div className="mobile-data-card-header">
                      <div>
                        <div className="mobile-data-title">{memberRecord.display_name}</div>
                        <div className="mobile-data-subtitle">{memberRecord.email}</div>
                      </div>
                      <span className="badge">{(memberRoles[memberRecord.id] || memberRecord.role).toUpperCase()}</span>
                    </div>
                    <div className="mobile-data-meta">
                      <div><span className="label-text">STATUS</span> <span>{memberRecord.status}</span></div>
                      <div><span className="label-text">JOINED</span> <span>{formatDate(memberRecord.created_at)}</span></div>
                    </div>
                    <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
                      <div>
                        <div className="label-text">ROLE</div>
                        <select
                          className="control-input"
                          value={memberRoles[memberRecord.id] || memberRecord.role}
                          disabled={member?.id === memberRecord.id}
                          onChange={(e) => setMemberRoles((current) => ({ ...current, [memberRecord.id]: e.target.value }))}
                        >
                          {roleOptionsForMember(memberRecord.role).map((candidate) => (
                            <option key={candidate} value={candidate}>{candidate}</option>
                          ))}
                        </select>
                      </div>
                      <div className="mobile-data-actions">
                        <button
                          className="action-btn"
                          disabled={updatingMemberId === memberRecord.id || member?.id === memberRecord.id || (memberRoles[memberRecord.id] || memberRecord.role) === memberRecord.role}
                          onClick={() => handleUpdateMemberRole(memberRecord.id, memberRecord.role)}
                        >
                          {updatingMemberId === memberRecord.id ? 'SAVING...' : 'SAVE ROLE'}
                        </button>
                        <button
                          className="action-btn destructive"
                          disabled={removingMemberId === memberRecord.id || member?.id === memberRecord.id}
                          onClick={() => handleRemoveMember(memberRecord.id)}
                        >
                          {removingMemberId === memberRecord.id ? 'REMOVING...' : 'REMOVE'}
                        </button>
                      </div>
                      {member?.id === memberRecord.id && (
                        <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                          You cannot change or remove your own membership from this screen.
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>No members yet.</div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Membership administration is restricted to workspace owners and admins.
            </div>
          )}
        </div>

        <div className="cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>INVITATIONS</div>
          {canManageMemberships ? (
            <>
              <div style={{ display: 'grid', gap: '1rem' }}>
                <div>
                  <div className="label-text">EMAIL</div>
                  <input className="control-input" value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} placeholder="teammate@example.com" />
                </div>
                <div>
                  <div className="label-text">DISPLAY NAME</div>
                  <input className="control-input" value={inviteDisplayName} onChange={(e) => setInviteDisplayName(e.target.value)} placeholder="Optional" />
                </div>
                <div>
                  <div className="label-text">ROLE</div>
                  <select className="control-input" value={inviteRole} onChange={(e) => setInviteRole(e.target.value as typeof assignableInviteRoles[number])}>
                    {visibleInviteRoles.map((candidate) => (
                      <option key={candidate} value={candidate}>{candidate}</option>
                    ))}
                  </select>
                </div>
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.5 }}>
                  Entering an email here does not send mail automatically. It creates an invite token for manual sharing.
                </div>
                <button className="btn-primary" disabled={creatingInvite} onClick={handleCreateInvite}>
                  {creatingInvite ? 'CREATING...' : 'CREATE INVITE'}
                </button>
              </div>

              <div style={{ marginTop: '2rem' }}>
                {invites.length > 0 ? (
                  <div className="mobile-data-list">
                    {invites.map((invite) => (
                      <div key={invite.id} className="mobile-data-card">
                        <div className="mobile-data-card-header">
                          <div>
                            <div className="mobile-data-title">{invite.display_name || invite.email}</div>
                            <div className="mobile-data-subtitle">{invite.email}</div>
                          </div>
                          <span className="badge">{invite.role.toUpperCase()}</span>
                        </div>
                        <div className="mobile-data-meta">
                          <div><span className="label-text">EXPIRES</span> <span>{formatDate(invite.expires_at)}</span></div>
                          <div><span className="label-text">STATUS</span> <span>{invite.status}</span></div>
                        </div>
                        <div className="mobile-data-actions">
                          <button className="action-btn destructive" onClick={() => handleRevokeInvite(invite.id)}>REVOKE</button>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                    No pending invitations.
                  </div>
                )}
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Invitation management is restricted to workspace owners and admins.
            </div>
          )}
        </div>
      </div>

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>TOP KEY ACTIVITY THIS MONTH</div>
          {canViewUsage ? (
            usageSummary.topKeys.length > 0 ? (
              <div className="responsive-scroll-x">
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th>KEY</th>
                      <th>REQUESTS</th>
                      <th>TOKENS</th>
                      <th>SUCCESS</th>
                      <th>ERRORS</th>
                    </tr>
                  </thead>
                  <tbody>
                    {usageSummary.topKeys.map((entry) => (
                      <tr key={entry.keyId}>
                        <td className="mono">{entry.keyId}</td>
                        <td>{formatCount(entry.requests)}</td>
                        <td>{formatCount(entry.tokens)}</td>
                        <td>{formatCount(entry.successes)}</td>
                        <td>{formatCount(entry.errors)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                Key-level usage will appear here once this workspace records traffic.
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted for this role.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
