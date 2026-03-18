import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { createApiKey, fetchApiKeys, revokeApiKey, type ApiKeyRecord } from '../lib/api';
import { useAuthSession } from '../lib/auth-context';
import { useIsMobile } from '../hooks/useIsMobile';
import { ActionGroup } from '../components/ActionGroup';
import { MetadataList } from '../components/MetadataList';
import { SectionHeader } from '../components/SectionHeader';

const humanKeyRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const serviceAccountRoles = ['operator', 'developer', 'read_only', 'billing'] as const;

function formatDate(dateStr: string | null | undefined) {
  if (!dateStr) return 'Never';
  try {
    return new Date(dateStr).toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    });
  } catch {
    return dateStr;
  }
}

function principalLabel(principalType?: string) {
  return principalType === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN';
}

function roleLabel(role: string) {
  return role.replace(/_/g, ' ').toUpperCase();
}

function keyScopeLabel(key: ApiKeyRecord) {
  return key.workspace_name || key.workspace_slug || 'Current workspace';
}

function keyUsageLabel(key: ApiKeyRecord) {
  return key.principal_type === 'service_account'
    ? 'Automation only'
    : key.role === 'user'
      ? 'Inference only'
      : 'Can start dashboard sessions';
}

function roleDescription(role: string, principalType: 'human' | 'service_account') {
  switch (role) {
    case 'admin':
      return principalType === 'human'
        ? 'Full workspace administration, membership, key, quota, and provider management.'
        : 'Not available for service accounts.';
    case 'operator':
      return 'Infrastructure operations and deployment control for this workspace.';
    case 'developer':
      return 'Model and product development access within this workspace.';
    case 'billing':
      return 'Quota, usage, and billing visibility for this workspace.';
    case 'read_only':
      return 'Read-only operational visibility without mutation rights.';
    case 'user':
      return 'Legacy inference-only key without dashboard access.';
    default:
      return 'Workspace-scoped access.';
  }
}

function assignableHumanRoles(currentRole?: string) {
  if (currentRole === 'owner') {
    return humanKeyRoles;
  }
  return humanKeyRoles.filter((role) => role !== 'admin');
}

