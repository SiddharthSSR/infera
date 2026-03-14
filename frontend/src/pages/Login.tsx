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

  return (
    <div className="login-page">
      <div className="login-shell animate-fade-in">
      {/* Left Panel — Branding */}
      <div className="login-brand-panel" style={{
        backgroundColor: 'var(--bg-accent)',
        borderRight: 'var(--grid-line)',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
        padding: '3rem',
      }}>
        <div />
        <div>
          <div className="display-text" style={{
            fontSize: '7rem',
            textAlign: 'left',
            border: 'none',
            padding: 0,
            lineHeight: 0.85,
          }}>
            INFERA
          </div>
          <div style={{
            fontSize: '1.05rem',
            color: 'var(--text-secondary)',
            marginTop: '1.5rem',
            fontWeight: 400,
            letterSpacing: '-0.01em',
          }}>
            Self-hosted inference at scale
          </div>
        </div>
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-end',
        }}>
          <span className="label-text mono" style={{ fontSize: '0.6rem', color: 'var(--text-secondary)' }}>
            {healthData?.version ? `v${healthData.version}` : 'v0.1.0'}
          </span>
          <span className="label-text" style={{ fontSize: '0.6rem', color: 'var(--text-secondary)' }}>
            OPEN SOURCE INFERENCE GATEWAY
          </span>
        </div>
      </div>

      {/* Right Panel — Connect Form */}
      <div className="login-form-panel" style={{
        backgroundColor: 'var(--bg-paper)',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
        padding: '3rem',
      }}>
        <div style={{ maxWidth: 380, width: '100%', margin: '0 auto' }}>
          {/* Gateway Status */}
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            marginBottom: '3rem',
          }}>
            <span
              className={health === 'offline' ? 'status-dot inactive' : 'status-dot'}
              style={health === 'checking' ? {
                animation: 'skeleton-pulse 1.5s ease-in-out infinite',
              } : undefined}
            />
            <span className="mono" style={{
              fontSize: '0.75rem',
              color: 'var(--text-secondary)',
            }}>
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

          {/* Section Header */}
          <div className="label-text" style={{ marginBottom: '2rem' }}>
            CONNECT
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit}>
            <div className="label-text" style={{ marginBottom: '0.75rem' }}>API KEY</div>
            <input
              type="password"
              className="control-input"
              placeholder="inf_..."
              value={key}
              onChange={e => { setKey(e.target.value); setError(''); }}
              autoFocus
              style={{ marginBottom: '1.5rem' }}
            />

            {error && (
              <div style={{
                color: 'var(--color-error)',
                fontSize: '0.85rem',
                marginBottom: '1.5rem',
              }}>
                {error}
              </div>
            )}

            <button
              className="btn-primary"
              type="submit"
              disabled={loading || connected}
              style={{ width: '100%', padding: '0.8rem' }}
            >
              {connected ? (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}>
                  <span className="status-dot" style={{ width: 6, height: 6 }} />
                  CONNECTED
                </span>
              ) : loading ? 'CONNECTING...' : 'CONNECT'}
            </button>
          </form>

          {/* Help Text */}
          <div style={{
            marginTop: '3rem',
            fontSize: '0.8rem',
            color: 'var(--text-secondary)',
            lineHeight: 1.6,
          }}>
            Enter your API key to access the dashboard. Keys are generated by your gateway admin.
            <div style={{ marginTop: '1rem', display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
              <Link className="nav-link" to="/docs">API DOCS</Link>
              <Link className="nav-link" to="/getting-started">GETTING STARTED</Link>
            </div>
          </div>
        </div>
      </div>

      </div>
    </div>
  );
}
