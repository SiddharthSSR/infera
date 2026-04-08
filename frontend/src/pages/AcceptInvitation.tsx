import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import {
  acceptWorkspaceInvitation,
  createSession,
  fetchInvitationPreview,
  type SessionInfo,
  type WorkspaceInvitationPreview,
} from '../lib/api';
import { DisplayHeader, GridRow, Cell, LabelText, ActionButton, ControlInput, AppShell, PublicNav } from '../components/shared';

type AcceptInvitationProps = {
  onAccepted: (session: SessionInfo) => void;
};

export function AcceptInvitation({ onAccepted }: AcceptInvitationProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const initialToken = searchParams.get('token')?.trim() || '';

  const [tokenInput, setTokenInput] = useState(initialToken);
  const [preview, setPreview] = useState<WorkspaceInvitationPreview | null>(null);
  const [displayName, setDisplayName] = useState('');
  const [acceptedKey, setAcceptedKey] = useState<string | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(Boolean(initialToken));
  const [accepting, setAccepting] = useState(false);
  const [sessionStarting, setSessionStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canAccept = Boolean(preview && !acceptedKey && !accepting);
  const effectiveDisplayName = useMemo(() => displayName.trim(), [displayName]);

  useEffect(() => {
    if (!initialToken) return;
    fetchInvitationPreview(initialToken)
      .then((invitation) => {
        setPreview(invitation);
        setDisplayName(invitation.display_name || '');
        setError(null);
      })
      .catch((err) => {
        setPreview(null);
        setError(err instanceof Error ? err.message : 'Failed to load invitation');
      })
      .finally(() => setLoadingPreview(false));
  }, [initialToken]);

  const handleLoadPreview = async () => {
    const trimmed = tokenInput.trim();
    if (!trimmed) {
      setError('Invitation token is required.');
      return;
    }
    setLoadingPreview(true);
    try {
      const invitation = await fetchInvitationPreview(trimmed);
      setSearchParams({ token: trimmed });
      setPreview(invitation);
      setDisplayName(invitation.display_name || '');
      setError(null);
    } catch (err) {
      setPreview(null);
      setError(err instanceof Error ? err.message : 'Failed to load invitation');
    } finally {
      setLoadingPreview(false);
    }
  };

  const handleAccept = async () => {
    const token = (searchParams.get('token') || tokenInput).trim();
    if (!token) {
      setError('Invitation token is required.');
      return;
    }
    setAccepting(true);
    try {
      const result = await acceptWorkspaceInvitation(token, effectiveDisplayName || undefined);
      setAcceptedKey(result.key);
      setError(null);
      toast.success('Invitation accepted. Copy your key before continuing.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to accept invitation');
    } finally {
      setAccepting(false);
    }
  };

  const handleContinue = async () => {
    if (!acceptedKey) return;
    setSessionStarting(true);
    try {
      const session = await createSession(acceptedKey);
      onAccepted(session);
      navigate('/workspace', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create dashboard session');
    } finally {
      setSessionStarting(false);
    }
  };

  return (
    <div style={{ minHeight: '100vh', background: 'linear-gradient(180deg, var(--bg-paper) 0%, #f1ede7 100%)' }}>
      <AppShell variant="bare" maxWidth={1200}>
        <PublicNav
          title="INVITATION ACCEPTANCE"
          links={[
            { path: '/getting-started', label: 'GETTING STARTED' },
            { path: '/', label: 'LOGIN' },
          ]}
          style={{ alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}
        />

        <GridRow columns="1fr 1fr">
          <Cell style={{ padding: '3rem 2rem' }}>
            <DisplayHeader align="left" noBorder padding="0" fontSize="4.5rem">
              JOIN WORKSPACE
            </DisplayHeader>
            <p style={{ marginTop: '1rem', color: 'var(--text-secondary)', maxWidth: 620, fontSize: '1rem', lineHeight: 1.6 }}>
              Use the invitation token you received from a workspace admin. The page previews the workspace and assigned role before acceptance.
            </p>
            <div className="help-callout" style={{ marginTop: '1.5rem', maxWidth: 640 }}>
              <LabelText as="div">WHAT HAPPENS NEXT</LabelText>
              <div className="help-callout-copy">
                Accepting an invitation creates a one-time human key for that workspace. Continuing starts a browser session in the invited workspace and makes it your active dashboard context. It does not email anyone automatically and it does not change any existing service-account keys.
              </div>
            </div>
            <div style={{ marginTop: '2rem', display: 'grid', gap: '1rem' }}>
              <div>
                <LabelText as="div">INVITATION TOKEN</LabelText>
                <ControlInput
                  value={tokenInput}
                  onChange={(e) => setTokenInput(e.target.value)}
                  placeholder="invite_..."
                />
              </div>
              <ActionButton variant="primary" disabled={loadingPreview} onClick={handleLoadPreview}>
                {loadingPreview ? 'LOADING...' : 'LOAD INVITATION'}
              </ActionButton>
              {error && (
                <div style={{ color: '#B3261E', fontSize: '0.9rem', lineHeight: 1.5 }}>
                  {error}
                </div>
              )}
            </div>
          </Cell>

          <Cell style={{ padding: '3rem 2rem', background: 'rgba(0,0,0,0.02)' }}>
            <LabelText as="div">INVITATION PREVIEW</LabelText>
            {preview ? (
              <div style={{ marginTop: '1.5rem', display: 'grid', gap: '1rem' }}>
                <div>
                  <LabelText as="div">WORKSPACE</LabelText>
                  <div className="value-text" style={{ fontSize: '1.1rem', marginTop: '0.35rem' }}>{preview.workspace_name}</div>
                  <div className="mono" style={{ marginTop: '0.35rem', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>{preview.workspace_slug}</div>
                </div>
                <div>
                  <LabelText as="div">ROLE</LabelText>
                  <div className="value-text" style={{ fontSize: '1.1rem', marginTop: '0.35rem' }}>{preview.role}</div>
                </div>
                <div>
                  <LabelText as="div">EMAIL</LabelText>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.35rem' }}>{preview.email}</div>
                </div>
                <div>
                  <LabelText as="div">DISPLAY NAME</LabelText>
                  <ControlInput
                    value={displayName}
                    disabled={Boolean(acceptedKey)}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder="Optional display name"
                  />
                </div>
                <div>
                  <LabelText as="div">EXPIRES</LabelText>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.35rem' }}>{new Date(preview.expires_at).toLocaleString()}</div>
                </div>
                <ActionButton variant="primary" disabled={!canAccept} onClick={handleAccept}>
                  {accepting ? 'ACCEPTING...' : 'ACCEPT INVITATION'}
                </ActionButton>
              </div>
            ) : (
              <div style={{ marginTop: '1rem', color: 'var(--text-secondary)', fontSize: '0.95rem', lineHeight: 1.6 }}>
                Load a valid invitation token to see the workspace, role, and expiry before accepting.
              </div>
            )}
          </Cell>
        </GridRow>

        {acceptedKey && (
          <GridRow columns="1fr">
            <Cell bg="#E8F5E9">
              <LabelText as="div">ONE-TIME API KEY — COPY NOW</LabelText>
              <pre className="code-block" style={{ marginTop: '1rem' }}>{acceptedKey}</pre>
              <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                <ActionButton
                  variant="primary"
                  onClick={() => navigator.clipboard.writeText(acceptedKey).then(() => toast.success('Invitation key copied.'))}
                >
                  COPY KEY
                </ActionButton>
                <ActionButton variant="secondary" disabled={sessionStarting} onClick={handleContinue}>
                  {sessionStarting ? 'STARTING SESSION...' : 'CONTINUE AND SWITCH WORKSPACE'}
                </ActionButton>
              </div>
              <div style={{ marginTop: '0.9rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                Continuing will start a dashboard session for <strong>{preview?.workspace_name || 'this workspace'}</strong> and make it your active workspace.
              </div>
            </Cell>
          </GridRow>
        )}
      </AppShell>
    </div>
  );
}
