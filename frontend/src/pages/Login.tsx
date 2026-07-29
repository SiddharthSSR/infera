import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { createSession } from '../lib/authAccessClient';
import {
  designPartnerRequestEndpoint,
  getPublicAcquisitionTarget,
} from '../lib/designPartnerRequest';
import { publicAnalytics } from '../lib/publicAnalytics';
import {
  ActionButton,
  AppShell,
  ControlInput,
  LabelText,
  PublicNav,
  StatusDot,
} from '../components/shared';
import type { SessionInfo } from '../types';

interface LoginProps {
  onAuthenticated: (session: SessionInfo) => void;
  intakeEndpoint?: string;
}

const sessionSafeguards = [
  {
    index: '01',
    label: 'Auth boundary',
    value: 'Human dashboard access',
  },
  {
    index: '02',
    label: 'Session mode',
    value: 'Stored server-side',
  },
  {
    index: '03',
    label: 'Workspace scope',
    value: 'Bound to the key workspace',
  },
];

export function Login({
  onAuthenticated,
  intakeEndpoint = designPartnerRequestEndpoint,
}: LoginProps) {
  const acquisition = getPublicAcquisitionTarget(intakeEndpoint);
  const accessRequestsOpen = acquisition.path === '/request-access';
  const [key, setKey] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [connected, setConnected] = useState(false);
  const [showKey, setShowKey] = useState(false);
  const keyInputRef = useRef<HTMLInputElement>(null);

  const focusKeyInput = () => {
    keyInputRef.current?.focus();
  };

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();

    if (!key.trim()) {
      setError('Enter a human dashboard key to continue.');
      focusKeyInput();
      return;
    }

    publicAnalytics.track('public_sign_in_intent', { source: 'sign_in_form' });
    setLoading(true);
    setError('');

    try {
      const session = await createSession(key.trim());
      setConnected(true);
      setTimeout(() => onAuthenticated(session), 500);
    } catch (err) {
      if (err instanceof Error) {
        if (err.message.includes('Invalid API key')) {
          setError('Invalid human dashboard key. Check your key and try again.');
        } else if (err.message.includes('Service accounts cannot create dashboard sessions')) {
          setError('Dashboard access requires a human key. Service-account keys are for API and automation use.');
        } else if (
          err.message.includes('Dashboard access required')
          || err.message.includes('Admin access required')
        ) {
          setError('Dashboard access required. Use an active human key with dashboard access; inference-only keys cannot sign in.');
        } else {
          setError('Could not connect to the gateway. Check its availability and try again.');
        }
      } else {
        setError('Could not connect to the gateway. Check its availability and try again.');
      }
      focusKeyInput();
    } finally {
      setLoading(false);
    }
  };

  return (
    <AppShell variant="public" className="login-page">
      <a className="public-skip-link" href="#login-form">Skip to sign in</a>
      <PublicNav
        title="OPEN INFERENCE CONTROL PLANE"
        className="login-public-nav"
        intakeEndpoint={intakeEndpoint}
      />

      <main className="login-shell" id="main-content">
        <section className="login-form-panel" aria-labelledby="login-title">
          <div className="login-form-card">
            <div className="login-session-scope mono" aria-label="Session scope">
              Human dashboard session / Workspace scoped
            </div>

            <div className="login-form-copy">
              <LabelText as="div">Connect</LabelText>
              <h1 id="login-title" className="login-form-title">Sign in with a human dashboard key</h1>
              <p className="login-form-description">
                Open your workspace console to manage models, nodes, access, and usage.
              </p>
            </div>

            <form
              id="login-form"
              onSubmit={handleSubmit}
              className="login-form-fields"
              aria-busy={loading}
              noValidate
            >
              <div className="login-field">
                <div className="login-field-header">
                  <LabelText as="label" htmlFor="login-dashboard-key">Human dashboard key</LabelText>
                  <div className="mono login-field-meta">POST /api/auth/session</div>
                </div>
                <div className="login-input-shell">
                  <ControlInput
                    ref={keyInputRef}
                    id="login-dashboard-key"
                    name="dashboard-key"
                    type={showKey ? 'text' : 'password'}
                    className="login-key-input"
                    placeholder="inf_..."
                    value={key}
                    onChange={(event) => {
                      setKey(event.target.value);
                      setError('');
                    }}
                    autoComplete="current-password"
                    aria-invalid={error ? 'true' : 'false'}
                    aria-describedby={error ? 'login-key-help login-key-error' : 'login-key-help'}
                    required
                  />
                  <button
                    type="button"
                    className="login-visibility-toggle"
                    onClick={() => {
                      setShowKey((visible) => !visible);
                      focusKeyInput();
                    }}
                    aria-label={showKey ? 'Hide human dashboard key' : 'Show human dashboard key'}
                    aria-controls="login-dashboard-key"
                  >
                    {showKey ? 'Hide' : 'Show'}
                  </button>
                </div>
              </div>

              {error ? (
                <p id="login-key-error" className="login-error" role="alert">{error}</p>
              ) : null}

              <ActionButton
                variant="primary"
                className="login-submit"
                type="submit"
                disabled={loading || connected}
              >
                {connected ? (
                  <span className="login-submit-state">
                    <StatusDot tone="success" />
                    Connected
                  </span>
                ) : loading ? 'Connecting...' : 'Connect'}
              </ActionButton>
            </form>

            <section className="login-access-guidance" aria-labelledby="login-access-title">
              <h2 id="login-access-title">Don’t have a dashboard key?</h2>
              <p>
                Access is approved before sign-in. After approval, a workspace admin issues an
                active human dashboard key. Service-account and inference keys cannot start a
                dashboard session. {!accessRequestsOpen ? 'New access requests are not open right now.' : null}
              </p>
              <div className="login-access-actions">
                <Link className="login-access-link" to={acquisition.path}>
                  <span className="login-access-link-title">
                    {accessRequestsOpen ? 'Request access' : 'Evaluate deployment fit'}
                  </span>
                  <span>
                    {accessRequestsOpen
                      ? 'New to Infera? Start with access review.'
                      : 'Review the public evaluation guide while access requests are closed.'}
                  </span>
                </Link>
                <Link className="login-access-link" to="/accept-invite">
                  <span className="login-access-link-title">Accept a workspace invitation</span>
                  <span>Use the token from your workspace admin.</span>
                </Link>
              </div>
            </section>

            <div className="login-help">
              <p id="login-key-help" className="login-help-note">
                Your key is validated by the gateway. The browser receives a workspace-scoped,
                server-side session; the key is not stored here.
              </p>
              <Link className="login-help-link" to="/docs">API Docs</Link>
            </div>
          </div>
        </section>

        <section className="login-brand-panel" aria-labelledby="login-orientation-title">
          <div className="login-brand-content">
            <p className="login-kicker">Private workspace</p>
            <h2 id="login-orientation-title" className="login-brand-title">
              Operator access,<br />without the noise.
            </h2>
            <p className="login-brand-subtitle">
              Use an active human key with dashboard access to open the workspace console for your assigned role.
            </p>
          </div>

          <dl className="login-trust-list" aria-label="Session safeguards">
            {sessionSafeguards.map((safeguard) => (
              <div key={safeguard.index}>
                <dt>{safeguard.index} / {safeguard.label}</dt>
                <dd>{safeguard.value}</dd>
              </div>
            ))}
          </dl>

          <Link className="login-back-link" to="/">← Back to product</Link>
        </section>
      </main>
    </AppShell>
  );
}
