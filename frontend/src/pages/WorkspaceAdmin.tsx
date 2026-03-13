import { useEffect, useMemo, useState } from 'react';
import { toast } from 'sonner';
import {
  fetchApiKeys,
  createApiKey,
  revokeApiKey,
  fetchWorkspaceQuota,
  updateWorkspaceQuota,
  fetchWorkspaceMembers,
  fetchWorkspaceInvites,
  createWorkspaceInvite,
  revokeWorkspaceInvite,
  type ApiKeyRecord,
  type WorkspaceQuotaRecord,
  type WorkspaceMemberRecord,
  type WorkspaceInvitationRecord,
} from '../lib/api';
import { useAuthSession } from '../lib/auth-context';

const assignableInviteRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const serviceAccountRoles = ['operator', 'developer', 'read_only', 'billing'] as const;

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

export function WorkspaceAdmin() {
  const { session } = useAuthSession();
  const workspaceId = session?.workspace?.id ?? '';
  const role = session?.key?.role ?? 'user';
  const member = session?.member;

  const canManageMemberships = role === 'owner' || role === 'admin';
  const canManageKeys = role === 'owner' || role === 'admin';
  const canManageQuota = role === 'owner' || role === 'admin' || role === 'billing';
  const canViewQuota = canManageQuota || role === 'read_only';

  const [loading, setLoading] = useState(true);
  const [quota, setQuota] = useState<WorkspaceQuotaRecord | null>(null);
  const [members, setMembers] = useState<WorkspaceMemberRecord[]>([]);
  const [invites, setInvites] = useState<WorkspaceInvitationRecord[]>([]);
  const [serviceAccounts, setServiceAccounts] = useState<ApiKeyRecord[]>([]);

  const [requestLimit, setRequestLimit] = useState('');
  const [tokenLimit, setTokenLimit] = useState('');
  const [enforceHardLimits, setEnforceHardLimits] = useState(true);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteDisplayName, setInviteDisplayName] = useState('');
  const [inviteRole, setInviteRole] = useState<typeof assignableInviteRoles[number]>('developer');
  const [newServiceAccountName, setNewServiceAccountName] = useState('');
  const [newServiceAccountRole, setNewServiceAccountRole] = useState<typeof serviceAccountRoles[number]>('operator');
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);
  const [createdInviteToken, setCreatedInviteToken] = useState<string | null>(null);
  const [savingQuota, setSavingQuota] = useState(false);
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [creatingServiceAccount, setCreatingServiceAccount] = useState(false);

  const visibleInviteRoles = useMemo(() => {
    if (role === 'owner') return assignableInviteRoles;
    return assignableInviteRoles.filter((candidate) => candidate !== 'admin');
  }, [role]);

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

    if (canManageMemberships) {
      tasks.push(
        fetchWorkspaceMembers(workspaceId).then(setMembers).catch(() => setMembers([])),
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

    await Promise.all(tasks);
  };

  useEffect(() => {
    setLoading(true);
    loadWorkspaceData().finally(() => setLoading(false));
  }, [workspaceId, canManageMemberships, canManageKeys, canViewQuota]);

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
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>Manage quota</span><span className="mono">{canManageQuota ? 'YES' : 'NO'}</span></div>
            <div style={{ display: 'flex', justifyContent: 'space-between' }}><span>View quota</span><span className="mono">{canViewQuota ? 'YES' : 'NO'}</span></div>
          </div>
        </div>
      </div>

      <div className="grid-row">
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
                      <span className="badge">{memberRecord.role.toUpperCase()}</span>
                    </div>
                    <div className="mobile-data-meta">
                      <div><span className="label-text">STATUS</span> <span>{memberRecord.status}</span></div>
                      <div><span className="label-text">JOINED</span> <span>{formatDate(memberRecord.created_at)}</span></div>
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
    </div>
  );
}
