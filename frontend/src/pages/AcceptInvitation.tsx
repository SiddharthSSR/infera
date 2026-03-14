import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import {
  acceptWorkspaceInvitation,
  createSession,
  fetchInvitationPreview,
  type SessionInfo,
  type WorkspaceInvitationPreview,
} from '../lib/api';

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
      <div className="app-shell" style={{ maxWidth: 1200 }}>
        <header className="top-nav" style={{ alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}>
          <div>
            <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
            <div className="label-text" style={{ marginTop: '0.5rem' }}>INVITATION ACCEPTANCE</div>
          </div>
          <div className="nav-group" style={{ gap: '1rem' }}>
            <Link className="nav-link" to="/getting-started">GETTING STARTED</Link>
            <Link className="nav-link" to="/">LOGIN</Link>
          </div>
        </header>

        <section className="grid-row" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <div className="cell" style={{ padding: '3rem 2rem' }}>
            <div className="display-text" style={{ textAlign: 'left', border: 'none', padding: 0, fontSize: '4.5rem' }}>
              JOIN WORKSPACE
            </div>
            <p style={{ marginTop: '1rem', color: 'var(--text-secondary)', maxWidth: 620, fontSize: '1rem', lineHeight: 1.6 }}>
              Use the invitation token you received from a workspace admin. The page previews the workspace and assigned role before acceptance.
            </p>
            <div style={{ marginTop: '2rem', display: 'grid', gap: '1rem' }}>
              <div>
                <div className="label-text">INVITATION TOKEN</div>
                <input
                  className="control-input"
                  value={tokenInput}
                  onChange={(e) => setTokenInput(e.target.value)}
                  placeholder="invite_..."
                />
              </div>
              <button className="btn-primary" disabled={loadingPreview} onClick={handleLoadPreview}>
                {loadingPreview ? 'LOADING...' : 'LOAD INVITATION'}
              </button>
              {error && (
                <div style={{ color: '#B3261E', fontSize: '0.9rem', lineHeight: 1.5 }}>
                  {error}
                </div>
              )}
            </div>
          </div>

          <div className="cell" style={{ padding: '3rem 2rem', background: 'rgba(0,0,0,0.02)' }}>
            <div className="label-text">INVITATION PREVIEW</div>
            {preview ? (
              <div style={{ marginTop: '1.5rem', display: 'grid', gap: '1rem' }}>
                <div>
                  <div className="label-text">WORKSPACE</div>
                  <div className="value-text" style={{ fontSize: '1.1rem', marginTop: '0.35rem' }}>{preview.workspace_name}</div>
                  <div className="mono" style={{ marginTop: '0.35rem', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>{preview.workspace_slug}</div>
                </div>
                <div>
                  <div className="label-text">ROLE</div>
                  <div className="value-text" style={{ fontSize: '1.1rem', marginTop: '0.35rem' }}>{preview.role}</div>
                </div>
                <div>
                  <div className="label-text">EMAIL</div>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.35rem' }}>{preview.email}</div>
                </div>
                <div>
                  <div className="label-text">DISPLAY NAME</div>
                  <input
                    className="control-input"
                    value={displayName}
                    disabled={Boolean(acceptedKey)}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder="Optional display name"
                  />
                </div>
                <div>
                  <div className="label-text">EXPIRES</div>
                  <div className="value-text" style={{ fontSize: '1rem', marginTop: '0.35rem' }}>{new Date(preview.expires_at).toLocaleString()}</div>
                </div>
                <button className="btn-primary" disabled={!canAccept} onClick={handleAccept}>
                  {accepting ? 'ACCEPTING...' : 'ACCEPT INVITATION'}
                </button>
              </div>
            ) : (
              <div style={{ marginTop: '1rem', color: 'var(--text-secondary)', fontSize: '0.95rem', lineHeight: 1.6 }}>
                Load a valid invitation token to see the workspace, role, and expiry before accepting.
              </div>
            )}
          </div>
        </section>

        {acceptedKey && (
          <section className="grid-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="cell" style={{ backgroundColor: '#E8F5E9' }}>
              <div className="label-text">ONE-TIME API KEY — COPY NOW</div>
              <pre className="code-block" style={{ marginTop: '1rem' }}>{acceptedKey}</pre>
              <div style={{ marginTop: '1rem', display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                <button
                  className="btn-primary"
                  onClick={() => navigator.clipboard.writeText(acceptedKey).then(() => toast.success('Invitation key copied.'))}
                >
                  COPY KEY
                </button>
                <button className="btn-secondary" disabled={sessionStarting} onClick={handleContinue}>
                  {sessionStarting ? 'STARTING SESSION...' : 'CONTINUE AND SWITCH WORKSPACE'}
                </button>
              </div>
              <div style={{ marginTop: '0.9rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                Continuing will start a dashboard session for <strong>{preview?.workspace_name || 'this workspace'}</strong> and make it your active workspace.
              </div>
            </div>
          </section>
        )}
      </div>
    </div>
  );
}
