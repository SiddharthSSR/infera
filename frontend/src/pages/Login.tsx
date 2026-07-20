import { Link } from 'react-router-dom';
import { useState, useEffect, useRef } from 'react';
import { createSession } from '../lib/authAccessClient';
import { publicAnalytics } from '../lib/publicAnalytics';
import { LabelText, StatusDot, ControlInput, ActionButton } from '../components/shared';
import type { SessionInfo } from '../types';

interface LoginProps {
  onAuthenticated: (session: SessionInfo) => void;
}

type HealthState = 'checking' | 'online' | 'offline';

interface HealthData {
  status: string;
  version?: string;
  uptime_seconds?: number;
  workers?: number;
  healthy_workers?: number;
}

function formatUptime(uptimeSeconds?: number) {
  if (uptimeSeconds == null || !Number.isFinite(uptimeSeconds)) return 'Awaiting';

  const totalMinutes = Math.max(0, Math.floor(uptimeSeconds / 60));
  const days = Math.floor(totalMinutes / (60 * 24));
  const hours = Math.floor((totalMinutes % (60 * 24)) / 60);
  const minutes = totalMinutes % 60;

  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return '<1m';
}

export function Login({ onAuthenticated }: LoginProps) {
  const [key, setKey] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [connected, setConnected] = useState(false);
  const [showKey, setShowKey] = useState(false);
  const [health, setHealth] = useState<HealthState>('checking');
  const [healthData, setHealthData] = useState<HealthData | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    const checkHealth = async () => {
      try {
        const res = await fetch('/health');
        if (res.ok) {
          const data = await res.json();
          setHealth('online');
          setHealthData(data);
        } else {
          setHealth('offline');
          setHealthData(null);
        }
      } catch {
        setHealth('offline');
        setHealthData(null);
      }
    };

    checkHealth();
    intervalRef.current = setInterval(checkHealth, 10000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!key.trim()) {
      setError('Please enter your API key');
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
          setError('Invalid API key. Check your key and try again.');
        } else if (err.message.includes('Admin access required')) {
          setError('Admin access required. Only admin keys can access the dashboard.');
        } else {
          setError('Could not connect to gateway. Is it running?');
        }
      } else {
        setError('Could not connect to gateway. Is it running?');
      }
    } finally {
      if (!connected) setLoading(false);
    }
  };

  const featureHighlights = [
    {
      index: '01',
      label: 'PUBLIC SOURCE',
      desc: 'Inspect the public gateway, worker, frontend, and deployment code before adopting the control plane.',
      meta: 'Verify the implementation',
    },
    {
      index: '02',
      label: 'OPENAI COMPATIBLE',
      desc: 'Point existing clients at your Infera base URL and keep the rest of the integration surface familiar.',
      meta: 'Drop-in client adoption',
    },
    {
      index: '03',
      label: 'MULTI-PROVIDER',
      desc: 'Provision across provider backends while keeping one workspace-level operator experience.',
      meta: 'One workspace, multiple runtimes',
    },
  ];

  const gatewayTone = health === 'offline' ? 'offline' : health === 'checking' ? 'checking' : 'online';
  const gatewayStatus = health === 'online' ? 'Online' : health === 'checking' ? 'Probing' : 'Offline';
  const workerCount = healthData?.workers ?? 0;
  const workerSummary = healthData?.healthy_workers != null && healthData?.workers != null
    ? `${healthData.healthy_workers}/${healthData.workers} healthy`
    : workerCount > 0
      ? `${workerCount} connected`
      : 'Waiting for workers';
  const runtimeBrief = [
    {
      label: 'Gateway',
      value: gatewayStatus,
      detail: health === 'online' ? 'Health endpoint responding' : health === 'checking' ? 'Polling /health' : 'No control-plane response',
      tone: gatewayTone,
    },
    {
      label: 'Workers',
      value: String(workerCount),
      detail: workerSummary,
      tone: workerCount > 0 ? 'online' : 'checking',
    },
    {
      label: 'Uptime',
      value: formatUptime(healthData?.uptime_seconds),
      detail: healthData?.version ? `Gateway ${healthData.version}` : 'Awaiting runtime metadata',
      tone: health === 'offline' ? 'offline' : 'checking',
    },
  ];
  const sessionNotes = [
    {
      label: 'Auth boundary',
      value: 'Admin only',
      detail: 'Full operator access only.',
    },
    {
      label: 'Session mode',
      value: 'Server-side',
      detail: 'Cookie issued after validation.',
    },
    {
      label: 'Workspace scope',
      value: 'Attached',
      detail: 'Bound to the key workspace.',
    },
  ];

  return (
    <div className="login-page">
      <div className="login-shell animate-fade-in">
        <section className="login-brand-panel">
          <div className="login-brand-content">
            <div className="login-kicker login-stagger login-stagger-1">Inference control plane</div>
            <div className="login-brand-grid">
              <div className="login-brand-hero">
                <div className="login-brand-principle login-stagger login-stagger-2">Technical. Minimal. Precise.</div>
                <h1 className="login-brand-title">INFERA</h1>
                <p className="login-brand-subtitle login-stagger login-stagger-3">
                  Self-hosted inference infrastructure with a product-grade operator surface.
                </p>
              </div>

              <div className="login-runtime-board">
                <div className="login-runtime-header">
                  <div className="label-text">Gateway signal</div>
                  <div className="mono login-runtime-endpoint">/health</div>
                </div>
                <div className="login-runtime-grid">
                  {runtimeBrief.map((item, i) => (
                    <div key={item.label} className={`login-runtime-card login-stagger login-stagger-${i + 4}`}>
                      <div className="login-runtime-label-row">
                        <span className="label-text">{item.label}</span>
                        <span className={`status-dot ${item.tone === 'online' ? '' : item.tone === 'offline' ? 'error' : 'neutral'}`} />
                      </div>
                      <div className="login-runtime-value">{item.value}</div>
                      <div className="login-runtime-detail">{item.detail}</div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div className="login-feature-list">
            {featureHighlights.map((feature, i) => (
              <div key={feature.label} className={`login-feature-item login-stagger login-stagger-${i + 5}`}>
                <div className="login-feature-meta">
                  <span className="login-feature-index mono">{feature.index}</span>
                  <div className="login-feature-title">{feature.label}</div>
                </div>
                <div className="login-feature-copy">{feature.desc}</div>
                <div className="login-feature-signal">{feature.meta}</div>
              </div>
            ))}
          </div>

          <div className="login-brand-footer login-stagger login-stagger-8">
            <LabelText className="mono">
            {healthData?.version ? `v${healthData.version}` : 'v0.1.0'}
            </LabelText>
            <LabelText>Public repository available</LabelText>
          </div>
        </section>

        <section className="login-form-panel">
          <div className="login-form-card">
            <div className="login-status-row login-stagger login-stagger-1">
              <StatusDot
                tone={health === 'offline' ? 'error' : health === 'checking' ? 'neutral' : 'success'}
                className={health === 'online' ? 'online-pulse' : undefined}
                style={health === 'checking' ? {
                  animation: 'skeleton-pulse 1.5s ease-in-out infinite',
                } : undefined}
              />
              <span className="mono login-status-copy">
                {health === 'checking' && 'Checking gateway...'}
                {health === 'online' && (
                  <>
                    Gateway online
                    {healthData?.workers != null && (
                      <>
                        <span style={{ color: 'var(--border-color)', margin: '0 6px' }}>·</span>
                        {healthData.workers} worker{healthData.workers !== 1 ? 's' : ''} connected
                      </>
                    )}
                  </>
                )}
                {health === 'offline' && 'Gateway unreachable'}
              </span>
            </div>

            <div className="login-form-copy login-stagger login-stagger-2">
              <LabelText as="div">Connect</LabelText>
              <h2 className="login-form-title">Sign in with an admin key</h2>
              <p className="login-form-description">
                Enter a valid admin key to open the workspace console and manage models, nodes, access, and usage.
              </p>
            </div>

            <div className="login-session-grid login-stagger login-stagger-3">
              {sessionNotes.map((note) => (
                <div key={note.label} className="login-session-card">
                  <div className="label-text">{note.label}</div>
                  <div className="login-session-value">{note.value}</div>
                  <p>{note.detail}</p>
                </div>
              ))}
            </div>

            <form onSubmit={handleSubmit} className="login-form-fields login-stagger login-stagger-4">
              <div className="login-field">
                <div className="login-field-header">
                  <LabelText as="label" htmlFor="login-api-key">API key</LabelText>
                  <div className="mono login-field-meta">POST /api/auth/session</div>
                </div>
                <div className="login-input-shell">
                  <ControlInput
                    id="login-api-key"
                    type={showKey ? 'text' : 'password'}
                    className="login-key-input"
                    placeholder="inf_..."
                    value={key}
                    onChange={e => { setKey(e.target.value); setError(''); }}
                    autoFocus
                    autoComplete="current-password"
                    aria-invalid={error ? 'true' : 'false'}
                  />
                  <button
                    type="button"
                    className="login-visibility-toggle"
                    onClick={() => setShowKey((visible) => !visible)}
                    aria-label={showKey ? 'Hide API key' : 'Show API key'}
                  >
                    {showKey ? 'Hide' : 'Show'}
                  </button>
                </div>
              </div>

              {error && (
                <div className="login-error" role="alert">{error}</div>
              )}

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

            <div className="login-help login-stagger login-stagger-5">
              <div className="login-help-note">
                Keys are generated by your gateway admin. The session is stored server-side and scoped to the workspace attached to the key you use.
              </div>
              <div className="login-help-links">
                <Link className="nav-link" to="/docs">API Docs</Link>
                <Link className="nav-link" to="/getting-started">Getting Started</Link>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
