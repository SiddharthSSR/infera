import { useEffect, useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { createApiKey, fetchApiKeys, revokeApiKey } from '../lib/authAccessClient';
import { useAuthSession } from '../lib/auth-context';
import { useIsMobile } from '../hooks/useIsMobile';
import { ActionGroup } from '../components/ActionGroup';
import { MetadataList } from '../components/MetadataList';
import { SectionHeader } from '../components/SectionHeader';
import { formatDate } from '../lib/formatting';
import { principalLabel, roleLabel, roleDescription } from '../lib/labels';
import { GridRow, Cell, LabelText, Badge, ActionButton, ControlInput, ControlSelect } from '../components/shared';
import { ApiKeysSkeleton } from '../components/skeletons';
import type { ApiKeyRecord } from '../types';

/* ------------------------------------------------------------------ */
/*  Custom brand radio                                                  */
/* ------------------------------------------------------------------ */

function BrandRadio({
  checked,
  onChange,
  name,
  children,
}: {
  checked: boolean;
  onChange: () => void;
  name: string;
  children: React.ReactNode;
}) {
  return (
    <label className="api-key-radio-option" style={{ cursor: 'pointer' }}>
      <span
        className="brand-radio"
        role="radio"
        aria-checked={checked}
        tabIndex={0}
        onKeyDown={(e) => { if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); onChange(); } }}
        onClick={onChange}
      >
        {checked && <span className="brand-radio-fill" />}
      </span>
      <input
        type="radio"
        name={name}
        checked={checked}
        onChange={onChange}
        style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }}
      />
      <div>{children}</div>
    </label>
  );
}

/* ------------------------------------------------------------------ */
/*  Inline revoke confirmation                                          */
/* ------------------------------------------------------------------ */

function InlineRevokeButton({ keyId, onRevoke }: { keyId: string; onRevoke: (id: string) => Promise<void> }) {
  const [confirming, setConfirming] = useState(false);
  const [revoking, setRevoking] = useState(false);

  const handleConfirm = async () => {
    setRevoking(true);
    await onRevoke(keyId);
    setRevoking(false);
    setConfirming(false);
  };

  if (confirming) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '0.35rem' }}>
        <span style={{ fontSize: '0.7rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--color-error)' }}>
          ARE YOU SURE?
        </span>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <ActionButton variant="destructive" disabled={revoking} onClick={handleConfirm}>
            {revoking ? 'REVOKING...' : 'CONFIRM'}
          </ActionButton>
          <ActionButton onClick={() => setConfirming(false)}>CANCEL</ActionButton>
        </div>
      </div>
    );
  }

  return (
    <ActionButton variant="destructive" onClick={() => setConfirming(true)}>
      REVOKE
    </ActionButton>
  );
}

function MobileInlineRevokeButton({ keyId, onRevoke }: { keyId: string; onRevoke: (id: string) => Promise<void> }) {
  const [confirming, setConfirming] = useState(false);
  const [revoking, setRevoking] = useState(false);

  const handleConfirm = async () => {
    setRevoking(true);
    await onRevoke(keyId);
    setRevoking(false);
    setConfirming(false);
  };

  if (confirming) {
    return (
      <div className="mobile-data-actions">
        <div style={{ fontSize: '0.7rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--color-error)', textAlign: 'center', padding: '0.25rem 0' }}>
          ARE YOU SURE?
        </div>
        <button className="mobile-data-action danger" disabled={revoking} onClick={handleConfirm}>
          {revoking ? 'REVOKING...' : 'CONFIRM REVOKE'}
        </button>
        <button className="mobile-data-action" onClick={() => setConfirming(false)}>CANCEL</button>
      </div>
    );
  }

  return (
    <div className="mobile-data-actions">
      <button className="mobile-data-action danger" onClick={() => setConfirming(true)}>
        REVOKE
      </button>
    </div>
  );
}

