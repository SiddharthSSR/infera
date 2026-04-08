import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import {
  fetchApiKeys,
  createApiKey,
  revokeApiKey,
  fetchAuditUsage,
  fetchProviders,
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
import type { ProviderStatus } from '../types';
import { useAuthSession } from '../lib/auth-context';
import { GridRow, Cell, LabelText, Badge, ActionButton, ControlInput, ControlSelect, ProgressBar, StatusDot } from '../components/shared';
import { WorkspaceSkeleton } from '../components/skeletons';
import { MetadataList } from '../components/MetadataList';
import { inviteStatusMeta, memberStatusMeta, normalizeInviteStatus } from '../lib/workspaceLifecycle';
import { buildWorkspaceActivityItems } from '../lib/workspaceActivity';
import { formatDateTime, formatCount, formatPercent, clampPercent, usageRatio, parseNullableLimit, monthRange } from '../lib/formatting';
import { capabilityLabels, providerLiveState } from '../lib/labels';

const assignableInviteRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const serviceAccountRoles = ['operator', 'developer', 'read_only', 'billing'] as const;
const workspaceMemberRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
type ProviderOptionField = {
  key: string;
  label: string;
  placeholder: string;
  defaultValue?: string;
  required?: boolean;
};

type ConfigurableProvider = {
  id: 'runpod' | 'vastai' | 'e2e';
  name: string;
  endpointPlaceholder: string;
  apiSecretLabel?: string;
  apiSecretPlaceholder?: string;
  optionFields?: ProviderOptionField[];
};

const configurableProviders: ConfigurableProvider[] = [
  { id: 'runpod', name: 'RunPod', endpointPlaceholder: 'https://api.runpod.io/graphql' },
  { id: 'vastai', name: 'Vast.ai', endpointPlaceholder: 'Optional custom endpoint' },
  {
    id: 'e2e',
    name: 'E2E TIR',
    endpointPlaceholder: 'https://tir.e2enetworks.com/api/v1',
    apiSecretLabel: 'AUTH TOKEN',
    apiSecretPlaceholder: 'Write-only auth token',
    optionFields: [
      { key: 'active_iam', label: 'ACTIVE IAM', placeholder: 'Required TIR IAM identifier', required: true },
      { key: 'team_id', label: 'TEAM ID', placeholder: 'Required team id', required: true },
      { key: 'project_id', label: 'PROJECT ID', placeholder: 'Required project id', required: true },
      { key: 'location', label: 'LOCATION', placeholder: 'Delhi', defaultValue: 'Delhi' },
      { key: 'image_type', label: 'IMAGE TYPE', placeholder: 'public', defaultValue: 'public' },
      { key: 'enable_ssh', label: 'ENABLE SSH', placeholder: 'true or false' },
      { key: 'ingress_host', label: 'INGRESS HOST', placeholder: 'Optional routable host override' },
      { key: 'worker_address', label: 'WORKER ADDRESS', placeholder: 'Optional host:port override' },
    ],
  },
];

function buildProviderOptionDefaults(providerId: ConfigurableProvider['id']): Record<string, string> {
  const provider = configurableProviders.find((candidate) => candidate.id === providerId);
  return Object.fromEntries(
    (provider?.optionFields || [])
      .filter((field) => field.defaultValue != null)
      .map((field) => [field.key, field.defaultValue ?? ''] as const),
  );
}

function validateProviderConfigDraft(
  provider: ConfigurableProvider,
  apiKey: string,
  apiSecret: string,
  options: Record<string, string>,
): string | null {
  if (!apiKey.trim()) {
    return 'Provider API key is required.';
  }
  if (provider.id === 'e2e' && !apiSecret.trim()) {
    return 'E2E auth token is required.';
  }
  for (const field of provider.optionFields || []) {
    if (field.required && !options[field.key]?.trim()) {
      return `${field.label} is required for ${provider.name}.`;
    }
  }
  return null;
}

// formatDate in WorkspaceAdmin uses datetime format — alias for backwards compat
const formatDate = formatDateTime;

export function WorkspaceAdmin() {
  const { session } = useAuthSession();
  const navigate = useNavigate();
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
  const [providerStatuses, setProviderStatuses] = useState<ProviderStatus[]>([]);
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);

  const [requestLimit, setRequestLimit] = useState('');
  const [tokenLimit, setTokenLimit] = useState('');
  const [enforceHardLimits, setEnforceHardLimits] = useState(true);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteDisplayName, setInviteDisplayName] = useState('');
  const [inviteRole, setInviteRole] = useState<typeof assignableInviteRoles[number]>('developer');
  const [newServiceAccountName, setNewServiceAccountName] = useState('');
  const [newServiceAccountRole, setNewServiceAccountRole] = useState<typeof serviceAccountRoles[number]>('operator');
  const [selectedProvider, setSelectedProvider] = useState<ConfigurableProvider['id']>('runpod');
  const [providerAPIKey, setProviderAPIKey] = useState('');
  const [providerAPISecret, setProviderAPISecret] = useState('');
  const [providerEndpoint, setProviderEndpoint] = useState('');
  const [providerOptions, setProviderOptions] = useState<Record<string, string>>(buildProviderOptionDefaults('runpod'));
  const [memberRoles, setMemberRoles] = useState<Record<string, string>>({});
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);
  const [createdInviteToken, setCreatedInviteToken] = useState<string | null>(null);
  const [savingQuota, setSavingQuota] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [creatingServiceAccount, setCreatingServiceAccount] = useState(false);
  const [savingProviderConfig, setSavingProviderConfig] = useState(false);
  const [updatingMemberId, setUpdatingMemberId] = useState<string | null>(null);
  const [removingMemberId, setRemovingMemberId] = useState<string | null>(null);
  const [settingsTab, setSettingsTab] = useState<'usage' | 'providers' | 'service' | 'members' | 'invites'>('usage');
  const createdInviteLink = createdInviteToken && typeof window !== 'undefined'
    ? `${window.location.origin}/accept-invite?token=${encodeURIComponent(createdInviteToken)}`
    : null;

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

  const requestUsageRatio = usageRatio(usageSummary.requests, quota?.monthly_request_limit);
  const tokenUsageRatio = usageRatio(usageSummary.tokens, quota?.monthly_token_limit);
  const quotaPressure = Math.max(requestUsageRatio, tokenUsageRatio);
  const quotaState = quotaPressure >= 1 ? 'EXCEEDED' : quotaPressure >= 0.8 ? 'NEAR LIMIT' : 'HEALTHY';
  const quotaStateClass = quotaPressure >= 1 ? 'error' : quotaPressure >= 0.8 ? 'warning' : '';
  const selectedProviderMeta = configurableProviders.find((provider) => provider.id === selectedProvider) || configurableProviders[0];
  const providerHealthRows = useMemo(() => {
    const configsByProvider = new Map(providerConfigs.map((config) => [config.provider, config]));
    const statusesByProvider = new Map(providerStatuses.map((status) => [status.provider, status]));

    return configurableProviders.map((provider) => {
      const config = configsByProvider.get(provider.id);
      const status = statusesByProvider.get(provider.id);
      const liveState = providerLiveState(status, config?.configured);

      return {
        id: provider.id,
        name: provider.name,
        config,
        status,
        liveState,
        capabilities: capabilityLabels(status?.capabilities),
      };
    });
  }, [providerConfigs, providerStatuses]);
  useEffect(() => {
    const existing = providerConfigs.find((config) => config.provider === selectedProvider);
    setProviderEndpoint(existing?.endpoint || '');
    setProviderOptions({
      ...buildProviderOptionDefaults(selectedProvider),
      ...(existing?.options || {}),
    });
  }, [providerConfigs, selectedProvider]);
  const memberCounts = useMemo(() => ({
    total: members.length,
    admins: members.filter((record) => record.role === 'admin' || record.role === 'owner').length,
    operators: members.filter((record) => record.role === 'operator').length,
  }), [members]);
  const pendingInvites = useMemo(
    () => invites.filter((invite) => normalizeInviteStatus(invite) === 'pending'),
    [invites],
  );
  const inviteHistory = useMemo(
    () => invites.filter((invite) => normalizeInviteStatus(invite) !== 'pending'),
    [invites],
  );
  const inviteCounts = useMemo(() => ({
    pending: pendingInvites.length,
    accepted: invites.filter((invite) => normalizeInviteStatus(invite) === 'accepted').length,
    expired: invites.filter((invite) => normalizeInviteStatus(invite) === 'expired').length,
    revoked: invites.filter((invite) => normalizeInviteStatus(invite) === 'revoked').length,
  }), [invites, pendingInvites.length]);
  const workspaceProfileItems = useMemo(
    () => [
      { label: 'MEMBERS', value: String(members.length), mono: true },
      { label: 'PENDING INVITES', value: String(pendingInvites.length), mono: true },
      { label: 'SERVICE ACCOUNTS', value: String(serviceAccounts.length), mono: true },
      {
        label: 'LIVE PROVIDERS',
        value: String(providerHealthRows.filter((provider) => provider.liveState.label === 'CONNECTED').length),
        mono: true,
      },
    ],
    [members.length, pendingInvites.length, providerHealthRows, serviceAccounts.length],
  );
  const workspaceActivity = useMemo(
    () => buildWorkspaceActivityItems({
      members,
      invites,
      serviceAccounts,
      providerConfigs,
    }),
    [members, invites, serviceAccounts, providerConfigs],
  );
  const teamActivity = useMemo(
    () => workspaceActivity.filter((item) => item.category === 'team').slice(0, 6),
    [workspaceActivity],
  );
  const accessActivity = useMemo(
    () => workspaceActivity.filter((item) => item.category === 'access').slice(0, 6),
    [workspaceActivity],
  );

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
      tasks.push(
        fetchProviders()
          .then((statuses) => setProviderStatuses(statuses.filter((status) => status.provider !== 'mock' && status.provider !== 'lambda')))
          .catch(() => setProviderStatuses([])),
      );
    } else {
      setProviderConfigs([]);
      setProviderStatuses([]);
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
    const validationError = validateProviderConfigDraft(
      selectedProviderMeta,
      providerAPIKey,
      providerAPISecret,
      providerOptions,
    );
    if (validationError) {
      toast.error(validationError);
      return;
    }
    setSavingProviderConfig(true);
    try {
      await upsertWorkspaceProviderConfig(workspaceId, selectedProvider, {
        api_key: providerAPIKey.trim(),
        api_secret: providerAPISecret.trim() || undefined,
        endpoint: providerEndpoint.trim() || undefined,
        options: providerOptions,
      });
      setProviderAPIKey('');
      setProviderAPISecret('');
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

  if (loading) return <WorkspaceSkeleton />;

  return (
    <div className="workspace-page animate-fade-in">
      {createdInviteToken && (
        <div style={{
          padding: '1.25rem 2rem',
          backgroundColor: '#E8F5E9',
          borderBottom: 'var(--grid-line)',
        }}>
          <LabelText as="div" style={{ marginBottom: '0.6rem' }}>INVITATION TOKEN — COPY NOW</LabelText>
          <div className="code-block" style={{ marginTop: 0 }}>{createdInviteToken}</div>
          <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.5 }}>
            This does not send an email yet. Share the token or the <code style={{ fontFamily: 'var(--font-mono)' }}>/accept-invite</code> link manually with the person you invited.
          </div>
          {createdInviteLink && (
            <div className="code-block" style={{ marginTop: '0.75rem' }}>{createdInviteLink}</div>
          )}
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <ActionButton variant="primary" onClick={() => navigator.clipboard.writeText(createdInviteToken).then(() => toast.success('Invitation token copied.'))}>COPY TOKEN</ActionButton>
            {createdInviteLink && (
              <ActionButton variant="secondary" onClick={() => navigator.clipboard.writeText(createdInviteLink).then(() => toast.success('Invitation link copied.'))}>COPY LINK</ActionButton>
            )}
            <ActionButton variant="secondary" onClick={() => setCreatedInviteToken(null)}>DISMISS</ActionButton>
          </div>
        </div>
      )}

      {createdSecret && (
        <div style={{
          padding: '1.25rem 2rem',
          backgroundColor: '#E8F5E9',
          borderBottom: 'var(--grid-line)',
        }}>
          <LabelText as="div" style={{ marginBottom: '0.6rem' }}>SERVICE ACCOUNT KEY — COPY NOW</LabelText>
          <div className="code-block" style={{ marginTop: 0 }}>{createdSecret}</div>
          <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <ActionButton variant="primary" onClick={() => navigator.clipboard.writeText(createdSecret).then(() => toast.success('Service account key copied.'))}>COPY KEY</ActionButton>
            <ActionButton variant="secondary" onClick={() => setCreatedSecret(null)}>DISMISS</ActionButton>
          </div>
        </div>
      )}

      <GridRow className="workspace-hero-row">
        <Cell span={2} className="workspace-profile-cell">
          <LabelText as="div" style={{ marginBottom: '1rem' }}>WORKSPACE PROFILE</LabelText>
          <h2 style={{ fontSize: '2rem', lineHeight: 1.1 }}>{session?.workspace?.name || 'Workspace'}</h2>
          <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <Badge>{role.toUpperCase()}</Badge>
            <Badge>{session?.key?.principal_type === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN'}</Badge>
            {session?.workspace?.slug && <Badge mono>{session.workspace.slug}</Badge>}
          </div>
          <div style={{ marginTop: '1.1rem' }}>
            <MetadataList
              items={workspaceProfileItems}
              columns={2}
            />
          </div>
          <div style={{ marginTop: '1.1rem', color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.6 }}>
            {member?.email
              ? `Signed in as ${member.email}.`
              : 'Signed in with a workspace-scoped key.'}
            <br />
            Workspace administration is gated by the backend role model already enforced on auth, quota, infrastructure, and audit routes.
          </div>
        </Cell>
        <Cell span={2} className="workspace-access-cell" bg="var(--bg-accent)">
          <LabelText as="div" style={{ marginBottom: '1rem' }}>ACCESS SURFACE</LabelText>
          <div style={{ display: 'grid', gap: '0.8rem', fontSize: '0.9rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage memberships</span><span className="mono">{canManageMemberships ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage service accounts</span><span className="mono">{canManageKeys ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage provider configs</span><span className="mono">{canManageProviderConfigs ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage quota</span><span className="mono">{canManageQuota ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>View quota</span><span className="mono">{canViewQuota ? 'YES' : 'NO'}</span></div>
          </div>
          <div className="help-callout" style={{ marginTop: '1rem', padding: '0.95rem 1rem' }}>
            <LabelText as="div">WORKSPACE ADMIN GUIDE</LabelText>
            <div className="help-callout-copy">
              Invite creation still produces a manual share token or link, not an email. Service accounts are for automation in this workspace only. Provider states here mean: <strong>connected</strong> for healthy credentials and live status, <strong>degraded</strong> for reachable but unhealthy, and <strong>auth failed</strong> when the saved credentials are rejected.
            </div>
          </div>
        </Cell>
      </GridRow>

      <GridRow>
        <Cell span={4} style={{ paddingTop: '1.25rem', paddingBottom: '1.25rem' }}>
          <LabelText as="div">SETTINGS SECTIONS</LabelText>
          <div className="chip-row" style={{ marginTop: '0.85rem' }}>
            {[
              ['usage', 'Usage & Quota'],
              ['providers', 'Providers'],
              ['service', 'Service Accounts'],
              ['members', 'Members'],
              ['invites', 'Invites'],
            ].map(([key, label]) => (
              <ActionButton
                key={key}
                variant={settingsTab === key ? 'primary' : 'secondary'}
                onClick={() => setSettingsTab(key as typeof settingsTab)}
              >
                {label}
              </ActionButton>
            ))}
          </div>
          <div style={{ marginTop: '0.85rem', color: 'var(--text-secondary)', fontSize: '0.86rem', lineHeight: 1.6 }}>
            Use the active section to focus the admin surface. This keeps usage, provider credentials, automation identity management, and access control from competing on one long page.
          </div>
        </Cell>
      </GridRow>

      {settingsTab === 'usage' && (
      <GridRow className="workspace-usage-row">
        <Cell span={2} className="workspace-usage-cell">
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>CURRENT MONTH USAGE</LabelText>
          {canViewUsage ? (
            <>
              <div className="workspace-usage-summary" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '1rem' }}>
                <div style={{ padding: '1rem', backgroundColor: 'var(--bg-accent)' }}>
                  <LabelText as="div">REQUESTS</LabelText>
                  <div style={{ fontSize: '2rem', marginTop: '0.5rem' }}>{formatCount(usageSummary.requests)}</div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                    {formatCount(usageSummary.successes)} success / {formatCount(usageSummary.errors)} error
                  </div>
                </div>
                <div style={{ padding: '1rem', backgroundColor: 'var(--bg-accent)' }}>
                  <LabelText as="div">TOKENS</LabelText>
                  <div style={{ fontSize: '2rem', marginTop: '0.5rem' }}>{formatCount(usageSummary.tokens)}</div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                    Aggregated from workspace audit records
                  </div>
                </div>
              </div>

              <div style={{ marginTop: '1.5rem', display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
                <StatusDot tone={(quotaStateClass || 'success') as 'success' | 'warning' | 'error'} />
                <LabelText>QUOTA STATE</LabelText>
                <Badge>{quotaState}</Badge>
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
                <ProgressBar value={clampPercent(requestUsageRatio)} warnAt={80} errorAt={100} />
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
                <ProgressBar value={clampPercent(tokenUsageRatio)} warnAt={80} errorAt={100} />
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted to workspace owners, admins, billing, and read-only roles.
            </div>
          )}
        </Cell>

        <Cell span={2} className="workspace-trend-cell" bg="var(--bg-accent)">
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>RECENT DAILY TREND</LabelText>
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
                      <Badge mono>{formatCount(entry.tokens)} TOKENS</Badge>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                No usage recorded for this workspace in the current month yet.
                <div className="help-actions">
                  <ActionButton onClick={() => navigate('/models')}>DEPLOY A MODEL</ActionButton>
                  <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
                </div>
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted for this role.
            </div>
          )}
        </Cell>
      </GridRow>
      )}

      {(settingsTab === 'providers' || settingsTab === 'service') && (
      <GridRow className="workspace-ops-row">
        <Cell span={2} className="workspace-provider-cell">
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>PROVIDER CONFIGS</LabelText>
          {canManageProviderConfigs ? (
            <>
              <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th scope="col">PROVIDER</th>
                      <th scope="col">CONFIG</th>
                      <th scope="col">LIVE STATE</th>
                      <th scope="col">ENDPOINT</th>
                      <th scope="col">ACTIVE</th>
                      <th scope="col">UPDATED</th>
                      <th scope="col" style={{ textAlign: 'right' }}>ACTION</th>
                    </tr>
                  </thead>
                  <tbody>
                    {providerHealthRows.map((provider) => (
                      <tr key={provider.id}>
                        <td>{provider.name}</td>
                        <td><Badge>{provider.config?.configured ? 'CONFIGURED' : 'NOT CONFIGURED'}</Badge></td>
                        <td>
                          <Badge tone={provider.liveState.tone as 'warning' | 'error' | '' | undefined || undefined}>
                            {provider.liveState.label}
                          </Badge>
                        </td>
                        <td className="mono">{provider.config ? (provider.config.endpoint || 'default') : '—'}</td>
                        <td>{provider.status?.active_instances ?? 0}</td>
                        <td>{provider.config ? formatDate(provider.config.updated_at) : '—'}</td>
                        <td style={{ textAlign: 'right' }}>
                          {provider.config ? (
                            <ActionButton variant="destructive" onClick={() => handleDeleteProviderConfig(provider.id)}>DELETE</ActionButton>
                          ) : (
                            <span style={{ color: 'var(--text-secondary)', fontSize: '0.8rem' }}>—</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="workspace-provider-health-grid" style={{ display: 'grid', gap: '1rem', marginBottom: '1.5rem' }}>
                {providerHealthRows.map((provider) => (
                  <div key={`${provider.id}-health`} className="workspace-provider-card">
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                      <div>
                        <LabelText as="div" style={{ marginBottom: '0.5rem' }}>{provider.name.toUpperCase()}</LabelText>
                        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                          <Badge>{provider.config?.configured ? 'CONFIGURED' : 'NOT CONFIGURED'}</Badge>
                          <Badge tone={provider.liveState.tone as 'warning' | 'error' | '' | undefined || undefined}>{provider.liveState.label}</Badge>
                        </div>
                      </div>
                      <div className="mono" style={{ color: 'var(--text-secondary)' }}>
                        {provider.status?.account_id || provider.config?.endpoint || (provider.config ? 'default endpoint' : 'not configured')}
                      </div>
                    </div>

                    <div style={{ marginTop: '1rem', color: 'var(--text-secondary)', fontSize: '0.88rem', lineHeight: 1.6 }}>
                      {provider.liveState.detail}
                    </div>

                    <div className="workspace-provider-meta" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '0.9rem', marginTop: '1rem' }}>
                      <div>
                        <LabelText as="div">ACTIVE INSTANCES</LabelText>
                        <div className="mono" style={{ marginTop: '0.4rem' }}>{provider.status?.active_instances ?? 0}</div>
                      </div>
                      <div>
                        <LabelText as="div">REGIONS</LabelText>
                        <div style={{ marginTop: '0.4rem', fontSize: '0.88rem', color: 'var(--text-secondary)' }}>
                          {provider.status?.capabilities?.known_regions?.length
                            ? provider.status.capabilities.known_regions.join(', ')
                            : 'Default'}
                        </div>
                      </div>
                      <div>
                        <LabelText as="div">BILLING SIGNAL</LabelText>
                        <div className="mono" style={{ marginTop: '0.4rem' }}>
                          {provider.status?.balance != null ? `$${provider.status.balance.toFixed(2)}` : '—'}
                        </div>
                      </div>
                    </div>

                    <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                      {provider.capabilities.length > 0 ? (
                        provider.capabilities.map((capability) => (
                          <Badge key={`${provider.id}-${capability}`}>{capability}</Badge>
                        ))
                      ) : (
                        <span style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>Capabilities will appear when live provider status is available.</span>
                      )}
                    </div>
                  </div>
                ))}
              </div>

              <div style={{ display: 'grid', gap: '1rem' }}>
                <div>
                  <LabelText as="div">PROVIDER</LabelText>
                  <ControlSelect value={selectedProvider} onChange={(e) => setSelectedProvider(e.target.value as ConfigurableProvider['id'])}>
                    {configurableProviders.map((provider) => (
                      <option key={provider.id} value={provider.id}>{provider.name}</option>
                    ))}
                  </ControlSelect>
                </div>
                <div>
                  <LabelText as="div">API KEY</LabelText>
                  <ControlInput type="password" value={providerAPIKey} onChange={(e) => setProviderAPIKey(e.target.value)} placeholder="Write-only" />
                </div>
                <div>
                  <LabelText as="div">{selectedProviderMeta.apiSecretLabel || 'API SECRET'}</LabelText>
                  <ControlInput type="password" value={providerAPISecret} onChange={(e) => setProviderAPISecret(e.target.value)} placeholder={selectedProviderMeta.apiSecretPlaceholder || 'Optional write-only secret'} />
                </div>
                <div>
                  <LabelText as="div">ENDPOINT</LabelText>
                  <ControlInput value={providerEndpoint} onChange={(e) => setProviderEndpoint(e.target.value)} placeholder={selectedProviderMeta.endpointPlaceholder} />
                </div>
                {(selectedProviderMeta.optionFields || []).map((field) => (
                  <div key={field.key}>
                    <LabelText as="div">{field.label}</LabelText>
                    <ControlInput
                      value={providerOptions[field.key] || ''}
                      onChange={(e) => setProviderOptions((current) => ({ ...current, [field.key]: e.target.value }))}
                      placeholder={field.placeholder}
                    />
                  </div>
                ))}
                {selectedProvider === 'e2e' && (
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.6 }}>
                    E2E requires an API key, auth token, and the target IAM/team/project identifiers. Leave endpoint blank to use the default TIR API base, and keep location set unless your project is pinned elsewhere.
                  </div>
                )}
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                  Stored secrets are never shown again after save. Update a provider by submitting a new key or token for the selected provider. Non-secret options reload when you revisit the provider.
                </div>
                <div>
                  <ActionButton variant="primary" disabled={savingProviderConfig} onClick={handleSaveProviderConfig}>
                    {savingProviderConfig ? 'SAVING...' : 'SAVE PROVIDER CONFIG'}
                  </ActionButton>
                </div>
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Provider configuration is restricted to workspace owners and admins.
            </div>
          )}
        </Cell>

        <Cell span={2} className="workspace-quota-cell">
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>WORKSPACE QUOTA</LabelText>
          {canViewQuota && quota ? (
            <>
              <div className="workspace-quota-inputs" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>
                <div>
                  <LabelText as="div">MONTHLY REQUEST LIMIT</LabelText>
                  <ControlInput value={requestLimit} disabled={!canManageQuota} onChange={(e) => setRequestLimit(e.target.value)} placeholder="Unlimited" />
                </div>
                <div>
                  <LabelText as="div">MONTHLY TOKEN LIMIT</LabelText>
                  <ControlInput value={tokenLimit} disabled={!canManageQuota} onChange={(e) => setTokenLimit(e.target.value)} placeholder="Unlimited" />
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
                <ActionButton variant="primary" style={{ marginTop: '1.25rem' }} disabled={savingQuota} onClick={handleSaveQuota}>
                  {savingQuota ? 'SAVING...' : 'SAVE QUOTA'}
                </ActionButton>
              )}
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              You do not have permission to view quota settings for this workspace.
            </div>
          )}
        </Cell>

        <Cell span={2} className="workspace-service-cell">
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>SERVICE ACCOUNTS</LabelText>
          {canManageKeys ? (
            <>
              <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th scope="col">NAME</th>
                      <th scope="col">ROLE</th>
                      <th scope="col">PREFIX</th>
                      <th scope="col">LAST USED</th>
                      <th scope="col" style={{ textAlign: 'right' }}>ACTION</th>
                    </tr>
                  </thead>
                  <tbody>
                    {serviceAccounts.map((key) => (
                      <tr key={key.id}>
                        <td>{key.name}</td>
                        <td><Badge>{key.role.toUpperCase()}</Badge></td>
                        <td className="mono">{key.key_prefix}</td>
                        <td>{formatDate(key.last_used)}</td>
                        <td style={{ textAlign: 'right' }}>
                          <ActionButton variant="destructive" onClick={() => handleRevokeServiceAccount(key.id)}>REVOKE</ActionButton>
                        </td>
                      </tr>
                    ))}
                    {serviceAccounts.length === 0 && (
                      <tr>
                        <td colSpan={5} style={{ color: 'var(--text-secondary)', padding: '1.5rem 0' }}>
                          No service accounts yet.
                          <div className="help-actions" style={{ justifyContent: 'center' }}>
                            <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="ci-bot"]')?.focus()}>CREATE ONE</ActionButton>
                            <ActionButton onClick={() => navigate('/api-keys')}>OPEN API KEYS</ActionButton>
                          </div>
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>

              <div className="workspace-service-create-row" style={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr auto', gap: '1rem', alignItems: 'end' }}>
                <div>
                  <LabelText as="div">NAME</LabelText>
                  <ControlInput value={newServiceAccountName} onChange={(e) => setNewServiceAccountName(e.target.value)} placeholder="ci-bot" />
                </div>
                <div>
                  <LabelText as="div">ROLE</LabelText>
                  <ControlSelect value={newServiceAccountRole} onChange={(e) => setNewServiceAccountRole(e.target.value as typeof serviceAccountRoles[number])}>
                    {serviceAccountRoles.map((candidate) => (
                      <option key={candidate} value={candidate}>{candidate}</option>
                    ))}
                  </ControlSelect>
                </div>
                <ActionButton variant="primary" disabled={creatingServiceAccount} onClick={handleCreateServiceAccount}>
                  {creatingServiceAccount ? 'CREATING...' : 'CREATE'}
                </ActionButton>
              </div>
            </>
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Service account management is restricted to workspace owners and admins.
            </div>
          )}
        </Cell>
      </GridRow>
      )}

      {(settingsTab === 'members' || settingsTab === 'invites') && (
      <div className="grid-row workspace-members-row" style={{ alignItems: 'start' }}>
        <div className="cell workspace-members-cell" style={{ gridColumn: 'span 2' }}>
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>MEMBERS</LabelText>
          <div className="workspace-lifecycle-summary">
            <Badge>TOTAL {memberCounts.total}</Badge>
            <Badge>ADMINS {memberCounts.admins}</Badge>
            <Badge>OPERATORS {memberCounts.operators}</Badge>
          </div>
          {canManageMemberships ? (
            members.length > 0 ? (
              <div className="mobile-data-list">
                {members.map((memberRecord) => {
                  const status = memberStatusMeta(memberRecord, member?.id);
                  return (
                    <div key={memberRecord.id} className="mobile-data-card">
                      <div className="mobile-data-card-header">
                        <div>
                          <div className="mobile-data-title">{memberRecord.display_name}</div>
                          <div className="mobile-data-subtitle">{memberRecord.email}</div>
                        </div>
                        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                          <Badge>{(memberRoles[memberRecord.id] || memberRecord.role).toUpperCase()}</Badge>
                          <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                        </div>
                      </div>
                      <div className="mobile-data-meta">
                        <div><LabelText>ACCESS</LabelText> <span>{status.detail}</span></div>
                        <div><LabelText>JOINED</LabelText> <span>{formatDate(memberRecord.created_at)}</span></div>
                      </div>
                      <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
                        <div>
                          <LabelText as="div">ROLE</LabelText>
                          <ControlSelect
                            value={memberRoles[memberRecord.id] || memberRecord.role}
                            disabled={member?.id === memberRecord.id}
                            onChange={(e) => setMemberRoles((current) => ({ ...current, [memberRecord.id]: e.target.value }))}
                          >
                            {roleOptionsForMember(memberRecord.role).map((candidate) => (
                              <option key={candidate} value={candidate}>{candidate}</option>
                            ))}
                          </ControlSelect>
                        </div>
                        <div className="mobile-data-actions">
                          <ActionButton
                            disabled={updatingMemberId === memberRecord.id || member?.id === memberRecord.id || (memberRoles[memberRecord.id] || memberRecord.role) === memberRecord.role}
                            onClick={() => handleUpdateMemberRole(memberRecord.id, memberRecord.role)}
                          >
                            {updatingMemberId === memberRecord.id ? 'SAVING...' : 'SAVE ROLE'}
                          </ActionButton>
                          <ActionButton
                            variant="destructive"
                            disabled={removingMemberId === memberRecord.id || member?.id === memberRecord.id}
                            onClick={() => handleRemoveMember(memberRecord.id)}
                          >
                            {removingMemberId === memberRecord.id ? 'REMOVING...' : 'REMOVE'}
                          </ActionButton>
                        </div>
                        {member?.id === memberRecord.id && (
                          <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                            You cannot change or remove your own membership from this screen.
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                No members yet.
                <div className="help-actions">
                  <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE INVITE</ActionButton>
                  <ActionButton onClick={() => navigate('/docs')}>READ TEAM ACCESS DOCS</ActionButton>
                </div>
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Membership administration is restricted to workspace owners and admins.
            </div>
          )}
        </div>

        <div className="cell workspace-invites-cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>INVITATIONS</LabelText>
          <div className="workspace-lifecycle-summary">
            <Badge>PENDING {inviteCounts.pending}</Badge>
            <Badge>ACCEPTED {inviteCounts.accepted}</Badge>
            <Badge>EXPIRED {inviteCounts.expired}</Badge>
            <Badge>REVOKED {inviteCounts.revoked}</Badge>
          </div>
          {canManageMemberships ? (
            <>
              <div style={{ display: 'grid', gap: '1rem' }}>
                <div>
                  <LabelText as="div">EMAIL</LabelText>
                  <ControlInput value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} placeholder="teammate@example.com" />
                </div>
                <div>
                  <LabelText as="div">DISPLAY NAME</LabelText>
                  <ControlInput value={inviteDisplayName} onChange={(e) => setInviteDisplayName(e.target.value)} placeholder="Optional" />
                </div>
                <div>
                  <LabelText as="div">ROLE</LabelText>
                  <ControlSelect value={inviteRole} onChange={(e) => setInviteRole(e.target.value as typeof assignableInviteRoles[number])}>
                    {visibleInviteRoles.map((candidate) => (
                      <option key={candidate} value={candidate}>{candidate}</option>
                    ))}
                  </ControlSelect>
                </div>
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.5 }}>
                  Entering an email here does not send mail automatically. It creates an invite token for manual sharing.
                </div>
                <ActionButton variant="primary" disabled={creatingInvite} onClick={handleCreateInvite}>
                  {creatingInvite ? 'CREATING...' : 'CREATE INVITE'}
                </ActionButton>
              </div>

              <div style={{ marginTop: '2rem' }}>
                {pendingInvites.length > 0 ? (
                  <div className="mobile-data-list">
                    {pendingInvites.map((invite) => {
                      const status = inviteStatusMeta(invite);
                      return (
                      <div key={invite.id} className="mobile-data-card">
                        <div className="mobile-data-card-header">
                          <div>
                            <div className="mobile-data-title">{invite.display_name || invite.email}</div>
                            <div className="mobile-data-subtitle">{invite.email}</div>
                          </div>
                          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                            <Badge>{invite.role.toUpperCase()}</Badge>
                            <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                          </div>
                        </div>
                        <div className="mobile-data-meta">
                          <div><LabelText>EXPIRES</LabelText> <span>{formatDate(invite.expires_at)}</span></div>
                          <div><LabelText>STATE</LabelText> <span>{status.detail}</span></div>
                        </div>
                        <div className="mobile-data-actions">
                          <ActionButton variant="destructive" onClick={() => handleRevokeInvite(invite.id)}>REVOKE</ActionButton>
                        </div>
                      </div>
                      );
                    })}
                  </div>
                ) : (
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                    No pending invitations. Accepted, expired, and revoked invites appear in history below.
                    <div className="help-actions">
                      <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE INVITE</ActionButton>
                      <ActionButton onClick={() => navigate('/docs')}>READ INVITE FLOW</ActionButton>
                    </div>
                  </div>
                )}
              </div>

              <div style={{ marginTop: '2rem' }}>
                <LabelText as="div" style={{ marginBottom: '1rem' }}>INVITE HISTORY</LabelText>
                {inviteHistory.length > 0 ? (
                  <div className="mobile-data-list">
                    {inviteHistory.map((invite) => {
                      const status = inviteStatusMeta(invite);
                      return (
                        <div key={invite.id} className="mobile-data-card">
                          <div className="mobile-data-card-header">
                            <div>
                              <div className="mobile-data-title">{invite.display_name || invite.email}</div>
                              <div className="mobile-data-subtitle">{invite.email}</div>
                            </div>
                            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                              <Badge>{invite.role.toUpperCase()}</Badge>
                              <span className={`badge ${status.tone ? `status-${status.tone}` : ''}`.trim()}>{status.label}</span>
                            </div>
                          </div>
                          <div className="mobile-data-meta">
                            <div><LabelText>CREATED</LabelText> <span>{formatDate(invite.created_at)}</span></div>
                            <div><LabelText>FINAL STATE</LabelText> <span>{status.detail}</span></div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                Invite history will appear once invites are accepted, revoked, or expire.
                <div className="help-actions">
                  <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>CREATE FIRST INVITE</ActionButton>
                  <ActionButton onClick={() => navigate('/docs')}>READ INVITE FLOW</ActionButton>
                </div>
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
      )}

      {(settingsTab === 'members' || settingsTab === 'invites' || settingsTab === 'providers' || settingsTab === 'service') && (
      <GridRow>
        <div className="cell workspace-activity-cell" style={{ gridColumn: 'span 2' }}>
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>RECENT TEAM ACTIVITY</LabelText>
          {canManageMemberships ? (
            teamActivity.length > 0 ? (
              <div className="workspace-activity-list">
                {teamActivity.map((item) => (
                  <div key={item.id} className="workspace-activity-item">
                    <div className="workspace-activity-header">
                      <LabelText>{item.title}</LabelText>
                      <span className={`badge ${item.tone ? `status-${item.tone}` : ''}`.trim()}>{formatDate(item.timestamp)}</span>
                    </div>
                    <div className="workspace-activity-detail">{item.detail}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                Team activity will appear here once members join or invites are created.
                <div className="help-actions">
                  <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="teammate@example.com"]')?.focus()}>INVITE FIRST MEMBER</ActionButton>
                  <ActionButton onClick={() => navigate('/docs')}>READ WORKSPACE HELP</ActionButton>
                </div>
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Team lifecycle visibility is restricted to workspace owners and admins.
            </div>
          )}
        </div>

        <div className="cell workspace-activity-cell workspace-activity-accent-cell" style={{ gridColumn: 'span 2' }}>
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>ACCESS AND CONFIG ACTIVITY</LabelText>
          {(canManageKeys || canManageProviderConfigs) ? (
            accessActivity.length > 0 ? (
              <div className="workspace-activity-list">
                {accessActivity.map((item) => (
                  <div key={item.id} className="workspace-activity-item">
                    <div className="workspace-activity-header">
                      <LabelText>{item.title}</LabelText>
                      <span className={`badge ${item.tone ? `status-${item.tone}` : ''}`.trim()}>{formatDate(item.timestamp)}</span>
                    </div>
                    <div className="workspace-activity-detail">{item.detail}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                Provider updates and service-account usage will appear here once this workspace records access activity.
                <div className="help-actions">
                  <ActionButton onClick={() => navigate('/api-keys')}>OPEN API KEYS</ActionButton>
                  <ActionButton onClick={() => navigate('/docs')}>READ AUTOMATION DOCS</ActionButton>
                </div>
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Access activity is restricted to roles that can manage service accounts or provider configs.
            </div>
          )}
        </div>
      </GridRow>
      )}

      {settingsTab === 'usage' && (
      <GridRow>
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <LabelText as="div" style={{ marginBottom: '1.5rem' }}>TOP KEY ACTIVITY THIS MONTH</LabelText>
          {canViewUsage ? (
            usageSummary.topKeys.length > 0 ? (
              <>
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.88rem', lineHeight: 1.6, marginBottom: '1rem' }}>
                  This is a lightweight audit view of which keys are driving the most traffic in the active workspace this month. Use it to spot the hottest automation key, unexpected error-heavy traffic, or access concentration.
                </div>
                <div className="responsive-scroll-x">
                <table className="data-table responsive-scroll-x-content">
                  <thead>
                    <tr>
                      <th scope="col">KEY</th>
                      <th scope="col">REQUESTS</th>
                      <th scope="col">TOKENS</th>
                      <th scope="col">SUCCESS</th>
                      <th scope="col">ERRORS</th>
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
              </>
            ) : (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
                Key-level usage will appear here once this workspace records traffic.
                <div className="help-actions">
                  <ActionButton onClick={() => navigate('/models')}>DEPLOY A MODEL</ActionButton>
                  <ActionButton onClick={() => navigate('/api-keys')}>OPEN API KEYS</ActionButton>
                </div>
              </div>
            )
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              Usage visibility is restricted for this role.
            </div>
          )}
        </div>
      </GridRow>
      )}
    </div>
  );
}