export function ApiKeys() {
  const isMobile = useIsMobile(900);
  const { session, availableWorkspaces } = useAuthSession();
  const [keys, setKeys] = useState<ApiKeyRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyPrincipal, setNewKeyPrincipal] = useState<'human' | 'service_account'>('human');
  const [newKeyRole, setNewKeyRole] = useState<string>('developer');
  const [creating, setCreating] = useState(false);
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [copying, setCopying] = useState(false);
  const [keyFilter, setKeyFilter] = useState('');
  const [principalFilter, setPrincipalFilter] = useState<'all' | 'human' | 'service_account'>('all');

  const activeWorkspaceName = session?.workspace?.name || 'Current workspace';
  const canSwitchWorkspaces = availableWorkspaces.length > 1;
  const availableHumanRoles = useMemo(
    () => assignableHumanRoles(session?.key?.role),
    [session?.key?.role],
  );
  const selectableRoles = newKeyPrincipal === 'service_account' ? serviceAccountRoles : availableHumanRoles;

  useEffect(() => {
    if (!selectableRoles.includes(newKeyRole as never)) {
      setNewKeyRole(selectableRoles[0] || 'developer');
    }
  }, [newKeyRole, selectableRoles]);

  const loadKeys = async () => {
    try {
      const data = await fetchApiKeys();
      setKeys(data);
    } catch {
      setKeys([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadKeys();
  }, []);

  const activeKeys = keys.filter((key) => key.status === 'active');
  const humanKeys = activeKeys.filter((key) => key.principal_type !== 'service_account');
  const serviceAccountKeys = activeKeys.filter((key) => key.principal_type === 'service_account');
  const visibleKeys = keys.filter((key) => {
    if (principalFilter !== 'all' && key.principal_type !== principalFilter) {
      return false;
    }
    if (!keyFilter.trim()) {
      return true;
    }
    const normalized = keyFilter.trim().toLowerCase();
    return [
      key.name,
      key.key_prefix,
      key.workspace_name,
      key.workspace_slug,
      key.role,
      key.principal_type,
    ].some((value) => value?.toLowerCase().includes(normalized));
  });

  const handleGenerate = async () => {
    if (!newKeyName.trim()) {
      toast.error('Please enter a key name');
      return;
    }

    setCreating(true);
    try {
      const result = await createApiKey(newKeyName.trim(), newKeyRole, newKeyPrincipal);
      setCreatedKey(result.key);
      setNewKeyName('');
      setNewKeyPrincipal('human');
      setNewKeyRole(assignableHumanRoles(session?.key?.role)[0] || 'developer');
      toast.success('API key created');
      await loadKeys();
    } catch (err: any) {
      toast.error(err.message || 'Failed to create key');
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (id: string) => {
    if (!confirm('Revoke this API key? This cannot be undone.')) return;
    try {
      await revokeApiKey(id);
      toast.success('API key revoked');
      await loadKeys();
    } catch (err: any) {
      toast.error(err.message || 'Failed to revoke key');
    }
  };

  const handleCopyKey = async () => {
    if (!createdKey) return;
    setCopying(true);
    try {
      await navigator.clipboard.writeText(createdKey);
      toast.success('Key copied to clipboard');
    } catch {
      toast.error('Failed to copy key');
    } finally {
      setCopying(false);
    }
  };

  const sessionModeLabel = session?.key?.principal_type === 'service_account' ? 'SERVICE ACCOUNT SESSION' : 'HUMAN SESSION';

  const sessionDetail =
    session?.key?.principal_type === 'service_account'
      ? 'This session is bound to a service-account key. It stays in one workspace and is intended for automation.'
      : canSwitchWorkspaces
        ? 'This dashboard session is workspace-scoped. Switching workspaces updates the active session context and reloads page data for that workspace.'
        : 'This dashboard session is currently scoped to one accessible workspace.';

  return (
    <div className="animate-fade-in api-keys-page">
      {createdKey && (
        <div className="api-key-banner">
          <div>
            <div className="api-key-banner-title">NEW API KEY — COPY NOW</div>
            <code className="mono api-key-banner-code">{createdKey}</code>
            <div className="api-key-banner-detail">
              The full key is only shown once. It is scoped to <strong>{activeWorkspaceName}</strong>.
            </div>
          </div>
          <div className="api-key-banner-actions">
            <button className="btn-primary" onClick={handleCopyKey} disabled={copying}>
              {copying ? 'COPYING...' : 'COPY'}
            </button>
            <button className="btn-secondary" onClick={() => setCreatedKey(null)}>DISMISS</button>
          </div>
        </div>
      )}

      <div className="grid-row api-keys-session-row">
        <div className="cell api-keys-session-cell" style={{ gridColumn: 'span 2' }}>
          <SectionHeader
            eyebrow="ACTIVE SESSION"
            title={activeWorkspaceName}
            description={sessionDetail}
            badge={<span className="badge">{sessionModeLabel}</span>}
          />
          <div className="chip-row" style={{ marginTop: '1rem' }}>
            {session?.key?.role && <span className="badge">{roleLabel(session.key.role)}</span>}
            {session?.workspace?.slug && <span className="badge mono">{session.workspace.slug}</span>}
          </div>
        </div>
        <div className="cell api-keys-summary-cell" style={{ backgroundColor: 'var(--bg-accent)' }}>
          <SectionHeader
            eyebrow="WORKSPACE KEY SUMMARY"
            title="Scope and inventory"
            description={`Keys listed here belong to ${activeWorkspaceName} only.`}
          />
          <div style={{ marginTop: '1.2rem' }}>
            <MetadataList
              items={[
                { label: 'ACTIVE KEYS', value: String(activeKeys.length), mono: true },
                { label: 'HUMAN', value: String(humanKeys.length), mono: true },
                { label: 'SERVICE ACCOUNTS', value: String(serviceAccountKeys.length), mono: true },
                { label: 'VISIBLE NOW', value: String(visibleKeys.length), mono: true },
              ]}
              columns={2}
            />
          </div>
        </div>
      </div>

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 3' }}>
          <SectionHeader
            eyebrow="WORKSPACE API KEYS"
            title="Filterable key inventory"
            description="Keep the visible list scoped to the keys you need to inspect or revoke."
            actions={(
              <ActionGroup compact>
                <input
                  type="text"
                  value={keyFilter}
                  onChange={(event) => setKeyFilter(event.target.value)}
                  placeholder="Search by key name, prefix, role, or workspace..."
                  className="control-input"
                  style={{ minWidth: isMobile ? '100%' : '22rem', margin: 0 }}
                />
                <select
                  className="control-input"
                  value={principalFilter}
                  onChange={(event) => setPrincipalFilter(event.target.value as 'all' | 'human' | 'service_account')}
                  style={{ minWidth: isMobile ? '100%' : '11rem', margin: 0 }}
                >
                  <option value="all">All principals</option>
                  <option value="human">Human</option>
                  <option value="service_account">Service account</option>
                </select>
              </ActionGroup>
            )}
          />

          {loading ? (
            <div style={{ padding: '3rem 0', textAlign: 'center', color: 'var(--text-secondary)' }}>
              Loading keys...
            </div>
          ) : isMobile ? (
            <div className="mobile-data-list" style={{ marginTop: '1.5rem' }}>
              {visibleKeys.length === 0 ? (
                <div style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '2rem 0' }}>
                  {keys.length === 0 ? 'No workspace keys. Create one to get started.' : 'No keys match the current filter.'}
                  <div className="help-actions" style={{ justifyContent: 'center' }}>
                    <button className="action-btn" onClick={() => document.querySelector<HTMLInputElement>('input[placeholder*="Platform operator"], input[placeholder*="CI deploy bot"]')?.focus()}>
                      CREATE KEY NOW
                    </button>
                    <Link className="action-btn" to="/docs">READ AUTH DOCS</Link>
                  </div>
                </div>
              ) : (
                visibleKeys.map((key) => (
                  <div key={key.id} className="mobile-data-card">
                    <div className="mobile-data-card-header">
                      <div>
                        <div className="mobile-data-title">{key.name}</div>
                        <div className="mobile-data-subtitle mono">{key.key_prefix}</div>
                      </div>
                      <span className="badge">{principalLabel(key.principal_type)}</span>
                    </div>
                    <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
                      <span className="badge">{roleLabel(key.role)}</span>
                      <span className="badge mono">{keyScopeLabel(key)}</span>
                    </div>
                    <div className="mobile-data-meta">
                      <div><span className="label-text">USAGE</span> <span>{keyUsageLabel(key)}</span></div>
                      <div><span className="label-text">CREATED</span> <span>{formatDate(key.created_at)}</span></div>
                      <div><span className="label-text">LAST USED</span> <span>{formatDate(key.last_used)}</span></div>
                      <div>
                        <span className="label-text">STATUS</span>{' '}
                        <span style={{ color: key.status === 'active' ? 'var(--color-success)' : 'var(--color-error)', fontWeight: 600 }}>
                          {key.status.toUpperCase()}
                        </span>
                      </div>
                    </div>
                    {key.status === 'active' && (
                      <div className="mobile-data-actions">
                        <button className="mobile-data-action danger" onClick={() => handleRevoke(key.id)}>
                          REVOKE
                        </button>
                      </div>
                    )}
                  </div>
                ))
              )}
            </div>
          ) : (
            <div className="responsive-scroll-x" style={{ marginTop: '1.5rem' }}>
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th>NAME / PREFIX</th>
                    <th>PRINCIPAL</th>
                    <th>ROLE</th>
                    <th>SCOPE</th>
                    <th>USAGE</th>
                    <th>LAST USED</th>
                    <th>STATUS</th>
                    <th style={{ textAlign: 'right' }}>ACTION</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleKeys.map((key) => (
                    <tr key={key.id}>
                      <td>
                        <div style={{ fontWeight: 500 }}>{key.name}</div>
                        <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: 4 }}>
                          {key.key_prefix}
                        </div>
                      </td>
                      <td><span className="badge">{principalLabel(key.principal_type)}</span></td>
                      <td><span className="badge">{roleLabel(key.role)}</span></td>
                      <td>
                        <div>{keyScopeLabel(key)}</div>
                        {key.workspace_slug && (
                          <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: 4 }}>
                            {key.workspace_slug}
                          </div>
                        )}
                      </td>
                      <td>{keyUsageLabel(key)}</td>
                      <td>{formatDate(key.last_used)}</td>
                      <td>
                        <span style={{
                          color: key.status === 'active' ? 'var(--color-success)' : 'var(--color-error)',
                          fontWeight: 600,
                          fontSize: '0.75rem',
                          textTransform: 'uppercase',
                        }}>
                          {key.status}
                        </span>
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        {key.status === 'active' && (
                          <button className="action-btn destructive" onClick={() => handleRevoke(key.id)}>
                            REVOKE
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                  {visibleKeys.length === 0 && (
                    <tr>
                      <td colSpan={8} style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '3rem 0' }}>
                        {keys.length === 0 ? 'No workspace keys. Create one to get started.' : 'No keys match the current filter.'}
                        <div className="help-actions" style={{ justifyContent: 'center' }}>
                          <button className="action-btn" onClick={() => document.querySelector<HTMLInputElement>('input[placeholder*="Platform operator"], input[placeholder*="CI deploy bot"]')?.focus()}>
                            CREATE KEY NOW
                          </button>
                          <Link className="action-btn" to="/docs">READ AUTH DOCS</Link>
                        </div>
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="cell api-key-create-cell" style={{ backgroundColor: 'var(--bg-accent)' }}>
          <SectionHeader
            eyebrow="CREATE WORKSPACE KEY"
            title="Create a scoped credential"
            description="Human keys can create dashboard sessions if their role allows dashboard access. Service-account keys are for automation only."
          />

          <div className="help-callout" style={{ marginTop: '1.5rem', marginBottom: '1.5rem' }}>
            <div className="label-text">WHEN TO USE WHICH KEY</div>
            <div className="help-callout-copy">
              Use a <strong>human key</strong> for a person who needs dashboard access inside the active workspace. Use a <strong>service account</strong> for CI, scripts, agents, and production automation. Switching workspace changes the session context you are browsing, but it does not make a key cross-workspace.
            </div>
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">KEY NAME</div>
            <input
              type="text"
              className="control-input"
              placeholder={newKeyPrincipal === 'service_account' ? 'e.g. CI deploy bot' : 'e.g. Platform operator'}
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleGenerate()}
            />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">PRINCIPAL TYPE</div>
            <div className="api-key-radio-list">
              <label className="api-key-radio-option">
                <input
                  type="radio"
                  name="principal_type"
                  checked={newKeyPrincipal === 'human'}
                  onChange={() => setNewKeyPrincipal('human')}
                />
                <div>
                  <div>Human</div>
                  <div className="api-key-option-detail">Use for a person who needs a dashboard session in this workspace.</div>
                </div>
              </label>
              <label className="api-key-radio-option">
                <input
                  type="radio"
                  name="principal_type"
                  checked={newKeyPrincipal === 'service_account'}
                  onChange={() => setNewKeyPrincipal('service_account')}
                />
                <div>
                  <div>Service account</div>
                  <div className="api-key-option-detail">Use for CI, scripts, agents, and automation. No dashboard session access.</div>
                </div>
              </label>
            </div>
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">ROLE</div>
            <div className="api-key-radio-list">
              {selectableRoles.map((role) => (
                <label key={role} className="api-key-radio-option">
                  <input
                    type="radio"
                    name="role"
                    checked={newKeyRole === role}
                    onChange={() => setNewKeyRole(role)}
                  />
                  <div>
                    <div>{roleLabel(role)}</div>
                    <div className="api-key-option-detail">{roleDescription(role, newKeyPrincipal)}</div>
                  </div>
                </label>
              ))}
            </div>
          </div>

          <button
            className="action-btn"
            style={{ width: '100%', textAlign: 'left', padding: '1rem 0' }}
            onClick={handleGenerate}
            disabled={creating}
          >
            {creating ? 'CREATING KEY...' : `CREATE ${newKeyPrincipal === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN'} KEY`}
          </button>

          <div className="api-key-create-footer">
            Keys are SHA-256 hashed and are scoped to <strong>{activeWorkspaceName}</strong>. Switching workspace changes which keys and session context you are looking at.
          </div>

          <div className="action-group compact" style={{ marginTop: '1rem' }}>
            <Link className="action-btn" to="/workspace">OPEN WORKSPACE</Link>
            <Link className="action-btn" to="/docs">READ AUTH DOCS</Link>
          </div>
        </div>
      </div>

      <div className="grid-row">
        <div className="cell">
          <div className="label-text">HUMAN KEYS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {humanKeys.length} active
          </div>
        </div>
        <div className="cell">
          <div className="label-text">SERVICE ACCOUNTS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {serviceAccountKeys.length} active
          </div>
        </div>
        <div className="cell">
          <div className="label-text">SECURITY</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>SHA-256 hashed, workspace-scoped, bearer auth</div>
        </div>
        <div className="cell">
          <div className="label-text">SESSION SCOPE</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.9rem' }}>
            Active workspace: <strong>{activeWorkspaceName}</strong>
          </div>
        </div>
      </div>
    </div>
  );
}
