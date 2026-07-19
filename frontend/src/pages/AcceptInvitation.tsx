import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import {
  acceptWorkspaceInvitation,
  createSession,
  fetchInvitationPreview,
} from '../lib/authAccessClient';
import { getInvitationRecoveryGuidance } from '../lib/authAccess';
import { DisplayHeader, GridRow, Cell, LabelText, ActionButton, ControlInput, AppShell, PublicNav } from '../components/shared';
import type { SessionInfo, WorkspaceInvitationPreview } from '../types';

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
  const [recoveryGuidance, setRecoveryGuidance] = useState<string | null>(null);
  const [errorContext, setErrorContext] = useState<'invitation' | 'session' | null>(null);
  const loadedPreviewTokenRef = useRef('');

  const canAccept = Boolean(preview && !acceptedKey && !accepting);
  const effectiveDisplayName = displayName.trim();

  useEffect(() => {
    if (!initialToken) return;
    if (loadedPreviewTokenRef.current === initialToken) {
      setLoadingPreview(false);
      return;
    }
    loadedPreviewTokenRef.current = initialToken;
    fetchInvitationPreview(initialToken)
      .then((invitation) => {
        setPreview(invitation);
        setDisplayName(invitation.display_name || '');
        setError(null);
        setRecoveryGuidance(null);
        setErrorContext(null);
      })
      .catch((err) => {
        setPreview(null);
        setError(err instanceof Error ? err.message : 'Failed to load invitation');
        setRecoveryGuidance(getInvitationRecoveryGuidance(err));
        setErrorContext('invitation');
      })
      .finally(() => setLoadingPreview(false));
  }, [initialToken]);

  const handleLoadPreview = async (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    const trimmed = tokenInput.trim();
    if (!trimmed) {
      setError('Invitation token is required.');
      setRecoveryGuidance('Paste the complete token from your invitation link, or ask the workspace admin to send a new invitation.');
      setErrorContext('invitation');
      return;
    }
    setLoadingPreview(true);
    try {
      const invitation = await fetchInvitationPreview(trimmed);
      loadedPreviewTokenRef.current = trimmed;
      setSearchParams({ token: trimmed });
      setPreview(invitation);
      setDisplayName(invitation.display_name || '');
      setError(null);
      setRecoveryGuidance(null);
      setErrorContext(null);
    } catch (err) {
      setPreview(null);
      setError(err instanceof Error ? err.message : 'Failed to load invitation');
      setRecoveryGuidance(getInvitationRecoveryGuidance(err));
      setErrorContext('invitation');
    } finally {
      setLoadingPreview(false);
    }
  };

  const handleAccept = async () => {
    const token = (searchParams.get('token') || tokenInput).trim();
    if (!token) {
      setError('Invitation token is required.');
      setRecoveryGuidance('Paste the complete token from your invitation link, or ask the workspace admin to send a new invitation.');
      setErrorContext('invitation');
      return;
    }
    setAccepting(true);
    try {
      const result = await acceptWorkspaceInvitation(token, effectiveDisplayName || undefined);
      setAcceptedKey(result.key);
      setError(null);
      setRecoveryGuidance(null);
      setErrorContext(null);
      toast.success('Invitation accepted. Copy your key before continuing.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to accept invitation');
      setRecoveryGuidance(getInvitationRecoveryGuidance(err));
      setErrorContext('invitation');
    } finally {
      setAccepting(false);
    }
  };

  const handleCopyKey = async () => {
    if (!acceptedKey) return;
    try {
      await navigator.clipboard.writeText(acceptedKey);
      toast.success('Invitation key copied.');
    } catch {
      setError('Could not copy the human key automatically.');
      setRecoveryGuidance('Select the key shown above and copy it manually before leaving this page.');
      setErrorContext('session');
    }
  };

  const handleContinue = async () => {
    if (!acceptedKey) return;
    setSessionStarting(true);
    setError(null);
    setRecoveryGuidance(null);
    setErrorContext(null);
    try {
      const session = await createSession(acceptedKey);
      onAccepted(session);
      navigate('/workspace', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create dashboard session');
      setRecoveryGuidance('Your human key is still shown below. Copy it, then retry. If needed, return to Sign in and paste the same key there.');
      setErrorContext('session');
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
            { path: '/sign-in', label: 'LOGIN' },
          ]}
          style={{ alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}
        />

        <GridRow columns="1fr 1fr">
          <Cell style={{ padding: '3rem 2rem' }}>
            <DisplayHeader className="invitation-display-header" align="left" noBorder padding="0">
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
            <form className="invitation-token-form" onSubmit={handleLoadPreview} noValidate>
              <div>
                <LabelText as="label" htmlFor="invitation-token">INVITATION TOKEN</LabelText>
                <ControlInput
                  id="invitation-token"
                  name="invitation-token"
                  value={tokenInput}
                  onChange={(e) => {
                    setTokenInput(e.target.value);
                    if (errorContext === 'invitation') {
                      setError(null);
                      setRecoveryGuidance(null);
                      setErrorContext(null);
                    }
                  }}
                  placeholder="invite_..."
                  autoComplete="off"
                  aria-describedby={errorContext === 'invitation' ? 'invitation-error invitation-recovery' : 'invitation-token-help'}
                  aria-invalid={errorContext === 'invitation'}
                />
                <div id="invitation-token-help" style={{ marginTop: '0.4rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.5 }}>
                  The token is used only to preview and accept this invitation.
                </div>
              </div>
              <ActionButton type="submit" variant="primary" disabled={loadingPreview} minHeight={44}>
                {loadingPreview ? 'LOADING...' : 'LOAD INVITATION'}
              </ActionButton>
              {error && errorContext === 'invitation' && (
                <div className="invitation-form-error" role="alert">
                  <div id="invitation-error">{error}</div>
                  {recoveryGuidance && <div className="invitation-recovery" id="invitation-recovery">{recoveryGuidance}</div>}
                </div>
              )}
            </form>
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
                  <LabelText as="label" htmlFor="invitation-display-name">DISPLAY NAME</LabelText>
                  <ControlInput
                    id="invitation-display-name"
                    name="display-name"
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
                <ActionButton variant="primary" disabled={!canAccept} onClick={handleAccept} minHeight={44}>
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
              <LabelText as="div">ONE-TIME HUMAN KEY — COPY NOW</LabelText>
              <pre className="code-block" style={{ marginTop: '1rem' }}>{acceptedKey}</pre>
              <div style={{ marginTop: '0.9rem', color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.6 }}>
                This human key starts your dashboard session. Store it securely for sign-in recovery, and create a separate service-account key for scripts or production automation.
              </div>
              <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                <ActionButton
                  variant="primary"
                  minHeight={44}
                  onClick={handleCopyKey}
                >
                  COPY KEY
                </ActionButton>
                <ActionButton variant="secondary" minHeight={44} disabled={sessionStarting} onClick={handleContinue}>
                  {sessionStarting ? 'STARTING SESSION...' : 'CONTINUE TO WORKSPACE SETUP'}
                </ActionButton>
              </div>
              <div style={{ marginTop: '0.9rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                Continuing starts a browser session for <strong>{preview?.workspace_name || 'this workspace'}</strong>, makes it active, and opens Workspace so you can review access and connect the first provider.
              </div>
              {error && errorContext === 'session' && (
                <div className="invitation-form-error invitation-session-error" role="alert">
                  <div>{error}</div>
                  {recoveryGuidance && <div className="invitation-recovery">{recoveryGuidance}</div>}
                </div>
              )}
            </Cell>
          </GridRow>
        )}
      </AppShell>
    </div>
  );
}
