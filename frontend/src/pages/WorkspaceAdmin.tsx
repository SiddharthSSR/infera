import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { useAuthSession } from '../lib/auth-context';
import { WorkspaceMembershipSection } from '../components/workspace/WorkspaceMembershipSection';
import { WorkspaceOperationsSection } from '../components/workspace/WorkspaceOperationsSection';
import { GridRow, Cell, LabelText, Badge, ActionButton, ProgressBar, StatusDot } from '../components/shared';
import { WorkspaceSkeleton } from '../components/skeletons';
import { useWorkspaceAdminState } from '../hooks/useWorkspaceAdminState';
import { MetadataList } from '../components/MetadataList';
import { normalizeInviteStatus } from '../lib/workspaceLifecycle';
import { buildWorkspaceActivityItems } from '../lib/workspaceActivity';
import { formatDateTime, formatCount, formatPercent, clampPercent, usageRatio } from '../lib/formatting';
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
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);
  const [createdInviteToken, setCreatedInviteToken] = useState<string | null>(null);
  const [settingsTab, setSettingsTab] = useState<'usage' | 'providers' | 'service' | 'members' | 'invites'>('usage');
  const createdInviteLink = createdInviteToken && typeof window !== 'undefined'
    ? `${window.location.origin}/accept-invite?token=${encodeURIComponent(createdInviteToken)}`
    : null;

  const {
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
  } = useWorkspaceAdminState({
    workspaceId,
    role,
  });

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
  useEffect(() => {
    setRequestLimit(quota?.monthly_request_limit != null ? String(quota.monthly_request_limit) : '');
    setTokenLimit(quota?.monthly_token_limit != null ? String(quota.monthly_token_limit) : '');
    setEnforceHardLimits(quota?.enforce_hard_limits ?? true);
  }, [quota]);
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

  const roleOptionsForMember = (currentRole: string) => {
    const options = new Set<string>(visibleMemberRoles);
    options.add(currentRole);
    return Array.from(options);
  };

  const handleQuotaSave = async () => {
    await handleSaveQuota({
      requestLimit,
      tokenLimit,
      enforceHardLimits,
    });
  };

  const handleInviteCreate = async () => {
    const token = await handleCreateInvite({
      email: inviteEmail,
      displayName: inviteDisplayName,
      inviteRole,
    });
    if (!token) return;
    setCreatedInviteToken(token);
    setInviteEmail('');
    setInviteDisplayName('');
    setInviteRole(visibleInviteRoles[0]);
  };

  const handleMemberRoleSave = async (memberId: string, currentRole: string) => {
    await handleUpdateMemberRole({
      memberId,
      currentRole,
      nextRole: memberRoles[memberId] || currentRole,
    });
  };

  const handleServiceAccountCreate = async () => {
    const secret = await handleCreateServiceAccount({
      name: newServiceAccountName,
      accountRole: newServiceAccountRole,
    });
    if (!secret) return;
    setCreatedSecret(secret);
    setNewServiceAccountName('');
    setNewServiceAccountRole('operator');
  };

  const handleProviderConfigSave = async () => {
    const saved = await handleSaveProviderConfig({
      selectedProvider,
      providerLabel: selectedProviderMeta.name,
      providerAPIKey,
      providerAPISecret,
      providerEndpoint,
      providerOptions,
      validationError: validateProviderConfigDraft(
        selectedProviderMeta,
        providerAPIKey,
        providerAPISecret,
        providerOptions,
      ),
    });
    if (!saved) return;
    setProviderAPIKey('');
    setProviderAPISecret('');
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
        <WorkspaceOperationsSection
          canManageProviderConfigs={canManageProviderConfigs}
          providerHealthRows={providerHealthRows}
          formatDate={formatDate}
          onDeleteProviderConfig={(provider) => {
            if (!confirm(`Delete ${provider} provider config for this workspace?`)) return;
            void handleDeleteProviderConfig(provider);
          }}
          configurableProviders={configurableProviders}
          selectedProvider={selectedProvider}
          onSelectedProviderChange={setSelectedProvider}
          providerAPIKey={providerAPIKey}
          onProviderAPIKeyChange={setProviderAPIKey}
          providerAPISecret={providerAPISecret}
          onProviderAPISecretChange={setProviderAPISecret}
          providerEndpoint={providerEndpoint}
          onProviderEndpointChange={setProviderEndpoint}
          selectedProviderMeta={selectedProviderMeta}
          providerOptions={providerOptions}
          onProviderOptionChange={(key, value) => setProviderOptions((current) => ({ ...current, [key]: value }))}
          savingProviderConfig={savingProviderConfig}
          onSaveProviderConfig={handleProviderConfigSave}
          canViewQuota={canViewQuota}
          quota={quota}
          canManageQuota={canManageQuota}
          requestLimit={requestLimit}
          onRequestLimitChange={setRequestLimit}
          tokenLimit={tokenLimit}
          onTokenLimitChange={setTokenLimit}
          enforceHardLimits={enforceHardLimits}
          onEnforceHardLimitsChange={setEnforceHardLimits}
          savingQuota={savingQuota}
          onSaveQuota={handleQuotaSave}
          canManageKeys={canManageKeys}
          serviceAccounts={serviceAccounts}
          onRevokeServiceAccount={(keyId) => {
            if (!confirm('Revoke this service account key?')) return;
            void handleRevokeServiceAccount(keyId);
          }}
          onOpenApiKeys={() => navigate('/api-keys')}
          newServiceAccountName={newServiceAccountName}
          onNewServiceAccountNameChange={setNewServiceAccountName}
          newServiceAccountRole={newServiceAccountRole}
          onNewServiceAccountRoleChange={(value) => setNewServiceAccountRole(value as typeof serviceAccountRoles[number])}
          serviceAccountRoles={serviceAccountRoles}
          creatingServiceAccount={creatingServiceAccount}
          onCreateServiceAccount={handleServiceAccountCreate}
        />
      )}

      {(settingsTab === 'members' || settingsTab === 'invites') && (
        <WorkspaceMembershipSection
          canManageMemberships={canManageMemberships}
          memberCounts={memberCounts}
          members={members}
          memberId={member?.id}
          memberRoles={memberRoles}
          setMemberRoles={setMemberRoles}
          roleOptionsForMember={roleOptionsForMember}
          updatingMemberId={updatingMemberId}
          removingMemberId={removingMemberId}
          onSaveMemberRole={handleMemberRoleSave}
          onRemoveMember={(memberId) => {
            if (!confirm('Remove this member from the workspace? Their linked human keys will be revoked.')) return;
            void handleRemoveMember(memberId);
          }}
          formatDate={formatDate}
          inviteCounts={inviteCounts}
          inviteEmail={inviteEmail}
          onInviteEmailChange={setInviteEmail}
          inviteDisplayName={inviteDisplayName}
          onInviteDisplayNameChange={setInviteDisplayName}
          inviteRole={inviteRole}
          onInviteRoleChange={(value) => setInviteRole(value as typeof assignableInviteRoles[number])}
          visibleInviteRoles={visibleInviteRoles}
          creatingInvite={creatingInvite}
          onCreateInvite={handleInviteCreate}
          pendingInvites={pendingInvites}
          inviteHistory={inviteHistory}
          onRevokeInvite={(inviteId) => {
            if (!confirm('Revoke this invitation?')) return;
            void handleRevokeInvite(inviteId);
          }}
          onOpenDocs={() => navigate('/docs')}
        />
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