const humanKeyRoles = ['developer', 'operator', 'read_only', 'billing', 'admin'] as const;
const serviceAccountRoles = ['operator', 'developer', 'read_only', 'billing'] as const;

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

  const handleRevoke = useCallback(async (id: string) => {
    try {
      await revokeApiKey(id);
      toast.success('API key revoked');
      await loadKeys();
    } catch (err: any) {
      toast.error(err.message || 'Failed to revoke key');
    }
  }, []);

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

  if (loading) return <ApiKeysSkeleton />;

  return (
    <div className="animate-fade-in api-keys-page">
      {createdKey && (
        <>
          <div className="key-modal-backdrop" onClick={() => setCreatedKey(null)} aria-hidden="true" />
          <div className="key-modal" role="dialog" aria-label="New API key">
            <LabelText as="div" style={{ marginBottom: '0.75rem' }}>NEW API KEY — COPY NOW</LabelText>
            <div className="key-modal-box">
              <code className="mono">{createdKey}</code>
            </div>
            <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6, marginTop: '1rem' }}>
              This is the only time the full key will be shown. It is scoped to <strong>{activeWorkspaceName}</strong>. Store it securely.
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', marginTop: '1.5rem' }}>
              <ActionButton variant="primary" onClick={handleCopyKey} disabled={copying}>
                {copying ? 'COPIED' : 'COPY TO CLIPBOARD'}
              </ActionButton>
              <ActionButton onClick={() => setCreatedKey(null)}>CLOSE</ActionButton>
            </div>
          </div>
        </>
      )}

      <GridRow className="api-keys-session-row">
        <Cell className="api-keys-session-cell" span={2}>
          <SectionHeader
            eyebrow="ACTIVE SESSION"
            title={activeWorkspaceName}
            description={sessionDetail}
            badge={<Badge>{sessionModeLabel}</Badge>}
          />
          <div className="chip-row" style={{ marginTop: '1rem' }}>
            {session?.key?.role && <Badge>{roleLabel(session.key.role)}</Badge>}
            {session?.workspace?.slug && <Badge mono>{session.workspace.slug}</Badge>}
          </div>
        </Cell>
        <Cell className="api-keys-summary-cell" bg="var(--bg-accent)">
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
        </Cell>
      </GridRow>

      <GridRow>
        <Cell span={3}>
          <SectionHeader
            eyebrow="WORKSPACE API KEYS"
            title="Filterable key inventory"
            description="Keep the visible list scoped to the keys you need to inspect or revoke."
            actions={(
              <ActionGroup compact>
                <ControlInput
                  type="text"
                  value={keyFilter}
                  onChange={(event) => setKeyFilter(event.target.value)}
                  placeholder="Search by key name, prefix, role, or workspace..."
                  style={{ minWidth: isMobile ? '100%' : '22rem', margin: 0 }}
                />
                <ControlSelect
                  value={principalFilter}
                  onChange={(event) => setPrincipalFilter(event.target.value as 'all' | 'human' | 'service_account')}
                  style={{ minWidth: isMobile ? '100%' : '11rem', margin: 0 }}
                >
                  <option value="all">All principals</option>
                  <option value="human">Human</option>
                  <option value="service_account">Service account</option>
                </ControlSelect>
              </ActionGroup>
            )}
          />

          {isMobile ? (
            <div className="mobile-data-list" style={{ marginTop: '1.5rem' }}>
              {visibleKeys.length === 0 ? (
                <div style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '2rem 0' }}>
                  {keys.length === 0 ? 'No workspace keys. Create one to get started.' : 'No keys match the current filter.'}
                  <div className="help-actions" style={{ justifyContent: 'center' }}>
                    <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder*="Platform operator"], input[placeholder*="CI deploy bot"]')?.focus()}>
                      CREATE KEY NOW
                    </ActionButton>
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
                      <Badge>{principalLabel(key.principal_type)}</Badge>
                    </div>
                    <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginBottom: '1rem' }}>
                      <Badge>{roleLabel(key.role)}</Badge>
                      <Badge mono>{keyScopeLabel(key)}</Badge>
                    </div>
                    <div className="mobile-data-meta">
                      <div><LabelText>USAGE</LabelText> <span>{keyUsageLabel(key)}</span></div>
                      <div><LabelText>CREATED</LabelText> <span>{formatDate(key.created_at)}</span></div>
                      <div><LabelText>LAST USED</LabelText> <span>{formatDate(key.last_used)}</span></div>
                      <div>
                        <LabelText>STATUS</LabelText>{' '}
                        <span style={{ color: key.status === 'active' ? 'var(--color-success)' : 'var(--color-error)', fontWeight: 600 }}>
                          {key.status.toUpperCase()}
                        </span>
                      </div>
                    </div>
                    {key.status === 'active' && (
                      <MobileInlineRevokeButton keyId={key.id} onRevoke={handleRevoke} />
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
                    <th scope="col">NAME / PREFIX</th>
                    <th scope="col">PRINCIPAL</th>
                    <th scope="col">ROLE</th>
                    <th scope="col">SCOPE</th>
                    <th scope="col">USAGE</th>
                    <th scope="col">LAST USED</th>
                    <th scope="col">STATUS</th>
                    <th scope="col" style={{ textAlign: 'right' }}>ACTION</th>
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
                      <td><Badge>{principalLabel(key.principal_type)}</Badge></td>
                      <td><Badge>{roleLabel(key.role)}</Badge></td>
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
                          <InlineRevokeButton keyId={key.id} onRevoke={handleRevoke} />
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
        </Cell>

        <Cell className="api-key-create-cell" bg="var(--bg-accent)">
          <SectionHeader
            eyebrow="CREATE WORKSPACE KEY"
            title="Create a scoped credential"
            description="Human keys can create dashboard sessions if their role allows dashboard access. Service-account keys are for automation only."
          />

          <div className="help-callout" style={{ marginTop: '1.5rem', marginBottom: '1.5rem' }}>
            <LabelText as="div">WHEN TO USE WHICH KEY</LabelText>
            <div className="help-callout-copy">
              Use a <strong>human key</strong> for a person who needs dashboard access inside the active workspace. Use a <strong>service account</strong> for CI, scripts, agents, and production automation. Switching workspace changes the session context you are browsing, but it does not make a key cross-workspace.
            </div>
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <LabelText as="div">KEY NAME</LabelText>
            <ControlInput
              type="text"
              placeholder={newKeyPrincipal === 'service_account' ? 'e.g. CI deploy bot' : 'e.g. Platform operator'}
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleGenerate()}
            />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <LabelText as="div">PRINCIPAL TYPE</LabelText>
            <div className="api-key-radio-list">
              <BrandRadio
                name="principal_type"
                checked={newKeyPrincipal === 'human'}
                onChange={() => setNewKeyPrincipal('human')}
              >
                <div>Human</div>
                <div className="api-key-option-detail">Use for a person who needs a dashboard session in this workspace.</div>
              </BrandRadio>
              <BrandRadio
                name="principal_type"
                checked={newKeyPrincipal === 'service_account'}
                onChange={() => setNewKeyPrincipal('service_account')}
              >
                <div>Service account</div>
                <div className="api-key-option-detail">Use for CI, scripts, agents, and automation. No dashboard session access.</div>
              </BrandRadio>
            </div>
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <LabelText as="div">ROLE</LabelText>
            <div className="api-key-radio-list">
              {selectableRoles.map((role) => (
                <BrandRadio
                  key={role}
                  name="role"
                  checked={newKeyRole === role}
                  onChange={() => setNewKeyRole(role)}
                >
                  <div>{roleLabel(role)}</div>
                  <div className="api-key-option-detail">{roleDescription(role, newKeyPrincipal)}</div>
                </BrandRadio>
              ))}
            </div>
          </div>

          <ActionButton
            style={{ width: '100%', textAlign: 'left', padding: '1rem 0' }}
            onClick={handleGenerate}
            disabled={creating}
          >
            {creating ? 'CREATING KEY...' : `CREATE ${newKeyPrincipal === 'service_account' ? 'SERVICE ACCOUNT' : 'HUMAN'} KEY`}
          </ActionButton>

          <div className="api-key-create-footer">
            Keys are SHA-256 hashed and are scoped to <strong>{activeWorkspaceName}</strong>. Switching workspace changes which keys and session context you are looking at.
          </div>

          <div className="action-group compact" style={{ marginTop: '1rem' }}>
            <Link className="action-btn" to="/workspace">OPEN WORKSPACE</Link>
            <Link className="action-btn" to="/docs">READ AUTH DOCS</Link>
          </div>
        </Cell>
      </GridRow>

      <GridRow>
        <Cell>
          <LabelText as="div">HUMAN KEYS</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {humanKeys.length} active
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">SERVICE ACCOUNTS</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {serviceAccountKeys.length} active
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">SECURITY</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>SHA-256 hashed, workspace-scoped, bearer auth</div>
        </Cell>
        <Cell>
          <LabelText as="div">SESSION SCOPE</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.9rem' }}>
            Active workspace: <strong>{activeWorkspaceName}</strong>
          </div>
        </Cell>
      </GridRow>
    </div>
  );
}
