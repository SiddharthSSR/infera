import { Link } from 'react-router-dom';
import { useState, useEffect, useRef } from 'react';
import { createSession, type SessionInfo } from '../lib/api';

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

export function Login({ onAuthenticated }: LoginProps) {
  const [key, setKey] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [connected, setConnected] = useState(false);
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
    { label: 'OPEN SOURCE', desc: 'Run anywhere: cloud, bare metal, or hybrid. Keep the control plane in your hands.' },
    { label: 'OPENAI COMPATIBLE', desc: 'Point existing clients at your Infera base URL and keep the rest of the integration surface familiar.' },
    { label: 'MULTI-PROVIDER', desc: 'Provision across provider backends while keeping one workspace-level operator experience.' },
  ];

  return (
    <div className="login-page">
      <div className="login-shell animate-fade-in">
        <section className="login-brand-panel">
          <div className="login-brand-content">
            <div className="login-kicker">Inference control plane</div>
            <h1 className="login-brand-title">INFERA</h1>
            <p className="login-brand-subtitle">
              Self-hosted inference infrastructure with a product-grade operator surface.
            </p>
          </div>

          <div className="login-feature-list">
            {featureHighlights.map((feature) => (
              <div key={feature.label} className="login-feature-item">
                <div className="login-feature-title">{feature.label}</div>
                <div className="login-feature-copy">{feature.desc}</div>
              </div>
            ))}
          </div>

          <div className="login-brand-footer">
            <span className="label-text mono">
            {healthData?.version ? `v${healthData.version}` : 'v0.1.0'}
            </span>
            <span className="label-text">Open source inference gateway</span>
          </div>
        </section>

        <section className="login-form-panel">
          <div className="login-form-card">
            <div className="login-status-row">
            <span
              className={health === 'offline' ? 'status-dot inactive' : 'status-dot'}
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

            <div className="login-form-copy">
              <div className="label-text">Connect</div>
              <h2 className="login-form-title">Sign in with an admin key</h2>
              <p className="login-form-description">
                Enter a valid admin key to open the workspace console and manage models, nodes, access, and usage.
              </p>
            </div>

            <form onSubmit={handleSubmit} className="login-form-fields">
              <div className="login-field">
                <div className="label-text">API key</div>
                <input
                  type="password"
                  className="control-input"
                  placeholder="inf_..."
                  value={key}
                  onChange={e => { setKey(e.target.value); setError(''); }}
                  autoFocus
                  autoComplete="current-password"
                />
              </div>

            {error && (
                <div className="login-error">{error}</div>
            )}

              <button
                className="btn-primary login-submit"
                type="submit"
                disabled={loading || connected}
              >
                {connected ? (
                  <span className="login-submit-state">
                    <span className="status-dot" />
                    Connected
                  </span>
                ) : loading ? 'Connecting...' : 'Connect'}
              </button>
            </form>

            <div className="login-help">
              <div>
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
